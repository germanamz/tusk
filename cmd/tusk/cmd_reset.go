package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/reset"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newResetCmd() *cobra.Command {
	resetCmd := &cobra.Command{
		Use:   "reset",
		Short: "Drop the local index and rebuild it from source files",
		Long: `Delete the local SQLite index (.tusk/index.db and its WAL/SHM
sidecars) and rebuild it from scratch by walking the workspace.

Unlike "tusk reindex" — which never deletes and only re-parses changed files —
reset destroys the index entirely, so it recovers from a corrupt or wedged
index. The markdown files are the source of truth, so nothing is lost; the
rebuild re-derives every node, edge, and embedding from disk.

Reset re-embeds every node, which can be expensive with a local embedding
model. It requires confirmation; pass --yes to skip the prompt.`,
		Example: `  # Drop and rebuild the index, with confirmation
  tusk reset

  # Non-interactive (e.g. scripts/agents)
  tusk reset --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			yes, _ := cmd.Flags().GetBool("yes")

			if !yes {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
					"Drop and rebuild the index for %s? This deletes .tusk/index.db and re-embeds every node. [y/N]: ",
					ws.Root)

				if !confirmReset(cmd.InOrStdin()) {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Aborted.")

					return nil
				}
			}

			verbose, _ := cmd.Flags().GetBool("verbose")
			logger := newLogger(cmd.ErrOrStderr(), verbose)

			loaded, loadErr := manifest.Load(ws.ManifestPath)

			if loadErr != nil {
				return fmt.Errorf("manifest: %w", loadErr)
			}

			manifest.MergeBuiltinPacks(loaded)

			result, resetErr := reset.Perform(cmd.Context(), reset.Config{
				Root:      ws.Root,
				IndexPath: ws.IndexPath,
				LockTTL:   5 * time.Second,
				Reopen:    func() (*index.Index, error) { return index.Open(ws.IndexPath) },
			})

			if resetErr != nil {
				return resetErr
			}

			defer func() { _ = result.Store.Close() }()

			engine, buildErr := newBehaviorEngine(loaded)

			if buildErr != nil {
				return buildErr
			}

			// reset is an explicit one-shot rebuild ("command returns ⇒ rebuilt"),
			// so it must never opt out of draining: a blocking reindex.Run with
			// Workers<=0 returns immediately WITHOUT indexing (reindex.go /
			// worker.go short-circuit on Workers<=0). Clamp to at least 1 worker
			// for the reset rebuild, regardless of [embeddings] workers / TUSK_EMBED_WORKERS.
			// This is a deliberate, single deviation from the shared worker resolver.
			rebuildWorkers := resolveEmbedWorkers(loaded)

			if rebuildWorkers < 1 {
				rebuildWorkers = 1
			}

			cfg := reindex.Config{
				Root:            ws.Root,
				Repo:            index.NewNodeRepo(result.Store),
				Edges:           index.NewEdgeRepo(result.Store),
				EdgeTypes:       loaded.EdgeTypes,
				WorkspaceIgnore: loaded.Workspace.Ignore,
				EmbedQueue:      index.NewEmbedQueueRepo(result.Store),
				Meta:            index.NewMetaRepo(result.Store),
				FileStates:      index.NewFileStateRepo(result.Store),
				Behaviors:       engine,
				DriftLog:        index.NewWorkflowDriftRepo(result.Store),
				NodeTypes:       loaded.NodeTypes,
				PropertyDrift:   index.NewPropertyDriftRepo(result.Store),
				Logger:          logger,
				Workers:         rebuildWorkers,
				Manifest:        loaded,
				Async:           false,
			}

			if embedder := buildEmbedder(loaded); embedder != nil {
				cfg.Embedder = embedder
				cfg.Chunker = embed.MarkdownRecursive{}
				cfg.EmbeddingRepo = index.NewEmbeddingRepo(result.Store)
			}

			report, runErr := reindex.Run(cfg)

			if runErr != nil {
				return runErr
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"Reset done: %d indexed, %d removed, %d skipped (epoch %d)\n",
				report.Indexed, report.Removed, report.Skipped, result.Epoch)

			return nil
		},
	}

	resetCmd.Flags().Bool("yes", false, "skip the confirmation prompt")

	return resetCmd
}

// confirmReset reads one line from in and returns true only for an affirmative
// answer ("y"/"yes", case-insensitive). EOF or any other input is "no".
func confirmReset(in io.Reader) bool {
	line, _ := bufio.NewReader(in).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))

	return answer == "y" || answer == "yes"
}
