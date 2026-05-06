// Package mcp implements the long-running tusk MCP server.
package mcp

import (
	"context"
	"fmt"
	"time"

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

	Embedder embed.Embedder
	Chunker  embed.ChunkingStrategy
}

// Open builds a Runtime rooted at workspaceRoot.
func Open(workspaceRoot string) (*Runtime, error) {
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

	rt := &Runtime{
		Root:         ws.Root,
		ManifestPath: ws.ManifestPath,
		IndexPath:    ws.IndexPath,
		Manifest:     loaded,
		Index:        store,
		Nodes:        index.NewNodeRepo(store),
		Edges:        index.NewEdgeRepo(store),
		EmbedQueue:   index.NewEmbedQueueRepo(store),
		Embeddings:   index.NewEmbeddingRepo(store),
		Meta:         index.NewMetaRepo(store),
	}

	if loaded.Embeddings.Provider == "ollama" {
		rt.Embedder = embed.NewOllamaEmbedder(embed.OllamaConfig{
			Endpoint: loaded.Embeddings.Endpoint,
			Model:    loaded.Embeddings.Model,
			Dim:      loaded.Embeddings.Dim,
		})
		rt.Chunker = embed.WholeDocument{}
	}

	rt.NodeService = node.NewServiceWithEmbedQueue(
		rt.Root,
		rt.Nodes,
		rt.Edges,
		loaded.EdgeTypes,
		rt.EmbedQueue,
	)

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

// ReloadManifest re-reads the manifest from disk and rebuilds the NodeService.
// Use after `tusk_reindex` or out-of-band manifest edits.
func (rt *Runtime) ReloadManifest() error {
	loaded, loadErr := manifest.Load(rt.ManifestPath)

	if loadErr != nil {
		return fmt.Errorf("mcp: reload manifest: %w", loadErr)
	}

	rt.Manifest = loaded
	rt.NodeService = node.NewServiceWithEmbedQueue(
		rt.Root,
		rt.Nodes,
		rt.Edges,
		loaded.EdgeTypes,
		rt.EmbedQueue,
	)

	return nil
}
