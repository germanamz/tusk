package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/query"
	"github.com/germanamz/tusk/internal/render"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newNodeListCmd() *cobra.Command {
	var (
		sortSpec    string
		take        int
		skip        int
		includeFlag []string
		fieldsFlag  []string
		formatFlag  string
		emitJSON    bool
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
edge type in tusk.toml). Recency shortcut: modified-since:<duration|ISO-date>
(e.g. modified-since:7d, modified-since:2026-05-23). Combine with AND, OR,
NOT, and parens. (Both : and = bind property comparisons; pick whichever
reads better.) Output is a tab-aligned table of id, type, title, path.

Use --sort to order by one or more keys (prefix +/-), --take N to limit,
and --skip M to paginate. Use --include to expand each row with body, edges,
or properties (comma-separated). Use --fields to project the rendered shape.
Use --format to pick compact or JSON output (default: compact for TTY, JSON
otherwise); --json is sugar for --format json. For structural-and-semantic
ranking, use "tusk query" with --semantic.`,
		Example: `  # All open tickets, highest priority first
  tusk node list 'type=ticket AND status=open' --sort '-priority'

  # Expand rows with body and edges
  tusk node list type=ticket --include body,edges

  # Page 2 of 20 most-recently-modified notes
  tusk node list type=note --sort '-modified' --take 20 --skip 20

  # Notes touched in the last 48 hours
  tusk node list 'type=note AND modified-since:48h'

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

			manifest.MergeBuiltinPacks(loaded)
			filterArg := ""

			if len(args) == 1 {
				filterArg = args[0]
			}

			store, openErr := index.Open(ws.IndexPath)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			result, runErr := query.ListRun(store.DB(), loaded, query.ListRequest{
				Filter:        filterArg,
				Sort:          sortSpec,
				Take:          take,
				Skip:          skip,
				Include:       includeFlag,
				Fields:        fieldsFlag,
				WorkspaceRoot: ws.Root,
			})

			if runErr != nil {
				return runErr
			}

			hasShapeFlags := len(includeFlag) > 0 || len(fieldsFlag) > 0
			format, formatErr := resolveFormat(emitJSON, formatFlag, hasShapeFlags)

			if formatErr != nil {
				return formatErr
			}

			switch format {
			case formatJSON:
				return writeJSON(cmd.OutOrStdout(), result.Rows)
			case formatCompact:
				return renderListCompact(cmd.OutOrStdout(), result.Rows, fieldsFlag)
			}

			// formatLegacy: tab-aligned id/type/title/path table.
			tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

			_, _ = fmt.Fprintln(tab, "ID\tTYPE\tTITLE\tPATH")

			for _, row := range result.Rows {
				_, _ = fmt.Fprintf(tab, "%s\t%s\t%s\t%s\n", row.ID, row.Type, row.Title, row.Path)
			}

			return tab.Flush()
		},
	}

	listCmd.Flags().StringVar(&sortSpec, "sort", "", "sort spec, e.g., +priority,-due,+modified")
	listCmd.Flags().IntVar(&take, "take", 0, "limit results to N rows")
	listCmd.Flags().IntVar(&skip, "skip", 0, "skip the first M rows (requires --take)")
	listCmd.Flags().StringSliceVar(&includeFlag, "include", nil, "expand rows: body|edges|properties (comma-separated)")
	listCmd.Flags().StringSliceVar(&fieldsFlag, "fields", nil, "project rendered rows to these fields (comma-separated)")
	listCmd.Flags().StringVar(&formatFlag, "format", "", "output format: compact|json (default: compact for TTY, json otherwise)")
	listCmd.Flags().BoolVar(&emitJSON, "json", false, "emit structured JSON (sugar for --format json)")

	return listCmd
}

// renderListCompact converts query.ListRow values to render.CompactRow and
// writes the compact form to out.
func renderListCompact(out interface{ Write(p []byte) (int, error) }, rows []query.ListRow, fields []string) error {
	compactRows := make([]render.CompactRow, 0, len(rows))

	for _, row := range rows {
		compactRows = append(compactRows, render.CompactRow{
			ID:         row.ID,
			Type:       row.Type,
			Title:      row.Title,
			Body:       row.Body,
			Properties: row.Properties,
			Edges:      row.Edges,
		})
	}

	return render.CompactNodeRows(out, compactRows, render.CompactOpts{Fields: fields})
}

// writeJSON marshals payload as JSON (followed by a newline) and writes to out.
func writeJSON(out interface{ Write(p []byte) (int, error) }, payload any) error {
	encoded, marshalErr := json.MarshalIndent(payload, "", "  ")

	if marshalErr != nil {
		return marshalErr
	}

	if _, writeErr := out.Write(encoded); writeErr != nil {
		return writeErr
	}

	if _, writeErr := out.Write([]byte("\n")); writeErr != nil {
		return writeErr
	}

	return nil
}
