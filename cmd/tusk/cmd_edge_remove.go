package main

import (
	"fmt"
	"os"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newEdgeRemoveCmd() *cobra.Command {
	var (
		edgeType string
		source   string
		target   string
	)

	removeCmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove an edge from the index",
		RunE: func(cmd *cobra.Command, args []string) error {
			if edgeType == "" || source == "" || target == "" {
				return fmt.Errorf("--type, --source, and --target are required")
			}

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

				edgeRepo := index.NewEdgeRepo(store)

				rows, listErr := edgeRepo.ListBySource(source)

				if listErr != nil {
					return listErr
				}

				cliExisting := filterCLI(rows)

				var kept []index.EdgeRow

				removed := 0

				for _, row := range cliExisting {
					if row.Type == edgeType && row.TargetID == target {
						removed++

						continue
					}

					kept = append(kept, row)
				}

				if removed == 0 {
					return fmt.Errorf("no CLI-added edge matches type=%q source=%q target=%q", edgeType, source, target)
				}

				renumbered := renumberByType(kept)

				if upsertErr := edgeRepo.UpsertAll(source, cliSourcePath, renumbered); upsertErr != nil {
					return upsertErr
				}

				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed edge %s: %s → %s\n", edgeType, source, target)

				return nil
			})
		},
	}

	removeCmd.Flags().StringVar(&edgeType, "type", "", "edge type")
	removeCmd.Flags().StringVar(&source, "source", "", "source node id")
	removeCmd.Flags().StringVar(&target, "target", "", "target node id")

	return removeCmd
}

func renumberByType(rows []index.EdgeRow) []index.EdgeRow {
	counters := map[string]int{}
	out := make([]index.EdgeRow, len(rows))

	for idx, row := range rows {
		out[idx] = row
		out[idx].Ordinal = counters[row.Type]
		counters[row.Type]++
	}

	return out
}
