// Package mcp implements the long-running tusk MCP server.
package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/germanamz/tusk/internal/behavior"
	"github.com/germanamz/tusk/internal/behavior/workflow"
	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/lock"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/workspace"
)

// Runtime bundles every shared dependency the MCP server's tool handlers need.
type Runtime struct {
	Root         string
	ManifestPath string
	IndexPath    string

	Manifest    *manifest.Manifest
	Index       *index.Index
	Nodes       *index.NodeRepo
	Edges       *index.EdgeRepo
	EmbedQueue  *index.EmbedQueueRepo
	Embeddings  *index.EmbeddingRepo
	Meta        *index.MetaRepo
	NodeService *node.Service

	BehaviorEngine *behavior.Engine
	WorkflowDrift  *index.WorkflowDriftRepo
	PropertyDrift  *index.PropertyDriftRepo

	Embedder embed.Embedder
	Chunker  embed.ChunkingStrategy

	Logger *slog.Logger // optional; nil silences output
}

// Option mutates a Runtime during Open.
type Option func(*Runtime)

// WithLogger sets the slog.Logger that Open stores on the Runtime. Forwarded
// into DrainerConfig.Logger and WatchConfig.Logger by Server.RunBackground.
func WithLogger(logger *slog.Logger) Option {
	return func(rt *Runtime) {
		rt.Logger = logger
	}
}

// Open builds a Runtime rooted at workspaceRoot.
func Open(workspaceRoot string, opts ...Option) (*Runtime, error) {
	ws, findErr := workspace.Find(workspaceRoot)

	if findErr != nil {
		return nil, fmt.Errorf("mcp: workspace: %w", findErr)
	}

	loaded, loadErr := manifest.Load(ws.ManifestPath)

	if loadErr != nil {
		return nil, fmt.Errorf("mcp: manifest: %w", loadErr)
	}

	store, openErr := index.Open(ws.IndexPath)

	if openErr != nil {
		return nil, fmt.Errorf("mcp: index: %w", openErr)
	}

	engine, buildErr := buildBehaviorEngine(loaded)

	if buildErr != nil {
		store.Close()

		return nil, fmt.Errorf("mcp: behavior engine: %w", buildErr)
	}

	driftRepo := index.NewWorkflowDriftRepo(store)
	propertyDriftRepo := index.NewPropertyDriftRepo(store)

	rt := &Runtime{
		Root:           ws.Root,
		ManifestPath:   ws.ManifestPath,
		IndexPath:      ws.IndexPath,
		Manifest:       loaded,
		Index:          store,
		Nodes:          index.NewNodeRepo(store),
		Edges:          index.NewEdgeRepo(store),
		EmbedQueue:     index.NewEmbedQueueRepo(store),
		Embeddings:     index.NewEmbeddingRepo(store),
		Meta:           index.NewMetaRepo(store),
		BehaviorEngine: engine,
		WorkflowDrift:  driftRepo,
		PropertyDrift:  propertyDriftRepo,
	}

	rt.NodeService = node.NewServiceWithBehaviors(
		rt.Root,
		rt.Nodes,
		rt.Edges,
		loaded.EdgeTypes,
		rt.EmbedQueue,
		loaded.NodeTypes,
		propertyDriftRepo,
		engine,
		driftRepo,
		os.Stderr,
		node.NewIndexRefLookup(rt.Nodes),
	)

	for _, opt := range opts {
		opt(rt)
	}

	if loaded.Embeddings.Provider == "ollama" {
		rt.Embedder = embed.NewOllamaEmbedder(embed.OllamaConfig{
			Endpoint: loaded.Embeddings.Endpoint,
			Model:    loaded.Embeddings.Model,
			Dim:      loaded.Embeddings.Dim,
			Logger:   rt.Logger,
		})
		rt.Chunker = embed.MarkdownRecursive{}
	}

	return rt, nil
}

// Close releases the index handle.
func (rt *Runtime) Close() error {
	if rt.Index == nil {
		return nil
	}

	return rt.Index.Close()
}

// WithWriteLock acquires the per-write workspace lock, runs body, and always
// releases. 5-second acquisition timeout.
func (rt *Runtime) WithWriteLock(body func() error) error {
	handle, newErr := lock.NewWorkspaceLock(rt.Root)

	if newErr != nil {
		return newErr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if acquireErr := handle.Acquire(ctx); acquireErr != nil {
		return acquireErr
	}

	defer func() { _ = handle.Release() }()

	return body()
}

// ReloadManifest re-reads the manifest from disk and rebuilds the NodeService
// and BehaviorEngine. Use after `tusk_reindex` or out-of-band manifest edits.
func (rt *Runtime) ReloadManifest() error {
	loaded, loadErr := manifest.Load(rt.ManifestPath)

	if loadErr != nil {
		return fmt.Errorf("mcp: reload manifest: %w", loadErr)
	}

	engine, buildErr := buildBehaviorEngine(loaded)

	if buildErr != nil {
		return fmt.Errorf("mcp: rebuild behavior engine: %w", buildErr)
	}

	rt.Manifest = loaded
	rt.BehaviorEngine = engine
	rt.NodeService = node.NewServiceWithBehaviors(
		rt.Root,
		rt.Nodes,
		rt.Edges,
		loaded.EdgeTypes,
		rt.EmbedQueue,
		loaded.NodeTypes,
		rt.PropertyDrift,
		engine,
		rt.WorkflowDrift,
		os.Stderr,
		node.NewIndexRefLookup(rt.Nodes),
	)

	return nil
}

// buildBehaviorEngine constructs a *behavior.Engine from loaded by registering
// every built-in pack kind. Mirrors cmd/tusk's newBehaviorEngine.
func buildBehaviorEngine(loaded *manifest.Manifest) (*behavior.Engine, error) {
	registry := behavior.NewRegistry()

	if registerErr := registry.Register(workflow.Kind{}); registerErr != nil {
		return nil, fmt.Errorf("mcp: register workflow: %w", registerErr)
	}

	declaredKeys := declaredKeysFromManifest(loaded)

	return registry.BuildEngineWithDeclaredKeys(loaded, declaredKeys)
}

// declaredKeysFromManifest converts the manifest's NodeTypes map into a slice
// of behavior.DeclaredKey. Mirrors cmd/tusk's declaredKeysFrom helper.
func declaredKeysFromManifest(loaded *manifest.Manifest) []behavior.DeclaredKey {
	var keys []behavior.DeclaredKey

	for typeName, nt := range loaded.NodeTypes {
		for _, prop := range nt.Properties {
			keys = append(keys, behavior.DeclaredKey{
				NodeType: typeName,
				Property: prop.Name,
				Source:   fmt.Sprintf("node-types.%s.properties[%s]", typeName, prop.Name),
			})
		}
	}

	return keys
}
