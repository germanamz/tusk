package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/germanamz/tusk/internal/aliasdispatch"
	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/leaseconfig"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/query"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/render"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/germanamz/tusk/internal/workspace/indexopen"
	"github.com/spf13/cobra"
)

// resolveWorkspace finds the enclosing workspace, loads its manifest, and
// merges the built-in type packs — the shared read-verb preamble. Not used by
// init (which creates the workspace) or watch/reindex (which interleave logger
// setup between Find and Load).
func resolveWorkspace() (*workspace.Workspace, *manifest.Manifest, error) {
	cwd, cwdErr := os.Getwd()

	if cwdErr != nil {
		return nil, nil, cwdErr
	}

	ws, findErr := workspace.Find(cwd)

	if findErr != nil {
		return nil, nil, fmt.Errorf("workspace: %w", findErr)
	}

	loaded, loadErr := manifest.Load(ws.ManifestPath)

	if loadErr != nil {
		return nil, nil, loadErr
	}

	manifest.MergeBuiltinPacks(loaded)

	return ws, loaded, nil
}

// openStore opens (or rebuilds) the workspace index at indexPath, wiring the
// reindex factory against root. Callers own the returned store's lifecycle and
// their own error wrapping. Shared by every command that opens the index.
func openStore(cmd *cobra.Command, root, indexPath string, loaded *manifest.Manifest) (*index.Index, error) {
	return indexopen.OpenOrRebuild(cmd.Context(), indexopen.Config{
		IndexPath: indexPath,
		ReindexFactory: func(idx *index.Index) reindex.Config {
			return reindex.Config{
				Root:       root,
				Repo:       index.NewNodeRepo(idx),
				Edges:      index.NewEdgeRepo(idx),
				EdgeTypes:  loaded.EdgeTypes,
				Meta:       index.NewMetaRepo(idx),
				FileStates: index.NewFileStateRepo(idx),
				EmbedQueue: index.NewEmbedQueueRepo(idx),
				Workers:    resolveEmbedWorkers(loaded),
			}
		},
		Logger: func(msg string) {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), msg)
		},
	})
}

// buildEmbedder constructs the Ollama embedder when [embeddings] selects it, or
// nil otherwise. Shared by the alias and context dispatch paths; the query and
// reindex commands build their embedder differently (validation / logger).
func buildEmbedder(loaded *manifest.Manifest) embed.Embedder {
	if loaded.Embeddings.Provider != "ollama" {
		return nil
	}

	timeout := time.Duration(embed.ResolveTimeoutSeconds(loaded.Embeddings.TimeoutSeconds)) * time.Second

	return embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: loaded.Embeddings.Endpoint,
		Model:    loaded.Embeddings.Model,
		Dim:      loaded.Embeddings.Dim,
		Timeout:  timeout,
	})
}

// newNodeService builds the per-call node.Service the write verbs use, wiring
// the runtime repos against store. warnings receives recovery / property-drift
// warning lines.
func newNodeService(ws *workspace.Workspace, store *index.Index, loaded *manifest.Manifest, engine node.Behaviors, warnings io.Writer) *node.Service {
	nodes := index.NewNodeRepo(store)

	return node.NewServiceWithBehaviors(
		ws.Root,
		nodes,
		index.NewEdgeRepo(store),
		loaded.EdgeTypes,
		index.NewEmbedQueueRepo(store),
		loaded.NodeTypes,
		index.NewPropertyDriftRepo(store),
		engine,
		index.NewWorkflowDriftRepo(store),
		warnings,
		node.NewIndexRefLookup(nodes),
		index.NewFileStateRepo(store),
		index.WorkerID(),
		leaseconfig.Resolve(loaded.Lease.TTLSeconds),
	)
}

// newAliasDeps assembles the aliasdispatch.Deps the run and context commands
// hand to the dispatcher. The CLI keeps the historical "no semantic page cap
// unless the caller asks" behavior (SemanticDefaultTake: 0); MCP applies 10.
func newAliasDeps(store *index.Index, loaded *manifest.Manifest, ws *workspace.Workspace, embedder embed.Embedder) aliasdispatch.Deps {
	return aliasdispatch.Deps{
		Database:            store.DB(),
		Manifest:            loaded,
		WorkspaceRoot:       ws.Root,
		NodeService:         node.NewService(ws.Root, index.NewNodeRepo(store)),
		Nodes:               index.NewNodeRepo(store),
		Edges:               index.NewEdgeRepo(store),
		EmbedQueue:          index.NewEmbedQueueRepo(store),
		WorkflowDrift:       index.NewWorkflowDriftRepo(store),
		PropertyDrift:       index.NewPropertyDriftRepo(store),
		Embeddings:          index.NewEmbeddingRepo(store),
		Meta:                index.NewMetaRepo(store),
		Embedder:            embedder,
		SemanticDefaultTake: 0,
	}
}

// listRowToCompactBasic maps a structural list row to the compact render row,
// copying only the structural fields. Shared by the node-list, context, and
// alias-node-list compact renderers.
func listRowToCompactBasic(row query.ListRow) render.CompactRow {
	return render.CompactRow{
		ID:         row.ID,
		Type:       row.Type,
		Title:      row.Title,
		Body:       row.Body,
		Properties: row.Properties,
		Edges:      row.Edges,
	}
}
