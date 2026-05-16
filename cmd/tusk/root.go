package main

import (
	"github.com/spf13/cobra"

	"github.com/germanamz/tusk/internal/version"
)

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "tusk",
		Short:         "Tusk — local-first agent brain",
		Long:          "Tusk indexes a markdown vault into a graph and serves structural and semantic queries.",
		Version:       version.Current,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "emit debug-level logs to stderr")

	rootCmd.AddCommand(newInitCmd())
	rootCmd.AddCommand(newNodeCmd())
	rootCmd.AddCommand(newReindexCmd())
	rootCmd.AddCommand(newEdgeCmd())
	rootCmd.AddCommand(newWatchCmd())
	rootCmd.AddCommand(newQueryCmd())
	rootCmd.AddCommand(newDoctorCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newMCPCmd())
	rootCmd.AddCommand(newPackCmd())

	return rootCmd
}
