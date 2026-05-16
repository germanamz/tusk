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

			rows, queryErr := selectEdges(edgeRepo, fromFilter, toFilter, typeFilter)

			if queryErr != nil {
				return queryErr
			}

			tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

			_, _ = fmt.Fprintln(tab, "TYPE\tSOURCE\tTARGET\tORDINAL\tSOURCE_PATH")

			for _, row := range rows {
				_, _ = fmt.Fprintf(tab, "%s\t%s\t%s\t%d\t%s\n", row.Type, row.SourceID, row.TargetID, row.Ordinal, row.SourcePath)
			}

			return tab.Flush()
		},
	}

	listCmd.Flags().StringVar(&fromFilter, "from", "", "filter to edges originating from this source id")
	listCmd.Flags().StringVar(&toFilter, "to", "", "filter to edges targeting this id")
	listCmd.Flags().StringVar(&typeFilter, "type", "", "filter by edge type")

	return listCmd
}

func selectEdges(repo *index.EdgeRepo, fromID, toID, edgeType string) ([]index.EdgeRow, error) {
	switch {
	case fromID != "":
		rows, listErr := repo.ListBySource(fromID)

		if listErr != nil {
			return nil, listErr
		}

		return narrow(rows, toID, edgeType), nil
	case toID != "":
		rows, listErr := repo.ListByTarget(toID)

		if listErr != nil {
			return nil, listErr
		}

		return narrow(rows, "", edgeType), nil
	case edgeType != "":
		return repo.ListByType(edgeType)
	}

	return nil, fmt.Errorf("specify at least one of --from, --to, --type")
}

func narrow(rows []index.EdgeRow, toID, edgeType string) []index.EdgeRow {
	var out []index.EdgeRow

	for _, row := range rows {
		if toID != "" && row.TargetID != toID {
			continue
		}

		if edgeType != "" && row.Type != edgeType {
			continue
		}

		out = append(out, row)
	}

	return out
}
