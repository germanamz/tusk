package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/germanamz/tusk/internal/typepacks"
)

func newPackAddCmd() *cobra.Command {
	var force bool

	addCmd := &cobra.Command{
		Use:   "add <name-or-url>",
		Short: "Copy a type pack's declarations into tusk.toml",
		Long: `Copy a type pack's node and edge type declarations into tusk.toml.

The pack is a built-in name (kanban, tags, vault) or a URL. Built-in
names are fetched over the network from the project's published packs, so
adding one by name needs connectivity; pass a full URL (or a file:// URL)
to install from elsewhere.

Idempotent for a given pack: re-running with the same pack is a no-op
unless --force is set, in which case any colliding sections in tusk.toml
are removed before the pack is appended.`,
		Example: `  # Add the kanban pack and verify the manifest is still valid
  tusk pack add kanban
  tusk doctor

  # Re-add a pack that already exists, replacing collisions
  tusk pack add kanban --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, getCwdErr := os.Getwd()

			if getCwdErr != nil {
				return getCwdErr
			}

			addErr := typepacks.AddPack(cmd.Context(), args[0], force, cwd)

			if addErr == nil {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "pack add: applied %q to tusk.toml\n", args[0])

				return nil
			}

			// Print the failure before any os.Exit: SilenceErrors is set on
			// root and main.go prints only on the normal return path, which
			// os.Exit bypasses — without this the user gets a bare exit code
			// and no message.
			if code, ok := packAddExitCode(addErr); ok {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), addErr)

				os.Exit(code)
			}

			return addErr // cobra exits with 1 (printed by main.go)
		},
	}

	addCmd.Flags().BoolVar(&force, "force", false, "remove colliding sections from tusk.toml before appending the pack")

	return addCmd
}

// packAddExitCode maps a pack-add failure to a distinct process exit code for
// scripting: 2 for fetch/network failures, 3 for TOML/validation failures. The
// bool is false for every other error, which cobra reports as exit code 1.
func packAddExitCode(addErr error) (int, bool) {
	msg := addErr.Error()

	switch {
	case strings.Contains(msg, "fetch"):
		return 2, true
	case strings.Contains(msg, "invalid TOML"), strings.Contains(msg, "disallowed top-level"), strings.Contains(msg, "decode pack"):
		return 3, true
	}

	return 0, false
}
