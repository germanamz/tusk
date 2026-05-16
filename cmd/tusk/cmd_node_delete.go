package main

import (
	"fmt"
	"os"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newNodeDeleteCmd() *cobra.Command {
	deleteCmd := &cobra.Command{
		Use:   "delete <node-id>",
		Short: "Delete a node file and remove it from the index",
		Long: `Delete a node file from disk and remove it from the index.

Edges pointing at the deleted node remain in the index as dangling
references; "tusk doctor" will surface them. Use "tusk node move" if you
want to rename rather than remove.`,
		Example: `  # Delete a stale note
  tusk node delete notes/2024-old

  # See which edges are now dangling
  tusk doctor`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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

				if deleteErr := node.Delete(ws.Root, index.NewNodeRepo(store), index.NewEdgeRepo(store), args[0]); deleteErr != nil {
					return deleteErr
				}

				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Deleted %s\n", args[0])

				return nil
			})
		},
	}

	return deleteCmd
}
