package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/render"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newEdgeListCmd() *cobra.Command {
	var (
		fromFilter string
		toFilter   string
		typeFilter string
		formatFlag string
		emitJSON   bool
	)

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List edges, optionally filtered by source, target, or kind",
		Long: `List edges in the index.

Filter with any combination of --from, --to, and --type. Output is a
tab-aligned table of source, type, target, attributed source-path. Use
--format compact|json (or --json) to opt into structured output; the
default mirrors prior CLI behavior (tab-aligned table for TTY).`,
		Example: `  # All edges that touch a node (either direction)
  tusk edge list --from tickets/T-001
  tusk edge list --to   tickets/T-001

  # Every "blocks" edge in the workspace
  tusk edge list --type blocks

  # JSON for piping into jq
  tusk edge list --from tickets/T-001 --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			store, openErr := index.Open(ws.IndexPath)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			edgeRepo := index.NewEdgeRepo(store)

			result, runErr := index.EdgeListRun(edgeRepo, index.EdgeListRequest{
				From:          fromFilter,
				To:            toFilter,
				Type:          typeFilter,
				RequireFilter: true,
			})

			if runErr != nil {
				return runErr
			}

			format, formatErr := resolveFormat(emitJSON, formatFlag, false)

			if formatErr != nil {
				return formatErr
			}

			switch format {
			case formatJSON:
				return writeJSON(cmd.OutOrStdout(), result.Rows)
			case formatCompact:
				entries := make([]render.EdgeListEntry, 0, len(result.Rows))

				for _, row := range result.Rows {
					entries = append(entries, render.EdgeListEntry{
						Type:       row.Type,
						SourceID:   row.SourceID,
						TargetID:   row.TargetID,
						SourcePath: row.SourcePath,
					})
				}

				return render.CompactEdgeRows(cmd.OutOrStdout(), entries)
			}

			// formatLegacy: tab-aligned table.
			tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

			_, _ = fmt.Fprintln(tab, "TYPE\tSOURCE\tTARGET\tSOURCE_PATH")

			for _, row := range result.Rows {
				_, _ = fmt.Fprintf(tab, "%s\t%s\t%s\t%s\n", row.Type, row.SourceID, row.TargetID, row.SourcePath)
			}

			return tab.Flush()
		},
	}

	listCmd.Flags().StringVar(&fromFilter, "from", "", "filter to edges originating from this source id")
	listCmd.Flags().StringVar(&toFilter, "to", "", "filter to edges targeting this id")
	listCmd.Flags().StringVar(&typeFilter, "type", "", "filter by edge type")
	listCmd.Flags().StringVar(&formatFlag, "format", "", "output format: compact|json (default: legacy table)")
	listCmd.Flags().BoolVar(&emitJSON, "json", false, "emit structured JSON (sugar for --format json)")

	return listCmd
}
