package main

import (
	"fmt"
	"io"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/spf13/cobra"
)

// subUnitKindOrder pins the rendering order of the per-kind counts so the
// doctor output stays stable across runs. Matches the order in the spec
// example (sections → table-cells) and groups by structural prominence.
var subUnitKindOrder = []string{
	"section",
	"paragraph",
	"list-item",
	"code-block",
	"blockquote",
	"table-cell",
}

// renderSubUnitPane prints the sub-unit health pane (spec §5.9). Skips
// the pane entirely when nil — workspaces with sub-units disabled AND a
// clean index get no output here. The renderer follows the doctor's
// existing two-space-indent convention.
func renderSubUnitPane(out io.Writer, pane *doctor.SubUnitPane) {
	if pane == nil {
		return
	}

	_, _ = fmt.Fprintln(out, "sub-units:")
	_, _ = fmt.Fprintf(out, "  total                 %d\n", pane.Total)

	if len(pane.CountByKind) > 0 {
		_, _ = fmt.Fprintln(out, "  by kind:")

		// Render the pinned order first so the common kinds appear in
		// a predictable layout; then any unexpected kinds the pack may
		// introduce later.
		seen := map[string]struct{}{}

		for _, kind := range subUnitKindOrder {
			if count, ok := pane.CountByKind[kind]; ok {
				_, _ = fmt.Fprintf(out, "    %-18s%d\n", kind, count)
				seen[kind] = struct{}{}
			}
		}

		for kind, count := range pane.CountByKind {
			if _, already := seen[kind]; already {
				continue
			}

			_, _ = fmt.Fprintf(out, "    %-18s%d\n", kind, count)
		}
	}

	_, _ = fmt.Fprintf(out, "  deduped sub-units     %d\n", pane.DedupedSubUnits)
	_, _ = fmt.Fprintf(out, "  orphans               %d\n", pane.OrphanedSubUnits)
	_, _ = fmt.Fprintf(out, "  queue (files)         %d\n", pane.EmbedQueueFiles)
	_, _ = fmt.Fprintf(out, "  queue (sub-units)     %d\n", pane.EmbedQueueSubUnits)
	_, _ = fmt.Fprintf(out, "  oversize payloads     %d\n", pane.OversizeEmbedPayloads)
}

// renderGraphExpansionPane prints the [query.graph-expansion] pane.
// Skips entirely when nil (no manifest was supplied to doctor.Run); for
// every other case the pane is populated so users see the active values
// regardless of the enabled flag. Follows the doctor's existing
// two-space-indent convention.
func renderGraphExpansionPane(out io.Writer, pane *doctor.GraphExpansionPane) {
	if pane == nil {
		return
	}

	_, _ = fmt.Fprintln(out, "graph expansion:")
	_, _ = fmt.Fprintf(out, "  enabled               %t\n", pane.Enabled)
	_, _ = fmt.Fprintf(out, "  hops                  %d\n", pane.Hops)
	_, _ = fmt.Fprintf(out, "  weight                %.2f\n", pane.Weight)
	_, _ = fmt.Fprintf(out, "  candidate multiplier  %d\n", pane.CandidateMultiplier)

	if len(pane.EdgeTypes) > 0 {
		_, _ = fmt.Fprintln(out, "  edge types:")

		for _, name := range pane.EdgeTypes {
			_, _ = fmt.Fprintf(out, "    %s\n", name)
		}
	}

	if len(pane.UnknownEdgeTypes) > 0 {
		_, _ = fmt.Fprintln(out, "  unknown edge types:")

		for _, name := range pane.UnknownEdgeTypes {
			_, _ = fmt.Fprintf(out, "    %s (not declared in manifest; walker will skip it)\n", name)
		}
	}

	if pane.WeightZeroNoOp {
		_, _ = fmt.Fprintln(out, "  warning: enabled but weight=0 — feature is a no-op")
	}

	_, _ = fmt.Fprintln(out, "  hint: use `tusk query --semantic ... --explain` to see per-result graph/cosine breakdown when debugging.")
}

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
  * Sub-unit pane: per-kind counts, deduped sub-units, oversize payloads.
  * Graph-expansion pane: the resolved [query.graph-expansion] settings,
    unknown edge types referenced from the block, and a no-op warning
    when the feature is enabled with weight=0.

Doctor also auto-migrates any legacy "__cli__" / "__mcp__" edge rows in the
index back into the source node's markdown frontmatter — pass --no-migrate
for a diagnostic-only run.

Sub-unit addresses: sub-units are indexed under structural addresses appended
to the file id, e.g. notes/doc#S1.2P3. The "deduped sub-units" count is the
number of content groups shared by two or more sub-units (embedded once, then
shared). Sections are aggregated from their descendants, never embedded, so
they are not flagged as missing embeddings.`,
		Example: `  # Health snapshot after a manifest change
  tusk pack add kanban
  tusk doctor

  # Quick check before starting an MCP session
  tusk doctor && tusk mcp

  # Diagnostic-only run; do not migrate legacy edge rows
  tusk doctor --no-migrate`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, loaded, resolveErr := resolveWorkspace()

			if resolveErr != nil {
				return resolveErr
			}
			introspect := buildVerbIntrospector(cmd.Root())
			manifest.ValidateAliases(loaded, introspect)
			manifest.ValidateContext(loaded, introspect)

			out := cmd.OutOrStdout()

			// Doctor's migration arm rewrites source markdown across the
			// workspace; keep the workspace flock. Per-file leases cover
			// row writes for runtime mutations but not the workspace-wide
			// schema migration this command performs.
			return withWorkspaceLock(ws, func() error {
				store, openErr := openStore(cmd, ws.Root, ws.IndexPath, loaded)

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

				if len(report.AliasErrors) > 0 {
					_, _ = fmt.Fprintln(out, "aliases:")

					for _, aliasErr := range report.AliasErrors {
						_, _ = fmt.Fprintf(out, "  %s: %s\n", aliasErr.Name, aliasErr.Message)
					}
				}

				if len(report.ContextErrors) > 0 || len(report.MissingPinnedIDs) > 0 {
					_, _ = fmt.Fprintln(out, "context:")

					for _, contextErr := range report.ContextErrors {
						_, _ = fmt.Fprintf(out, "  %s\n", contextErr.Message)
					}

					for _, missingID := range report.MissingPinnedIDs {
						_, _ = fmt.Fprintf(out, "  missing pinned: %s\n", missingID)
					}
				}

				if len(report.Issues) == 0 {
					_, _ = fmt.Fprintln(out, "doctor: no issues")
				}

				for _, issue := range report.Issues {
					_, _ = fmt.Fprintf(out, "  [%s] %s: %s\n", issue.Kind, issue.NodeID, issue.Message)
				}

				_, _ = fmt.Fprintf(out, "embed queue depth: %d\n", report.EmbedQueueDepth)
				_, _ = fmt.Fprintf(out, "reindex queue depth: %d\n", report.ReindexQueueDepth)

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

				renderSubUnitPane(out, report.SubUnitPane)
				renderGraphExpansionPane(out, report.GraphExpansion)

				return nil
			})
		},
	}

	doctorCmd.Flags().BoolVar(&noMigrate, "no-migrate", false, "skip auto-migration of legacy __cli__/__mcp__ edge rows (diagnostic-only run)")

	return doctorCmd
}
