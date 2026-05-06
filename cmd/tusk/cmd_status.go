package main

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/status"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Quick workspace summary: node counts, edge count, queue depth, last reindex",
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

			snap, snapErr := status.Snapshot(status.Config{
				Nodes:      index.NewNodeRepo(store),
				Edges:      index.NewEdgeRepo(store),
				EmbedQueue: index.NewEmbedQueueRepo(store),
				Meta:       index.NewMetaRepo(store),
			})

			if snapErr != nil {
				return snapErr
			}

			tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

			_, _ = fmt.Fprintln(tab, "TYPE\tCOUNT")

			types := make([]string, 0, len(snap.NodesByType))

			for typeName := range snap.NodesByType {
				types = append(types, typeName)
			}

			sort.Strings(types)

			for _, typeName := range types {
				_, _ = fmt.Fprintf(tab, "%s\t%d\n", typeName, snap.NodesByType[typeName])
			}

			_ = tab.Flush()

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "edges: %d\nembed queue depth: %d\nlast reindex (unix ns): %s\n",
				snap.EdgeCount, snap.EmbedQueueDepth, snap.LastReindexAt)

			return nil
		},
	}

	return statusCmd
}
