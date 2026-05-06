package main

import "github.com/spf13/cobra"

const versionString = "v1.0.0-dev"

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "tusk",
		Short:         "Tusk — local-first agent brain",
		Long:          "Tusk indexes a markdown vault into a graph and serves structural and semantic queries.",
		Version:       versionString,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.AddCommand(newInitCmd())
	rootCmd.AddCommand(newNodeCmd())
	rootCmd.AddCommand(newReindexCmd())
	rootCmd.AddCommand(newEdgeCmd())
	rootCmd.AddCommand(newWatchCmd())
	rootCmd.AddCommand(newQueryCmd())
	rootCmd.AddCommand(newDoctorCmd())

	return rootCmd
}
