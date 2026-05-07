package main

import (
	"fmt"
	"io"
	"os"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newNodeCreateCmd() *cobra.Command {
	var (
		nodeType string
		title    string
		relPath  string
		setFlags []string
	)

	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new node file and index it",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Accept path as either --path flag or positional argument.
			if relPath == "" && len(args) > 0 {
				relPath = args[0]
			}

			if relPath == "" {
				return fmt.Errorf("--path is required")
			}

			if nodeType == "" {
				return fmt.Errorf("--type is required")
			}

			cwd, getCwdErr := os.Getwd()

			if getCwdErr != nil {
				return getCwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			loaded, loadErr := manifest.Load(ws.ManifestPath)

			if loadErr != nil {
				return loadErr
			}

			engine, buildErr := newBehaviorEngine(loaded)

			if buildErr != nil {
				return buildErr
			}

			body, readErr := readBodyOrEmpty(cmd.InOrStdin())

			if readErr != nil {
				return readErr
			}

			setProps, setErr := parseSetFlags(setFlags)

			if setErr != nil {
				return setErr
			}

			return withWorkspaceLock(ws, func() error {
				store, openErr := index.Open(ws.IndexPath)

				if openErr != nil {
					return openErr
				}

				defer store.Close()

				service := node.NewServiceWithBehaviors(
					ws.Root,
					index.NewNodeRepo(store),
					index.NewEdgeRepo(store),
					loaded.EdgeTypes,
					index.NewEmbedQueueRepo(store),
					nil,
					nil,
					engine,
					index.NewWorkflowDriftRepo(store),
					cmd.ErrOrStderr(),
				)

				created, createErr := service.Create(node.CreateInput{
					RelPath:    relPath,
					Type:       nodeType,
					Title:      title,
					Properties: setProps,
					Body:       body,
				})

				if createErr != nil {
					return createErr
				}

				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Created %s (id=%s)\n", created.Path, created.ID)

				return nil
			})
		},
	}

	createCmd.Flags().StringVar(&nodeType, "type", "", "node type (e.g. ticket, note)")
	createCmd.Flags().StringVar(&title, "title", "", "optional node title")
	createCmd.Flags().StringVar(&relPath, "path", "", "workspace-relative path with extension (e.g. notes/hello.md)")
	createCmd.Flags().StringArrayVar(&setFlags, "prop", nil, "set property: --prop key=value (repeatable)")

	return createCmd
}

// readBodyOrEmpty reads markdown body from stdin if there is piped data; returns
// an empty body otherwise. (Plan 1b accepts an empty body.)
func readBodyOrEmpty(stdin io.Reader) ([]byte, error) {
	stat, statOK := stdin.(*os.File)

	if !statOK {
		return []byte(""), nil
	}

	fileInfo, fileErr := stat.Stat()

	if fileErr != nil {
		return []byte(""), nil
	}

	// If stdin is a terminal (character device), no piped body.
	if (fileInfo.Mode() & os.ModeCharDevice) != 0 {
		return []byte(""), nil
	}

	body, readErr := io.ReadAll(stat)

	if readErr != nil {
		return nil, readErr
	}

	return body, nil
}
