package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/germanamz/tusk/internal/node"
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
		Long: `Modify a node's frontmatter properties without touching its body.

Use --prop key=value (repeatable) to set values and --unset key
(repeatable) to remove them. Values are typed the same way as in
"node create": int, then bool, then float, then string.

The operation coordinates with concurrent watchers and other tusk
processes via a per-file lease, so safe interleaving is preserved
without holding a workspace-wide lock.`,
		Example: `  # Change a ticket's status and priority
  tusk node modify tickets/T-001 --prop status=in-progress --prop priority=2

  # Remove a property entirely
  tusk node modify tickets/T-001 --unset blocked-by`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, loaded, resolveErr := resolveWorkspace()

			if resolveErr != nil {
				return resolveErr
			}
			setProps, setErr := parseSetFlags(setFlags)

			if setErr != nil {
				return setErr
			}

			engine, buildErr := newBehaviorEngine(loaded)

			if buildErr != nil {
				return buildErr
			}

			store, openErr := openStore(cmd, ws.Root, ws.IndexPath, loaded)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			service := newNodeService(ws, store, loaded, engine, cmd.ErrOrStderr())

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
		},
	}

	modifyCmd.Flags().StringArrayVar(&setFlags, "prop", nil, "set property: --prop key=value (repeatable)")
	modifyCmd.Flags().StringArrayVar(&unsetFlags, "unset", nil, "unset property: --unset key (repeatable)")

	return modifyCmd
}

// parseSetFlags converts ["k=v", "n=42", "b=true", "x=3.14"] into a
// map[string]any with best-effort scalar typing (int, bool, float, then
// string). Whole numbers stay int; a non-whole decimal like "3.14" becomes a
// float64 so a declared float property validates and renders as a number.
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

		if floatValue, parseErr := strconv.ParseFloat(value, 64); parseErr == nil {
			props[key] = floatValue

			continue
		}

		props[key] = value
	}

	return props, nil
}
