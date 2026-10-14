package main

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/spf13/cobra"
)

func newReindexCmd() *cobra.Command {
	var (
		noEmbed     bool
		strictEmbed bool
	)

	reindexCmd := &cobra.Command{
		Use:   "reindex",
		Short: "Walk the workspace and bring the index up to date with disk",
		Long: `Walk the workspace and bring the SQLite index up to date with disk.

Reindex skips any file whose mtime and size are unchanged since the last
pass and re-parses only the rest. Pass --force to re-read, re-hash, and
re-process every file regardless.

When [embeddings] is configured, reindex then embeds the nodes it queued. If
the embedding backend can't be reached (Ollama stopped, or a CI runner without
it), the index is still built: the summary says how many nodes stay queued,
reindex exits 0, and a later reindex, "tusk watch" or "tusk mcp" embeds them.
Pass --strict-embed to exit 1 in that case, or --no-embed to skip embedding.

Run reindex after editing files outside Tusk (vim, Obsidian, scripts) or
after changing node/edge declarations in tusk.toml. For a schema change on a
running daemon, prefer "tusk reload" (or the tusk_reload MCP tool): it swaps
the in-memory schema and kicks a reindex that re-validates the index against
the new node-types, edge-types, and behaviors, surfacing violation counts in
the reindex output and "tusk doctor".

Worker pool: the embed/reindex pool size resolves from TUSK_EMBED_WORKERS,
then [embeddings] workers in tusk.toml, then max(1, NumCPU/2). Setting
the pool size to 0 opts out: this invocation walks and enqueues but does
not drain — another instance (or a later tusk reindex run) must drain
the queue.

Sub-unit addresses: each markdown sub-unit is indexed under a structural
address appended to the file id, e.g. notes/doc#S1.2P3. Editing prose in place
keeps a unit's address and re-embeds only the changed content; restructuring
shifts addresses but reuses unchanged vectors, so a reorder does not re-embed.`,
		Example: `  # Catch the index up with disk
  tusk reindex

  # Build the index without embedding (no Ollama needed)
  tusk reindex --no-embed

  # Fail when embeddings can't be refreshed
  tusk reindex --strict-embed

  # Pair with watch for continuous indexing while you author
  tusk watch &
  tusk reindex`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, findErr := findWorkspace(cmd)

			if findErr != nil {
				return findErr
			}

			verbose, _ := cmd.Flags().GetBool("verbose")
			logger := newLogger(cmd.ErrOrStderr(), verbose)

			force, _ := cmd.Flags().GetBool("force")

			loaded, loadErr := manifest.Load(ws.ManifestPath)

			if loadErr != nil {
				return fmt.Errorf("manifest: %w", loadErr)
			}

			manifest.MergeBuiltinPacks(loaded)

			store, openErr := openStore(cmd, ws.Root, ws.IndexPath, loaded)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			config, configErr := cliReindexConfig(ws.Root, store, loaded, logger)

			if configErr != nil {
				return configErr
			}

			config.Force = force

			if !noEmbed {
				if embedder, chunker := embed.NewFromManifest(loaded.Embeddings, logger); embedder != nil {
					config.Embedder = embedder
					config.Chunker = chunker
					config.EmbeddingRepo = index.NewEmbeddingRepo(store)
				}
			}

			report, runErr := reindex.Run(config)

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

			summaryExtras := ""

			if report.RefHealed > 0 {
				summaryExtras = fmt.Sprintf(", %d ref%s healed", report.RefHealed, plural(report.RefHealed))
			}

			if report.EmbedSettingsRequeued > 0 {
				summaryExtras += fmt.Sprintf(", %d node%s re-queued for new [embeddings] settings",
					report.EmbedSettingsRequeued, plural(report.EmbedSettingsRequeued))
			}

			if report.EmbedDeferred != nil {
				queued, _ := config.EmbedQueue.DepthByKind("embed")

				summaryExtras += fmt.Sprintf(", %d queued for embedding (embedder unreachable)", queued)
			}

			if len(violationParts) > 0 {
				_, _ = fmt.Fprintf(out,
					"Reindex done: %d indexed, %d removed, %s%s (%s)\nRun `tusk doctor` to inspect violations\n",
					report.Indexed, report.Removed, skippedSummary(report), summaryExtras,
					strings.Join(violationParts, ", "))
			} else {
				_, _ = fmt.Fprintf(out, "Reindex done: %d indexed, %d removed, %s%s\n",
					report.Indexed, report.Removed, skippedSummary(report), summaryExtras)
			}

			if strictEmbed && report.EmbedDeferred != nil {
				return fmt.Errorf("reindex: embedding failed (--strict-embed): %w", report.EmbedDeferred)
			}

			return nil
		},
	}

	reindexCmd.Flags().Bool("force", false, "re-process every file even if its mtime and size are unchanged")
	reindexCmd.Flags().BoolVar(&noEmbed, "no-embed", false, "build the structural index only; leave the embedding queue for a later run")
	reindexCmd.Flags().BoolVar(&strictEmbed, "strict-embed", false, "exit 1 when the embedding backend is unreachable")
	reindexCmd.MarkFlagsMutuallyExclusive("no-embed", "strict-embed")

	return reindexCmd
}

// cliReindexConfig builds the inline-drain reindex config the reindex and
// doctor commands share: the full per-file set (validators, drift repos,
// manifest) so every file is processed exactly as the background drainer would.
// It wires no embedder; a caller that embeds sets Embedder, Chunker and
// EmbeddingRepo itself.
func cliReindexConfig(root string, store *index.Index, loaded *manifest.Manifest, logger *slog.Logger) (reindex.Config, error) {
	engine, buildErr := newBehaviorEngine(loaded)

	if buildErr != nil {
		return reindex.Config{}, buildErr
	}

	return reindex.Config{
		Root:            root,
		Repo:            index.NewNodeRepo(store),
		Edges:           index.NewEdgeRepo(store),
		EdgeTypes:       loaded.EdgeTypes,
		WorkspaceIgnore: loaded.Workspace.Ignore,
		EmbedQueue:      index.NewEmbedQueueRepo(store),
		Meta:            index.NewMetaRepo(store),
		FileStates:      index.NewFileStateRepo(store),
		Behaviors:       engine,
		DriftLog:        index.NewWorkflowDriftRepo(store),
		NodeTypes:       loaded.NodeTypes,
		PropertyDrift:   index.NewPropertyDriftRepo(store),
		Logger:          logger,
		Workers:         resolveEmbedWorkers(loaded),
		Manifest:        loaded,
	}, nil
}

// skippedSummary renders a summary line's skip count, naming how many of the
// skips were plain markdown that is not a node rather than a fault (#789).
func skippedSummary(report *reindex.Report) string {
	switch report.NotNodes {
	case 0:
		return fmt.Sprintf("%d skipped", report.Skipped)
	case 1:
		return fmt.Sprintf("%d skipped (1 not a node)", report.Skipped)
	default:
		return fmt.Sprintf("%d skipped (%d not nodes)", report.Skipped, report.NotNodes)
	}
}

func plural(count int) string {
	if count == 1 {
		return ""
	}

	return "s"
}
