package main

import "github.com/spf13/cobra"

func newPackCmd() *cobra.Command {
	packCmd := &cobra.Command{
		Use:   "pack",
		Short: "Install and manage built-in type packs",
		Long: `Manage type packs.

A type pack is a bundle of node-type and edge-type declarations that
"tusk pack add" copies into the workspace manifest. Use packs to seed a
new workspace with sensible defaults (e.g. a "gtd" pack with task and
project types) instead of writing the schema by hand.`,
	}

	packCmd.AddCommand(newPackAddCmd())

	return packCmd
}
