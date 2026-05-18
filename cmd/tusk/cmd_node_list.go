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
		Long: `List nodes from the index, optionally filtering by expression.

The filter is a property and edge-traversal expression. Property
predicates use comparison operators (key=value, key:value, key!=value,
key<value, key<=value, key>value, key>=value); ranges use key=lo..hi.
Edge traversal uses edge-type-> or edge-type<- and may chain multi-hop.
Traversal shortcuts: tree=id, parent=id, root=id (qualified: tree:<alias>=id,
parent:<alias>=id, root:<alias>=id, where <alias> is set via hierarchy on an
edge type in tusk.toml). Combine with AND, OR,
NOT, and parens. (Both : and = bind property comparisons; pick whichever
reads better.) Output is a tab-aligned table of id, type, title, path.

Use --sort to order by one or more keys (prefix +/-), --take N to limit,
and --skip M to paginate. For structural-and-semantic ranking, use
"tusk query" with --semantic.`,
		Example: `  # All open tickets, highest priority first
  tusk node list 'type=ticket AND status=open' --sort '-priority'

  # Page 2 of 20 most-recently-modified notes
  tusk node list type=note --sort '-modified' --take 20 --skip 20

  # Pipe a single id into "node get"
  tusk node list 'type=ticket AND priority=1' --take 1 | awk 'NR==2 {print $1}' | xargs tusk node get`,
		Args: cobra.MaximumNArgs(1),
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
