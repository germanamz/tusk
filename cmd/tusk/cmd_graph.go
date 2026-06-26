package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/germanamz/tusk/internal/graphview"
	"github.com/germanamz/tusk/internal/mcp"
	"github.com/spf13/cobra"
)

type graphConfig struct {
	addr     string
	autoOpen bool
	ready    func(addr string) // optional; called once listening (tests)
}

func newGraphCmd() *cobra.Command {
	var (
		addr     string
		autoOpen bool
	)

	graphCmd := &cobra.Command{
		Use:   "graph",
		Short: "Serve an interactive 3D graph view of the vault",
		Long: `Serve a local, read-only 3D graph of the vault (nodes + edges) and
keep it live as files change.

Nodes are grouped by a configurable cluster lens set under [graph.cluster]
in tusk.toml. The lens producer (by = type | property | ancestor | community)
assigns each node a group key; an absent block defaults to by = "type",
reproducing the original color-by-type behavior.

The group drives three visual channels:

  Color  — one hue per distinct group, listed in the legend.
  Huddle — set huddle = true to pull same-group nodes toward a shared
           anchor so clusters huddle into visible lobes.
  Hull   — set hull = true to wrap each group in a translucent 3D
           convex-hull boundary; groups with fewer than 4 members are
           silently skipped.

Degree (total connections, incoming plus outgoing) still drives node size
and brightness independently of the group lens, so hubs stand out as large,
bright nodes while leaf nodes recede. Use the facet bar to filter by type
or edge kind, click a node to inspect it and highlight its neighbors, and
drag to rotate, alt+drag to pan, scroll to zoom.

The server binds to loopback by default. It does not open a browser
automatically: press space in this terminal to open it, or pass --open.`,
		Example: `  # Serve on 127.0.0.1:7373 and press space to open
  tusk graph

  # Open the browser automatically
  tusk graph --open

  # Bind a specific loopback port
  tusk graph --addr 127.0.0.1:9000`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isLoopbackAddr(addr) {
				if !confirmNonLoopback(cmd, addr) {
					return fmt.Errorf("graph: refusing to bind non-loopback address %q without confirmation", addr)
				}
			}

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			return serveGraph(ctx, cmd, graphConfig{addr: addr, autoOpen: autoOpen})
		},
	}

	graphCmd.Flags().StringVar(&addr, "addr", graphview.DefaultAddr, "loopback listen address")
	graphCmd.Flags().BoolVar(&autoOpen, "open", false, "open the browser automatically at startup")

	return graphCmd
}

// serveGraph opens the runtime, starts background maintenance, serves the graph
// handler, and runs the foreground console until ctx is cancelled.
func serveGraph(ctx context.Context, cmd *cobra.Command, cfg graphConfig) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		return cwdErr
	}

	opts := []mcp.Option{mcp.WithAliasIntrospector(buildVerbIntrospector(cmd.Root()))}
	if logger := mcpLoggerFromFlags(cmd); logger != nil {
		opts = append(opts, mcp.WithLogger(logger))
	}

	runtime, openErr := mcp.Open(cwd, opts...)
	if openErr != nil {
		return openErr
	}

	defer runtime.Close()

	deps := graphview.Deps{
		Root:       runtime.Root,
		Nodes:      runtime.Nodes,
		Edges:      runtime.Edges,
		Render:     graphview.NewRenderer(runtime.Root, runtime.Nodes),
		Query:      graphview.NewQuerier(runtime.Index.DB(), runtime.Manifest, runtime.Embedder, runtime.Embeddings, runtime.Nodes, runtime.Edges, runtime.Root),
		Changes:    graphview.NewChangeSource(runtime.Root, runtime.Meta),
		Manifest:   runtime.Manifest,
		Embeddings: runtime.Embeddings,
		Logger:     mcpLoggerFromFlags(cmd),
	}

	viewServer := graphview.New(deps)

	// Background maintenance (watcher + drainers) and the SSE hub.
	bgServer := mcp.NewServer(runtime)

	bgDone := make(chan error, 1)
	go func() { bgDone <- bgServer.RunBackground(ctx) }()

	go viewServer.Run(ctx)

	listener, listenErr := net.Listen("tcp", cfg.addr)
	if listenErr != nil {
		cancel()
		<-bgDone

		return fmt.Errorf("graph: listen %s: %w", cfg.addr, listenErr)
	}

	boundURL := "http://" + listener.Addr().String()

	if cfg.ready != nil {
		cfg.ready(listener.Addr().String())
	}

	httpServer := &http.Server{Handler: viewServer.Handler(), ReadHeaderTimeout: 10 * time.Second}

	serveErrCh := make(chan error, 1)
	go func() { serveErrCh <- httpServer.Serve(listener) }()

	if cfg.autoOpen {
		_ = openBrowser(boundURL)
	}

	// Tilt-style foreground console (status line + keypress loop).
	runConsole(ctx, cancel, cmd, viewServer, runtime, boundURL)

	cancel() // unblock RunBackground + viewServer.Run before draining

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)

	<-bgDone

	if serveErr := <-serveErrCh; serveErr != nil && serveErr != http.ErrServerClosed {
		return serveErr
	}

	return nil
}

// isLoopbackAddr reports whether addr binds only the loopback interface.
func isLoopbackAddr(addr string) bool {
	host, _, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return false
	}

	if host == "localhost" {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

func confirmNonLoopback(cmd *cobra.Command, addr string) bool {
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %q is not loopback; the graph server is unauthenticated and read-only but would be reachable from your network.\nProceed? [y/N] ", addr)

	var answer string
	_, _ = fmt.Fscanln(cmd.InOrStdin(), &answer)

	return answer == "y" || answer == "Y"
}
