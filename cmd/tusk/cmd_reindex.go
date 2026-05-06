package main

import (
	"fmt"
	"os"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newReindexCmd() *cobra.Command {
	reindexCmd := &cobra.Command{
		Use:   "reindex",
		Short: "Walk the workspace and bring the index up to date with disk",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			return withWorkspaceLock(ws, func() error {
				store, openErr := index.Open(ws.IndexPath)

				if openErr != nil {
					return openErr
				}

				defer store.Close()

				loaded, loadErr := manifest.Load(ws.ManifestPath)

				if loadErr != nil {
					return fmt.Errorf("manifest: %w", loadErr)
				}

				engine, buildErr := newBehaviorEngine(loaded)

				if buildErr != nil {
					return buildErr
				}

				edgeRepo := index.NewEdgeRepo(store)
				driftRepo := index.NewWorkflowDriftRepo(store)

				var embedder embed.Embedder
				var chunker embed.ChunkingStrategy
				var embedQueue *index.EmbedQueueRepo
				var embeddingRepo *index.EmbeddingRepo

				if loaded.Embeddings.Provider == "ollama" {
					embedder = embed.NewOllamaEmbedder(embed.OllamaConfig{
						Endpoint: loaded.Embeddings.Endpoint,
						Model:    loaded.Embeddings.Model,
						Dim:      loaded.Embeddings.Dim,
					})
					chunker = embed.WholeDocument{}
					embedQueue = index.NewEmbedQueueRepo(store)
					embeddingRepo = index.NewEmbeddingRepo(store)
				}

				report, runErr := reindex.Run(reindex.Config{
					Root:            ws.Root,
					Repo:            index.NewNodeRepo(store),
					Edges:           edgeRepo,
					EdgeTypes:       loaded.EdgeTypes,
					WorkspaceIgnore: loaded.Workspace.Ignore,
					EmbedQueue:      embedQueue,
					EmbeddingRepo:   embeddingRepo,
					Embedder:        embedder,
					Chunker:         chunker,
					Meta:            index.NewMetaRepo(store),
					Behaviors:       engine,
					DriftLog:        driftRepo,
				})

				if runErr != nil {
					return runErr
				}

				out := cmd.OutOrStdout()

				if report.WorkflowViolations > 0 {
					_, _ = fmt.Fprintf(out,
						"Reindex done: %d indexed, %d removed, %d skipped, %d workflow-violation%s\nRun `tusk doctor` to inspect violations\n",
						report.Indexed, report.Removed, report.Skipped,
						report.WorkflowViolations, plural(report.WorkflowViolations))
				} else {
					_, _ = fmt.Fprintf(out, "Reindex done: %d indexed, %d removed, %d skipped\n",
						report.Indexed, report.Removed, report.Skipped)
				}

				return nil
			})
		},
	}

	return reindexCmd
}

func plural(count int) string {
	if count == 1 {
		return ""
	}

	return "s"
}
