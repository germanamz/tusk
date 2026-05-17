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
		Short: "Copy a built-in type pack's declarations into tusk.toml",
		Long: `Copy a built-in type pack's node and edge type declarations into
tusk.toml.

Idempotent for a given pack: re-running with the same pack is a no-op
unless --force is set, in which case any colliding sections in tusk.toml
are removed before the pack is appended.`,
		Example: `  # Add the gtd pack and verify the manifest is still valid
  tusk pack add gtd
  tusk doctor

  # Re-add a pack that already exists, replacing collisions
  tusk pack add gtd --force`,
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

			msg := addErr.Error()

			switch {
			case strings.Contains(msg, "fetch"):
				os.Exit(2)
			case strings.Contains(msg, "invalid TOML"), strings.Contains(msg, "disallowed top-level"), strings.Contains(msg, "decode pack"):
				os.Exit(3)
			}

			return addErr // cobra exits with 1
		},
	}

	addCmd.Flags().BoolVar(&force, "force", false, "remove colliding sections from tusk.toml before appending the pack")

	return addCmd
}
