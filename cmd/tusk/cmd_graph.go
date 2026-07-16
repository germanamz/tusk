package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/germanamz/tusk/internal/graphview"
	"github.com/germanamz/tusk/internal/mcp"
	"github.com/germanamz/tusk/internal/webui"
	"github.com/spf13/cobra"
)

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

			return serveWebUI(ctx, cmd, graphWebUIConfig(addr, autoOpen))
		},
	}

	graphCmd.Flags().StringVar(&addr, "addr", graphview.DefaultAddr, "loopback listen address")
	graphCmd.Flags().BoolVar(&autoOpen, "open", false, "open the browser automatically at startup")

	return graphCmd
}

// graphWebUIConfig describes the graph view to the shared serveWebUI spine: how
// to build the graph server once the runtime is open, and how to render its
// status line. Tests reach for it directly so they can add a ready hook.
func graphWebUIConfig(addr string, autoOpen bool) webUIConfig {
	return webUIConfig{
		Name:     "graph",
		Addr:     addr,
		AutoOpen: autoOpen,
		Title:    "tusk graph",
		BuildServer: func(rt *mcp.Runtime) webViewServer {
			return graphview.New(graphview.Deps{
				Root:         rt.Root,
				Nodes:        rt.Nodes,
				Edges:        rt.Edges,
				Render:       graphview.NewRenderer(rt.Root, rt.Nodes),
				Query:        graphview.NewQuerier(rt.Index.DB(), rt.Manifest, rt.Embedder, rt.Embeddings, rt.Nodes, rt.Edges, rt.Root),
				Changes:      webui.NewChangeSource(rt.Root, rt.Meta),
				Manifest:     rt.Manifest,
				Embeddings:   rt.Embeddings,
				Logger:       rt.Logger,
				AllowedHosts: deriveAllowedHosts(addr),
			})
		},
		StatusLine: statusLine,
	}
}
