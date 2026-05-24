package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newEdgeListCmd() *cobra.Command {
	var (
		fromFilter string
		toFilter   string
		typeFilter string
	)

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List edges, optionally filtered by source, target, or kind",
		Long: `List edges in the index.

Filter with any combination of --from, --to, and --type. Output is a
tab-aligned table of source, type, target, attributed source-path.`,
		Example: `  # All edges that touch a node (either direction)
  tusk edge list --from tickets/T-001
  tusk edge list --to   tickets/T-001

  # Every "blocks" edge in the workspace
  tusk edge list --type blocks`,
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

	return listCmd
}
