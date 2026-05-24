package main

import (
	"fmt"
	"os"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var noMigrate bool

	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Surface validation warnings, dangling edges, and index health issues",
		Long: `Run health checks against the workspace and index.

Doctor reports:
  * Off-schema nodes (type not declared in tusk.toml).
  * Property drift (frontmatter values whose type does not match the
    manifest declaration).
  * Dangling edges (edges whose target node no longer exists).
  * Embedding queue depth and last-reindex timestamp.

Doctor also auto-migrates any legacy "__cli__" / "__mcp__" edge rows in the
index back into the source node's markdown frontmatter — pass --no-migrate
for a diagnostic-only run.`,
		Example: `  # Health snapshot after a manifest change
  tusk pack add gtd
  tusk doctor

  # Quick check before starting an MCP session
  tusk doctor && tusk mcp

  # Diagnostic-only run; do not migrate legacy edge rows
  tusk doctor --no-migrate`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			loaded, loadErr := manifest.Load(ws.ManifestPath)

			if loadErr != nil {
				return loadErr
			}

			out := cmd.OutOrStdout()

			return withWorkspaceLock(ws, func() error {
				store, openErr := index.Open(ws.IndexPath)

				if openErr != nil {
					return openErr
				}

				defer store.Close()

				cfg := doctor.Config{
					Nodes:         index.NewNodeRepo(store),
					Edges:         index.NewEdgeRepo(store),
					EmbedQueue:    index.NewEmbedQueueRepo(store),
					WorkflowDrift: index.NewWorkflowDriftRepo(store),
					PropertyDrift: index.NewPropertyDriftRepo(store),
					Embeddings:    index.NewEmbeddingRepo(store),
					Manifest:      loaded,
					Root:          ws.Root,
				}

				result, runErr := doctor.RunWithMigration(doctor.Request{Cfg: cfg, NoMigrate: noMigrate})

				if runErr != nil {
					return runErr
				}

				if result.Migration != nil {
					if len(result.Migration.Migrated) > 0 {
						_, _ = fmt.Fprintf(out, "migrated %d legacy CLI/MCP edges into source frontmatter:\n", len(result.Migration.Migrated))

						for _, line := range result.Migration.Migrated {
							_, _ = fmt.Fprintf(out, "  %s\n", line)
						}
					}

					if len(result.Migration.Skipped) > 0 {
						_, _ = fmt.Fprintf(out, "skipped %d legacy CLI/MCP edges:\n", len(result.Migration.Skipped))

						for _, line := range result.Migration.Skipped {
							_, _ = fmt.Fprintf(out, "  %s\n", line)
						}
					}
				}

				report := result.Report

				if len(report.Issues) == 0 {
					_, _ = fmt.Fprintln(out, "doctor: no issues")
				}

				for _, issue := range report.Issues {
					_, _ = fmt.Fprintf(out, "  [%s] %s: %s\n", issue.Kind, issue.NodeID, issue.Message)
				}

				_, _ = fmt.Fprintf(out, "embed queue depth: %d\n", report.EmbedQueueDepth)

				if report.EmbedStats != nil {
					stats := report.EmbedStats

					_, _ = fmt.Fprintf(out, "embed stats: %d nodes, %d chunks (mean %.1f, median %d, max %d)\n",
						stats.TotalNodes, stats.TotalChunks, stats.MeanChunks, stats.MedianChunks, stats.MaxChunks)

					if len(stats.TopByChunks) > 0 {
						_, _ = fmt.Fprintln(out, "top by chunks:")

						for _, entry := range stats.TopByChunks {
							_, _ = fmt.Fprintf(out, "  %s\t%d\n", entry.NodeID, entry.Chunks)
						}
					}
				}

				return nil
			})
		},
	}

	doctorCmd.Flags().BoolVar(&noMigrate, "no-migrate", false, "skip auto-migration of legacy __cli__/__mcp__ edge rows (diagnostic-only run)")

	return doctorCmd
}
