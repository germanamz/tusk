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

// mcpLoggerFromFlags builds the daemon's stderr logger: Warn level by default,
// Debug when --verbose. It is never nil — a default daemon must surface
// background-component failures (a dead watcher, a stuck drainer) rather than
// run deaf. Logs go to stderr so they never corrupt the stdio JSON-RPC stream
// on stdout.
func mcpLoggerFromFlags(cmd *cobra.Command) *slog.Logger {
	verbose, _ := cmd.Flags().GetBool("verbose")

	return newLogger(cmd.ErrOrStderr(), verbose)
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
  sse     listens for SSE clients on --addr (default 127.0.0.1:8765, loopback)

The SSE transport exposes the full write surface (node/edge create, modify,
delete, and reset) with no authentication, so it binds loopback by default.
Binding a non-loopback address requires interactive confirmation.

The server holds the workspace open for the lifetime of the session, drains
the embed queue in the background, and watches the workspace for external
edits.

Worker pool: TUSK_EMBED_WORKERS overrides [embeddings] workers in tusk.toml;
both default to max(1, NumCPU/2). Setting the pool size to 0 turns this
instance into a read-only server — the embed drainer, reindex drainer, and
file watcher are all skipped, so another instance (or a scheduled tusk
reindex) must keep the index fresh.`,
		Example: `  # Default: stdio transport (Claude Code, Cursor, Zed)
  tusk mcp

  # SSE transport bound to loopback for browser-based clients
  tusk mcp --transport sse --addr 127.0.0.1:8765

  # Verify the workspace is healthy first
  tusk doctor && tusk mcp`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if guardErr := guardMCPBind(cmd, transport, addr); guardErr != nil {
				return guardErr
			}

			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			opts := []mcp.Option{
				mcp.WithAliasIntrospector(buildVerbIntrospector(cmd.Root())),
				mcp.WithLogger(mcpLoggerFromFlags(cmd)),
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

			bgErr := <-bgDone

			// The transport error is user-facing and takes priority; a background
			// component failure (a dead watcher, a stuck drainer) is surfaced only
			// when the transport itself exited cleanly so it is not masked.
			if transportErr != nil {
				return transportErr
			}

			return bgErr
		},
	}

	mcpCmd.Flags().StringVar(&transport, "transport", "stdio", "transport: stdio | sse")
	mcpCmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8765", "SSE listen address (loopback by default; only used when --transport sse)")

	return mcpCmd
}

// guardMCPBind refuses to start the SSE transport on a non-loopback address
// without explicit confirmation. The SSE transport exposes the full write
// surface (node/edge create, modify, delete, and reset) with no authentication,
// so an all-interfaces bind would hand write access to anyone who can reach the
// port. stdio and loopback binds pass through untouched.
func guardMCPBind(cmd *cobra.Command, transport, addr string) error {
	if transport != "sse" {
		return nil
	}

	if isLoopbackAddr(addr) {
		return nil
	}

	_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
		"warning: %q is not loopback; the MCP SSE server is unauthenticated and exposes write tools (node/edge create, modify, delete, and reset) to anyone who can reach it.\nProceed? [y/N] ",
		addr)

	var answer string

	_, _ = fmt.Fscanln(cmd.InOrStdin(), &answer)

	if answer != "y" && answer != "Y" {
		return fmt.Errorf("mcp: refusing to bind non-loopback address %q without confirmation", addr)
	}

	return nil
}
