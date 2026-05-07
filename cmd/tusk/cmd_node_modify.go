package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newNodeModifyCmd() *cobra.Command {
	var (
		setFlags   []string
		unsetFlags []string
	)

	modifyCmd := &cobra.Command{
		Use:   "modify <id>",
		Short: "Modify a node's frontmatter properties",
		Args:  cobra.ExactArgs(1),
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

			setProps, setErr := parseSetFlags(setFlags)

			if setErr != nil {
				return setErr
			}

			engine, buildErr := newBehaviorEngine(loaded)

			if buildErr != nil {
				return buildErr
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
					loaded.NodeTypes,
					index.NewPropertyDriftRepo(store),
					engine,
					index.NewWorkflowDriftRepo(store),
					cmd.ErrOrStderr(),
				)

				modified, modifyErr := service.Modify(node.ModifyInput{
					ID:        args[0],
					SetProps:  setProps,
					UnsetKeys: unsetFlags,
				})

				if modifyErr != nil {
					return modifyErr
				}

				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Modified %s\n", modified.ID)

				return nil
			})
		},
	}

	modifyCmd.Flags().StringArrayVar(&setFlags, "prop", nil, "set property: --prop key=value (repeatable)")
	modifyCmd.Flags().StringArrayVar(&unsetFlags, "unset", nil, "unset property: --unset key (repeatable)")

	return modifyCmd
}

// parseSetFlags converts ["k=v", "n=42", "b=true"] into a map[string]any with
// best-effort scalar typing (int, bool, then string).
func parseSetFlags(flags []string) (map[string]any, error) {
	props := map[string]any{}

	for _, raw := range flags {
		eq := strings.IndexByte(raw, '=')

		if eq <= 0 {
			return nil, fmt.Errorf("--prop: expected key=value, got %q", raw)
		}

		key := raw[:eq]
		value := raw[eq+1:]

		if intValue, parseErr := strconv.Atoi(value); parseErr == nil {
			props[key] = intValue

			continue
		}

		if boolValue, parseErr := strconv.ParseBool(value); parseErr == nil {
			props[key] = boolValue

			continue
		}

		props[key] = value
	}

	return props, nil
}
