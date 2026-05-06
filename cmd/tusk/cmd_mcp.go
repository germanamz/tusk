package main

import (
	"fmt"
	"os"

	"github.com/germanamz/tusk/internal/mcp"
	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	var (
		transport string
		addr      string
	)

	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run the long-running MCP server (stdio or SSE)",
		Long: `Run the Tusk MCP server.

Transports:
  stdio   reads JSON-RPC over stdin, writes over stdout (default)
  sse     listens for SSE clients on --addr (default :8765)

The server holds the workspace open for the lifetime of the session, drains
the embed queue in the background, and watches the workspace for external
edits.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			runtime, openErr := mcp.Open(cwd)

			if openErr != nil {
				return openErr
			}

			defer runtime.Close()

			srv := mcp.NewServer(runtime)

			switch transport {
			case "stdio", "":
				return srv.ServeStdio()
			case "sse":
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "tusk mcp: SSE listening on %s\n", addr)

				return srv.ServeSSE(addr)
			}

			return fmt.Errorf("--transport: unknown value %q (want stdio|sse)", transport)
		},
	}

	mcpCmd.Flags().StringVar(&transport, "transport", "stdio", "transport: stdio | sse")
	mcpCmd.Flags().StringVar(&addr, "addr", ":8765", "SSE listen address (only used when --transport sse)")

	return mcpCmd
}
