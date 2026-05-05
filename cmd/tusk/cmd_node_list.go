package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newNodeListCmd() *cobra.Command {
	var typeFilter string

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List nodes from the index",
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

			service := node.NewService(ws.Root, index.NewNodeRepo(store))

			nodes, listErr := service.List(node.ListFilter{Type: typeFilter})

			if listErr != nil {
				return listErr
			}

			tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

			_, _ = fmt.Fprintln(tab, "ID\tTYPE\tTITLE\tPATH")

			for _, item := range nodes {
				_, _ = fmt.Fprintf(tab, "%s\t%s\t%s\t%s\n", item.ID, item.Type, item.Title, item.Path)
			}

			return tab.Flush()
		},
	}

	listCmd.Flags().StringVar(&typeFilter, "type", "", "filter by node type (exact match)")

	return listCmd
}
