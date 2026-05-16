package main

import (
	"github.com/spf13/cobra"

	"github.com/germanamz/tusk/internal/version"
)

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "tusk",
		Short: "Local-first agent brain: index a markdown vault into a graph",
		Long: `Tusk turns a directory of markdown files into a schema-validated,
semantically-indexed graph. Files (markdown + tusk.toml) are the source of
truth; git is the history; tusk is the indexer and retrieval engine.

Run "tusk init" to create a workspace, "tusk node create" to add content,
"tusk reindex" or "tusk watch" to keep the index live, "tusk query" /
"tusk node list" to retrieve, and "tusk mcp" to expose the same surface to
an MCP-compatible agent.

Most read/write verbs are also exposed as MCP tools (tusk_query,
tusk_node_create, …) so agents can use the same surface without a shell.`,
		Example: `  # Bootstrap a fresh vault and verify it
  tusk init --name my-brain
  tusk doctor

  # Index existing markdown files on disk
  tusk reindex

  # Run the MCP server for Claude Code / Cursor
  tusk mcp`,
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
