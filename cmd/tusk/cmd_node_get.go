package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newNodeGetCmd() *cobra.Command {
	getCmd := &cobra.Command{
		Use:   "get <node-id>",
		Short: "Print the markdown file for a node by id",
		Long: `Print the full markdown file (frontmatter + body) for a node by id.

The node id is the workspace-relative path without extension (e.g. a node
file at notes/hello.md has id "notes/hello"). Output goes to stdout
verbatim — useful for piping into editors, less, or another tusk command.`,
		Example: `  # Print a node
  tusk node get notes/hello

  # Open in $EDITOR (round-trip through a temp file)
  tusk node get notes/hello > /tmp/hello.md && $EDITOR /tmp/hello.md`,
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

			store, openErr := index.Open(ws.IndexPath)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			service := node.NewService(ws.Root, index.NewNodeRepo(store))

			loaded, getErr := service.Get(args[0])

			if getErr != nil {
				return getErr
			}

			rendered, renderErr := os.ReadFile(filepath.Join(ws.Root, loaded.Path))

			if renderErr != nil {
				return renderErr
			}

			_, _ = fmt.Fprint(cmd.OutOrStdout(), string(rendered))

			return nil
		},
	}

	return getCmd
}
