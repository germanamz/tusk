package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newNodeListCmd() *cobra.Command {
	var (
		sortSpec string
		take     int
		skip     int
	)

	listCmd := &cobra.Command{
		Use:   "list [filter]",
		Short: "List nodes from the index, optionally filtering by expression",
		Args:  cobra.MaximumNArgs(1),
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

			filterArg := ""

			if len(args) == 1 {
				filterArg = args[0]
			}

			expr, parseErrs := filter.NewParser(filterArg).Parse()

			if len(parseErrs) > 0 {
				return fmt.Errorf("filter parse: %v", parseErrs[0])
			}

			validateErrs := filter.Validate(expr, *loaded)

			if len(validateErrs) > 0 {
				return fmt.Errorf("filter validate: %v", validateErrs[0])
			}

			sortKeys, sortErr := filter.ParseSort(sortSpec)

			if sortErr != nil {
				return sortErr
			}

			sqlQuery, params, compileErr := filter.Compile(expr, filter.CompileOptions{
				SortKeys: sortKeys,
				Take:     take,
				Skip:     skip,
			})

			if compileErr != nil {
				return compileErr
			}

			store, openErr := index.Open(ws.IndexPath)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			rows, queryErr := store.DB().Query(sqlQuery, params...)

			if queryErr != nil {
				return queryErr
			}

			defer rows.Close()

			tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

			_, _ = fmt.Fprintln(tab, "ID\tTYPE\tTITLE\tPATH")

			for rows.Next() {
				var (
					rowID         string
					rowType       string
					rowPath       string
					rowTitle      string
					propertiesRaw string
					lastMtime     int64
					lastSize      int64
					lastChecksum  string
				)

				if scanErr := rows.Scan(&rowID, &rowType, &rowPath, &rowTitle, &propertiesRaw, &lastMtime, &lastSize, &lastChecksum); scanErr != nil {
					return scanErr
				}

				_, _ = fmt.Fprintf(tab, "%s\t%s\t%s\t%s\n", rowID, rowType, rowTitle, rowPath)
			}

			return tab.Flush()
		},
	}

	listCmd.Flags().StringVar(&sortSpec, "sort", "", "sort spec, e.g., +priority,-due,+modified")
	listCmd.Flags().IntVar(&take, "take", 0, "limit results to N rows")
	listCmd.Flags().IntVar(&skip, "skip", 0, "skip the first M rows (requires --take)")

	return listCmd
}
