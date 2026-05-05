package main

import "github.com/spf13/cobra"

func newNodeCmd() *cobra.Command {
	nodeCmd := &cobra.Command{
		Use:   "node",
		Short: "Manage individual nodes (create, get, list)",
	}

	nodeCmd.AddCommand(newNodeCreateCmd())
	nodeCmd.AddCommand(newNodeGetCmd())
	nodeCmd.AddCommand(newNodeListCmd())

	return nodeCmd
}
