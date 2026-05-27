package main

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/status"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/germanamz/tusk/internal/workspace/indexopen"
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

			store, openErr := indexopen.OpenOrRebuild(cmd.Context(), indexopen.Config{
				IndexPath: ws.IndexPath,
				ReindexFactory: func(idx *index.Index) reindex.Config {
					return reindex.Config{
						Root:      ws.Root,
						Repo:      index.NewNodeRepo(idx),
						Edges:     index.NewEdgeRepo(idx),
						EdgeTypes: loaded.EdgeTypes,
					}
				},
				Logger: func(msg string) {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), msg)
				},
			})

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

			tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

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

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "edges: %d\nembed queue depth: %d\nreindex queue depth: %d\nlast reindex (unix ns): %s\n",
				result.EdgeCount, result.EmbedQueueDepth, result.ReindexQueueDepth, result.LastReindexAt)

			return nil
		},
	}

	return statusCmd
}
