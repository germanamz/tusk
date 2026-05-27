package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/germanamz/tusk/internal/workspace/indexopen"
	"github.com/spf13/cobra"
)

func newReindexCmd() *cobra.Command {
	reindexCmd := &cobra.Command{
		Use:   "reindex",
		Short: "Walk the workspace and bring the index up to date with disk",
		Long: `Walk the workspace and bring the SQLite index up to date with disk.

Reindex compares each file's mtime, size, and checksum against the index
and re-parses only changed files. Embedding refreshes for changed nodes
happen lazily — run "tusk watch" alongside, or in the background, to
drain the embedding queue.

Run reindex after editing files outside Tusk (vim, Obsidian, scripts) or
after changing node/edge declarations in tusk.toml.`,
		Example: `  # Catch the index up with disk
  tusk reindex

  # Pair with watch for continuous indexing while you author
  tusk watch &
  tusk reindex`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			verbose, _ := cmd.Flags().GetBool("verbose")
			logger := newLogger(cmd.ErrOrStderr(), verbose)

			loaded, loadErr := manifest.Load(ws.ManifestPath)

			if loadErr != nil {
				return fmt.Errorf("manifest: %w", loadErr)
			}

			manifest.MergeBuiltinPacks(loaded)

			store, openErr := indexopen.OpenOrRebuild(cmd.Context(), indexopen.Config{
				IndexPath: ws.IndexPath,
				ReindexFactory: func(idx *index.Index) reindex.Config {
					return reindex.Config{
						Root:      ws.Root,
						Repo:      index.NewNodeRepo(idx),
						Edges:     index.NewEdgeRepo(idx),
						EdgeTypes: loaded.EdgeTypes,
					}
				},
				Logger: func(msg string) {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), msg)
				},
			})

			if openErr != nil {
				return openErr
			}

			defer store.Close()

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
				timeout := time.Duration(embed.ResolveTimeoutSeconds(loaded.Embeddings.TimeoutSeconds)) * time.Second

				embedder = embed.NewOllamaEmbedder(embed.OllamaConfig{
					Endpoint: loaded.Embeddings.Endpoint,
					Model:    loaded.Embeddings.Model,
					Dim:      loaded.Embeddings.Dim,
					Logger:   logger,
					Timeout:  timeout,
				})
				chunker = embed.MarkdownRecursive{}
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
				NodeTypes:       loaded.NodeTypes,
				PropertyDrift:   index.NewPropertyDriftRepo(store),
				Logger:          logger,
				Workers:         embed.ResolveWorkers(loaded.Embeddings.Workers),
				Manifest:        loaded,
			})

			if runErr != nil {
				return runErr
			}

			out := cmd.OutOrStdout()

			var violationParts []string

			if report.WorkflowViolations > 0 {
				violationParts = append(violationParts,
					fmt.Sprintf("%d workflow-violation%s", report.WorkflowViolations, plural(report.WorkflowViolations)))
			}

			if report.PropertyViolations > 0 {
				violationParts = append(violationParts,
					fmt.Sprintf("%d property-violation%s", report.PropertyViolations, plural(report.PropertyViolations)))
			}

			if report.RefDangling > 0 {
				violationParts = append(violationParts,
					fmt.Sprintf("%d ref-dangling", report.RefDangling))
			}

			if report.RefAmbiguous > 0 {
				violationParts = append(violationParts,
					fmt.Sprintf("%d ref-ambiguous", report.RefAmbiguous))
			}

			if report.RefTypeMismatch > 0 {
				violationParts = append(violationParts,
					fmt.Sprintf("%d ref-type-mismatch", report.RefTypeMismatch))
			}

			if report.RefCycle > 0 {
				violationParts = append(violationParts,
					fmt.Sprintf("%d ref-cycle", report.RefCycle))
			}

			if len(violationParts) > 0 {
				_, _ = fmt.Fprintf(out,
					"Reindex done: %d indexed, %d removed, %d skipped (%s)\nRun `tusk doctor` to inspect violations\n",
					report.Indexed, report.Removed, report.Skipped,
					strings.Join(violationParts, ", "))
			} else {
				_, _ = fmt.Fprintf(out, "Reindex done: %d indexed, %d removed, %d skipped\n",
					report.Indexed, report.Removed, report.Skipped)
			}

			return nil
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
