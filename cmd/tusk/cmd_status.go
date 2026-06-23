package main

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/status"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Print a one-screen workspace summary",
		Long: `Print a one-screen summary of workspace state: node counts by
type, total edges, embedding-queue depth, and time of last reindex.

Use status as a fast pulse check; use "tusk doctor" for validation
warnings and drift detail.`,
		Example: `  # Fast pulse check
  tusk status

  # Watch status in a loop while "tusk watch" is running
  watch -n 5 tusk status`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, loaded, resolveErr := resolveWorkspace()

			if resolveErr != nil {
				return resolveErr
			}

			store, openErr := openStore(cmd, ws.Root, ws.IndexPath, loaded)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			result, runErr := status.Run(status.Request{
				Nodes:      index.NewNodeRepo(store),
				Edges:      index.NewEdgeRepo(store),
				EmbedQueue: index.NewEmbedQueueRepo(store),
				Meta:       index.NewMetaRepo(store),
			})

			if runErr != nil {
				return runErr
			}

			return renderStatusCompact(cmd.OutOrStdout(), result)
		},
	}

	return statusCmd
}

// renderStatusCompact writes the one-screen compact status summary: a sorted
// TYPE/COUNT tabwriter block followed by edge / queue-depth / last-reindex
// lines. Shared by `tusk status` and the status-alias compact path so both
// surfaces emit identical text (single `last reindex (unix ns):` label).
func renderStatusCompact(out io.Writer, result *status.Result) error {
	tab := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)

	_, _ = fmt.Fprintln(tab, "TYPE\tCOUNT")

	types := make([]string, 0, len(result.NodesByType))

	for typeName := range result.NodesByType {
		types = append(types, typeName)
	}

	sort.Strings(types)

	for _, typeName := range types {
		_, _ = fmt.Fprintf(tab, "%s\t%d\n", typeName, result.NodesByType[typeName])
	}

	_ = tab.Flush()

	_, writeErr := fmt.Fprintf(out, "edges: %d\nembed queue depth: %d\nreindex queue depth: %d\nlast reindex (unix ns): %s\n",
		result.EdgeCount, result.EmbedQueueDepth, result.ReindexQueueDepth, result.LastReindexAt)

	return writeErr
}
