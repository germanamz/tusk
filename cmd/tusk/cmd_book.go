package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/germanamz/tusk/internal/bookview"
	"github.com/germanamz/tusk/internal/mcp"
	"github.com/spf13/cobra"
)

func newBookCmd() *cobra.Command {
	var (
		addr     string
		autoOpen bool
	)

	bookCmd := &cobra.Command{
		Use:   "book",
		Short: "Serve a read-only reading view of the vault",
		Long: `Serve a local, read-only reading UI for the vault: rendered node
documents (with math, mermaid diagrams, and images), semantic search, and
graph-expansion navigation, kept live as files change.

The server binds to loopback by default. It does not open a browser
automatically: press space in this terminal to open it, or pass --open.`,
		Example: `  # Serve on 127.0.0.1:7474 and press space to open
  tusk book

  # Open the browser automatically
  tusk book --open

  # Bind a specific loopback port
  tusk book --addr 127.0.0.1:9001`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isLoopbackAddr(addr) {
				if !confirmNonLoopback(cmd, addr, "book") {
					return fmt.Errorf("book: refusing to bind non-loopback address %q without confirmation", addr)
				}
			}

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			return serveWebUI(ctx, cmd, bookWebUIConfig(addr, autoOpen))
		},
	}

	bookCmd.Flags().StringVar(&addr, "addr", bookview.DefaultAddr, "loopback listen address")
	bookCmd.Flags().BoolVar(&autoOpen, "open", false, "open the browser automatically at startup")

	return bookCmd
}

// bookWebUIConfig describes the book view to the shared serveWebUI spine: how
// to build the book server once the runtime is open, and how to render its
// status line. Tests reach for it directly so they can add a ready hook.
func bookWebUIConfig(addr string, autoOpen bool) webUIConfig {
	return webUIConfig{
		Name:     "book",
		Addr:     addr,
		AutoOpen: autoOpen,
		Title:    "tusk book",
		BuildServer: func(rt *mcp.Runtime) webViewServer {
			return bookview.New(bookview.Deps{
				Root:         rt.Root,
				Nodes:        rt.Nodes,
				Edges:        rt.Edges,
				Search:       bookview.NewSearcher(rt.Index.DB(), rt.Manifest, rt.Embedder, rt.Embeddings, rt.Nodes, rt.Edges, rt.Root),
				Related:      bookview.NewRelated(rt.Edges, rt.Manifest, rt.Nodes),
				Meta:         rt.Meta,
				Logger:       rt.Logger,
				AllowedHosts: deriveAllowedHosts(addr),
			})
		},
		StatusLine: statusLine,
	}
}
