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
		Short: "Fetch and merge a type pack into tusk.toml",
		Args:  cobra.ExactArgs(1),
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
