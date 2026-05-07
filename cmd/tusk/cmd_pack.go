package main

import "github.com/spf13/cobra"

func newPackCmd() *cobra.Command {
	packCmd := &cobra.Command{
		Use:   "pack",
		Short: "Manage type packs",
	}

	packCmd.AddCommand(newPackAddCmd())

	return packCmd
}
