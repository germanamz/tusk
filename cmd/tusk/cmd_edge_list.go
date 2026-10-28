package main

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/query"
	"github.com/germanamz/tusk/internal/render"
	"github.com/spf13/cobra"
)

func newEdgeListCmd() *cobra.Command {
	var (
		fromFilter string
		toFilter   string
		typeFilter string
		formatFlag string
		emitJSON   bool
		subUnits   bool
	)

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List edges, optionally filtered by source, target, or kind",
		Long: `List edges in the index.

Filter with any combination of --from, --to, and --type. Output is a
tab-aligned table of source, type, target, attributed source-path. Use
--format compact|json (or --json) to opt into structured output; the
default mirrors prior CLI behavior (tab-aligned table for TTY).

A link written in a paragraph is stored for the file and again for the
paragraph and each section around it. The listing shows it once, from
the file, with UNITS naming the paragraphs, list items or sections the
link is written in, so you can open the passage with tusk node get. It
also leaves out each file's contains edges to its own sections and
paragraphs. --sub-units lists the stored rows as they are. A --from that
names a sub-unit (notes/x#S1.2P1) lists that sub-unit's own edges.

A file's targets of one type are listed in the order the file writes
them: its frontmatter list first, then body links in reading order.`,
		Example: `  # All edges that touch a node (either direction)
  tusk edge list --from tickets/T-001
  tusk edge list --to   tickets/T-001

  # Every "blocks" edge in the workspace
  tusk edge list --type blocks

  # One row per stored edge, including each section and paragraph's copy
  tusk edge list --to tickets/T-001 --sub-units

  # JSON for piping into jq
  tusk edge list --from tickets/T-001 --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, loaded, resolveErr := resolveWorkspace(cmd)

			if resolveErr != nil {
				return resolveErr
			}

			store, openErr := openStore(cmd, ws.Root, ws.IndexPath, loaded)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			result, runErr := query.EdgeListRun(index.NewEdgeRepo(store), index.NewNodeRepo(store), query.EdgeListRequest{
				EdgeListRequest: index.EdgeListRequest{
					From:          fromFilter,
					To:            toFilter,
					Type:          typeFilter,
					RequireFilter: true,
				},
				SubUnits: subUnits,
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
				return render.CompactEdgeRows(cmd.OutOrStdout(), render.EdgeListEntries(result.Rows))
			}

			return writeEdgeTable(cmd.OutOrStdout(), result.Rows)
		},
	}

	listCmd.Flags().StringVar(&fromFilter, "from", "", "filter to edges originating from this source id")
	listCmd.Flags().StringVar(&toFilter, "to", "", "filter to edges targeting this id")
	listCmd.Flags().StringVar(&typeFilter, "type", "", "filter by edge type")
	listCmd.Flags().StringVar(&formatFlag, "format", "", "output format: compact|json (default: legacy table)")
	listCmd.Flags().BoolVar(&emitJSON, "json", false, "emit structured JSON (sugar for --format json)")
	listCmd.Flags().BoolVar(&subUnits, "sub-units", false, "list the stored rows: one per section or paragraph that holds a link, plus each file's contains edges")

	return listCmd
}

// writeEdgeTable renders `tusk edge list`'s default tab-aligned table. The
// UNITS column appears only when a row folds sub-unit copies of its edge, and
// no line keeps the padding of an empty last cell.
func writeEdgeTable(out io.Writer, rows []query.FoldedEdge) error {
	withUnits := slices.ContainsFunc(rows, func(row query.FoldedEdge) bool { return len(row.Units) > 0 })

	var buf bytes.Buffer

	tab := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	header := "TYPE\tSOURCE\tTARGET\tSOURCE_PATH"

	if withUnits {
		header += "\tUNITS"
	}

	_, _ = fmt.Fprintln(tab, header)

	for _, row := range rows {
		line := strings.Join([]string{row.Type, row.SourceID, row.TargetID, row.SourcePath}, "\t")

		if withUnits {
			line += "\t" + strings.Join(row.Units, ",")
		}

		_, _ = fmt.Fprintln(tab, line)
	}

	if flushErr := tab.Flush(); flushErr != nil {
		return flushErr
	}

	for line := range strings.Lines(buf.String()) {
		if _, writeErr := io.WriteString(out, strings.TrimRight(line, " \n")+"\n"); writeErr != nil {
			return writeErr
		}
	}

	return nil
}
