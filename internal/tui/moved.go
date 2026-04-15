package tui

import (
	"fmt"

	"github.com/spf13/cobra"
)

// registerMovedStubs registers hidden stub commands for flat task verbs that
// have moved under `tusk task`. Each stub returns an error pointing the user
// at the new invocation. Subsequent v0.11 phases extend the map below.
func (a *App) registerMovedStubs() {
	moved := map[string]string{
		"add":    "task create",
		"info":   "task get",
		"list":   "task list",
		"modify": "task modify",
		"tree":   "task tree",
	}

	for old, newPath := range moved {
		a.root.AddCommand(&cobra.Command{
			Use:                old,
			Hidden:             true,
			DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				return fmt.Errorf("unknown command %q; did you mean 'tusk %s'?", old, newPath)
			},
		})
	}
}
