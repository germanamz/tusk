package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/render"
	"github.com/spf13/cobra"
)

func newNodeRenderCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "render <node-id>",
		Short: "Render a node's content as plain text (tags / markup stripped)",
		Long: `Render a node's content as plain text.

HTML nodes have their tags stripped and entities decoded; markdown nodes have
their markup removed. The output is "just the words" — useful for piping a node
into a tool that wants prose, not markup.

The node id is the workspace-relative path: markdown nodes drop the extension
(notes/hello.md has id "notes/hello"), HTML nodes retain it (page.html has id
"page.html"). Render is read-only — it never touches files or index state.`,
		Example: `  # Strip markdown markup
  tusk node render notes/hello

  # Strip HTML tags
  tusk node render page.html`,
		Args: cobra.ExactArgs(1),
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

			row, getErr := index.NewNodeRepo(store).Get(args[0])

			if getErr != nil {
				return getErr
			}

			body, readErr := os.ReadFile(filepath.Join(ws.Root, row.Path))

			if readErr != nil {
				return readErr
			}

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), render.NodeText(row.Path, body))

			return nil
		},
	}
}
