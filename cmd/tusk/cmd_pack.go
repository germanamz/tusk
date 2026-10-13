package main

import "github.com/spf13/cobra"

func newPackCmd() *cobra.Command {
	packCmd := &cobra.Command{
		Use:   "pack",
		Short: "Install and manage type packs",
		Long: `Manage type packs.

A type pack is a bundle of node-type and edge-type declarations that
"tusk pack add" copies into the workspace manifest. Use packs to seed a
new workspace with sensible defaults (e.g. the "kanban" pack with a
ticket workflow) instead of writing the schema by hand.

A pack is named or a URL. Built-in packs (kanban, tags, vault) are
compiled into tusk, so adding one by name works offline; pass a full URL
(or a file:// URL for a local copy) to install one from elsewhere.`,
	}

	packCmd.AddCommand(newPackAddCmd())

	return packCmd
}
