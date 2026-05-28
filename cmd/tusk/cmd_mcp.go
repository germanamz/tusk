package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/germanamz/tusk/internal/mcp"
	"github.com/spf13/cobra"
)

// mcpLoggerFromFlags returns a verbose logger when --verbose is set, else nil.
// Returning nil keeps Runtime.Logger nil so existing nil-checks short-circuit.
func mcpLoggerFromFlags(cmd *cobra.Command) *slog.Logger {
	verbose, _ := cmd.Flags().GetBool("verbose")

	if !verbose {
		return nil
	}

	return newLogger(cmd.ErrOrStderr(), true)
}

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
edits.

Worker pool: TUSK_EMBED_WORKERS overrides [embeddings] workers in tusk.toml;
both default to max(1, NumCPU/2). Setting the pool size to 0 turns this
instance into a read-only server — the embed and reindex drainers are not
started, so another instance (or a scheduled tusk reindex) must keep the
index fresh. The file watcher still runs and still enqueues work for
whichever instance is draining.`,
		Example: `  # Default: stdio transport (Claude Code, Cursor, Zed)
  tusk mcp

  # SSE transport bound to loopback for browser-based clients
  tusk mcp --transport sse --addr 127.0.0.1:8765

  # Verify the workspace is healthy first
  tusk doctor && tusk mcp`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			opts := []mcp.Option{
				mcp.WithAliasIntrospector(buildVerbIntrospector(cmd.Root())),
			}

			if logger := mcpLoggerFromFlags(cmd); logger != nil {
				opts = append(opts, mcp.WithLogger(logger))
			}

			runtime, openErr := mcp.Open(cwd, opts...)

			if openErr != nil {
				return openErr
			}

			defer runtime.Close()

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			serverInstance := mcp.NewServer(runtime)

			bgDone := make(chan error, 1)

			go func() {
				bgDone <- serverInstance.RunBackground(ctx)
			}()

			var transportErr error

			switch transport {
			case "stdio", "":
				transportErr = serverInstance.ServeStdio()
			case "sse":
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "tusk mcp: SSE listening on %s\n", addr)
				transportErr = serverInstance.ServeSSE(addr)
			default:
				cancel()
				<-bgDone

				return fmt.Errorf("--transport: unknown value %q (want stdio|sse)", transport)
			}

			cancel()

			<-bgDone

			return transportErr
		},
	}

	mcpCmd.Flags().StringVar(&transport, "transport", "stdio", "transport: stdio | sse")
	mcpCmd.Flags().StringVar(&addr, "addr", ":8765", "SSE listen address (only used when --transport sse)")

	return mcpCmd
}
