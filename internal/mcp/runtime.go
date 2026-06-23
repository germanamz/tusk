// Package mcp implements the long-running tusk MCP server.
package mcp

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/germanamz/tusk/internal/behavior"
	"github.com/germanamz/tusk/internal/behavior/workflow"
	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/embedconfig"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/leaseconfig"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/germanamz/tusk/internal/workspace/indexopen"
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
	FileState   *index.FileStateRepo
	NodeService *node.Service

	BehaviorEngine *behavior.Engine
	WorkflowDrift  *index.WorkflowDriftRepo
	PropertyDrift  *index.PropertyDriftRepo

	Embedder embed.Embedder
	Chunker  embed.ChunkingStrategy
	Workers  int
	LeaseTTL time.Duration
	WorkerID string

	// AliasIntrospector is the manifest.VerbIntrospector used to validate
	// manifest-declared aliases at Open time. Callers that construct a
	// Runtime via Open get the introspector wired from the Cobra root
	// (via SetAliasIntrospector); tests may leave it nil to skip alias
	// validation entirely.
	aliasIntrospector manifest.VerbIntrospector

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

// WithAliasIntrospector wires a manifest.VerbIntrospector that Open (and
// buildReloaded) consult to validate manifest-declared aliases. Callers
// that have a Cobra root build the introspector with
// cmd/tusk.buildVerbIntrospector; callers without (tests) can pass a
// hand-built map.
func WithAliasIntrospector(introspect manifest.VerbIntrospector) Option {
	return func(rt *Runtime) {
		rt.aliasIntrospector = introspect
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

	manifest.MergeBuiltinPacks(loaded)

	rt := &Runtime{}

	for _, opt := range opts {
		opt(rt)
	}

	rt.Root = ws.Root
	rt.ManifestPath = ws.ManifestPath
	rt.IndexPath = ws.IndexPath

	store, openErr := indexopen.OpenOrRebuild(indexopen.Config{
		IndexPath: ws.IndexPath,
		ReindexFactory: func(idx *index.Index) reindex.Config {
			return reindex.Config{
				Root:       ws.Root,
				Repo:       index.NewNodeRepo(idx),
				Edges:      index.NewEdgeRepo(idx),
				EdgeTypes:  loaded.EdgeTypes,
				Meta:       index.NewMetaRepo(idx),
				FileStates: index.NewFileStateRepo(idx),
				EmbedQueue: index.NewEmbedQueueRepo(idx),
			}
		},
		Logger: func(msg string) {
			if rt.Logger != nil {
				rt.Logger.Info(msg)
			}
		},
	})

	if openErr != nil {
		return nil, fmt.Errorf("mcp: index: %w", openErr)
	}

	if buildErr := rt.buildFromStore(store, loaded); buildErr != nil {
		store.Close()

		return nil, buildErr
	}

	return rt, nil
}

// buildFromStore wires an already-open store and an already-merged manifest into
// rt: behavior engine, every repo, NodeService, embedder/chunker, worker count,
// and alias/context validation. rt.Root, rt.ManifestPath, rt.IndexPath,
// rt.Logger, and rt.aliasIntrospector must already be set. Shared by Open and by
// the reset/sibling-reopen paths so a Runtime is assembled identically at boot
// and after a swap.
func (rt *Runtime) buildFromStore(store *index.Index, loaded *manifest.Manifest) error {
	engine, buildErr := buildBehaviorEngine(loaded)

	if buildErr != nil {
		return fmt.Errorf("mcp: behavior engine: %w", buildErr)
	}

	driftRepo := index.NewWorkflowDriftRepo(store)
	propertyDriftRepo := index.NewPropertyDriftRepo(store)

	rt.Manifest = loaded
	rt.Index = store
	rt.Nodes = index.NewNodeRepo(store)
	rt.Edges = index.NewEdgeRepo(store)
	rt.EmbedQueue = index.NewEmbedQueueRepo(store)
	rt.Embeddings = index.NewEmbeddingRepo(store)
	rt.Meta = index.NewMetaRepo(store)
	rt.FileState = index.NewFileStateRepo(store)
	rt.BehaviorEngine = engine
	rt.WorkflowDrift = driftRepo
	rt.PropertyDrift = propertyDriftRepo
	rt.LeaseTTL = leaseconfig.Resolve(loaded.Lease.TTLSeconds)
	rt.WorkerID = index.WorkerID()

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
		rt.FileState,
		rt.WorkerID,
		rt.LeaseTTL,
	)

	if rt.aliasIntrospector != nil {
		manifest.ValidateAliases(loaded, rt.aliasIntrospector)
		manifest.ValidateContext(loaded, rt.aliasIntrospector)
	}

	if loaded.Embeddings.Provider == "ollama" {
		timeout := time.Duration(embed.ResolveTimeoutSeconds(loaded.Embeddings.TimeoutSeconds)) * time.Second

		rt.Embedder = embed.NewOllamaEmbedder(embed.OllamaConfig{
			Endpoint: loaded.Embeddings.Endpoint,
			Model:    loaded.Embeddings.Model,
			Dim:      loaded.Embeddings.Dim,
			Logger:   rt.Logger,
			Timeout:  timeout,
		})
		rt.Chunker = embed.MarkdownRecursive{}
	} else {
		rt.Embedder = nil
		rt.Chunker = nil
	}

	// Resolve worker count regardless of provider — the reindex worker pool
	// processes workflow/property/edge/sub-unit work even when no embedder
	// is configured. embedconfig honors an explicit 0 as the opt-out signal.
	rt.Workers = embedconfig.ResolveWorkers(loaded.Embeddings.Workers)

	return nil
}

// Close releases the index handle acquired by Open. Idempotent: subsequent
// calls are no-ops because Index is niled after the first release.
func (rt *Runtime) Close() error {
	if rt.Index == nil {
		return nil
	}

	closeErr := rt.Index.Close()
	rt.Index = nil

	return closeErr
}

// buildReloaded re-reads the manifest from disk, validates it with the SAME
// semantics as boot (parse error and behavior-engine build failure are
// blocking; dangling aliases / bad [context] entries are dropped and recorded
// on the fresh Manifest), and builds a FRESH Runtime that reuses the open index
// handle but recomputes everything else from the new manifest — so a hot reload
// is identical to a restart. On a blocking failure it returns an error and
// mutates nothing.
func (rt *Runtime) buildReloaded() (*Runtime, *ManifestDiff, error) {
	loaded, loadErr := manifest.Load(rt.ManifestPath)
	if loadErr != nil {
		return nil, nil, fmt.Errorf("mcp: reload manifest: %w", loadErr)
	}

	manifest.MergeBuiltinPacks(loaded)

	diff := DiffManifests(rt.Manifest, loaded)

	fresh := &Runtime{
		Root:              rt.Root,
		ManifestPath:      rt.ManifestPath,
		IndexPath:         rt.IndexPath,
		Logger:            rt.Logger,
		aliasIntrospector: rt.aliasIntrospector,
	}

	if buildErr := fresh.buildFromStore(rt.Index, loaded); buildErr != nil {
		return nil, nil, buildErr
	}

	return fresh, &diff, nil
}

// SetAliasIntrospector injects an alias introspector after construction. Used by
// tests; production callers wire it through the WithAliasIntrospector option.
func (rt *Runtime) SetAliasIntrospector(introspect manifest.VerbIntrospector) {
	rt.aliasIntrospector = introspect
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
