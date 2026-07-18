package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/germanamz/tusk/internal/bookview"
	"github.com/germanamz/tusk/internal/graphview"
	"github.com/germanamz/tusk/internal/mcp"
	"github.com/germanamz/tusk/internal/webapp"
	"github.com/germanamz/tusk/internal/webui"
	"github.com/spf13/cobra"
)

func newWebCmd() *cobra.Command {
	var (
		addr     string
		autoOpen bool
		view     string
	)

	webCmd := &cobra.Command{
		Use:   "web",
		Short: "Serve the unified web app: 3D graph + reading views",
		Long: `Serve a local, read-only web app for the vault, combining the 3D graph
view and the reading view behind one server, and keep it live as files change.

Switch between the two views from the top bar, or deep-link a view directly:
"/" opens the graph, "/read" opens the reader. A light/dark theme toggle in the
header follows your system by default and remembers your choice.

The graph view groups nodes by a configurable cluster lens (set under
[graph.cluster] in tusk.toml), sizes and brightens them by degree, and lets you
filter by type or edge kind, inspect neighbors, and walk sub-units. The reading
view renders node documents (math, mermaid diagrams, images), offers semantic
search, and surfaces graph-expansion neighbors.

The server binds to loopback by default. It does not open a browser
automatically: press space in this terminal to open it, or pass --open.`,
		Example: `  # Serve on 127.0.0.1:7373 and press space to open
  tusk web

  # Open the browser automatically on the reading view
  tusk web --open --view read

  # Bind a specific loopback port
  tusk web --addr 127.0.0.1:9000`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if view != "graph" && view != "read" {
				return fmt.Errorf("web: invalid --view %q (want \"graph\" or \"read\")", view)
			}

			return runWeb(cmd, addr, autoOpen, view)
		},
	}

	webCmd.Flags().StringVar(&addr, "addr", webapp.DefaultAddr, "loopback listen address")
	webCmd.Flags().BoolVar(&autoOpen, "open", false, "open the browser automatically at startup")
	webCmd.Flags().StringVar(&view, "view", "graph", "initial view to open: graph|read")

	return webCmd
}

// runWeb is the shared entry for `tusk web` and its deprecated `graph`/`book`
// aliases: it enforces the loopback guard, wires signal handling, and serves the
// unified app pinned to the requested initial view.
func runWeb(cmd *cobra.Command, addr string, autoOpen bool, view string) error {
	if !isLoopbackAddr(addr) {
		if !confirmNonLoopback(cmd, addr, "web") {
			return fmt.Errorf("web: refusing to bind non-loopback address %q without confirmation", addr)
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return serveWebUI(ctx, cmd, webWebUIConfig(addr, autoOpen, view))
}

// webWebUIConfig describes the unified web app to the shared serveWebUI spine:
// how to build the webapp server (composing the graph and reading views) once
// the runtime is open, which route to open first, and how to render the status
// line. Tests reach for it directly so they can add a ready hook.
func webWebUIConfig(addr string, autoOpen bool, view string) webUIConfig {
	return webUIConfig{
		Name:     "web",
		Addr:     addr,
		AutoOpen: autoOpen,
		Title:    "tusk web",
		OpenPath: viewOpenPath(view),
		BuildServer: func(rt *mcp.Runtime) webViewServer {
			return webapp.New(webapp.Deps{
				Graph: graphview.Deps{
					Root:       rt.Root,
					Nodes:      rt.Nodes,
					Edges:      rt.Edges,
					Render:     graphview.NewRenderer(rt.Root, rt.Nodes),
					Query:      graphview.NewQuerier(rt.Index.DB(), rt.Manifest, rt.Embedder, rt.Embeddings, rt.Nodes, rt.Edges, rt.Root),
					Changes:    webui.NewChangeSource(rt.Root, rt.Meta),
					Manifest:   rt.Manifest,
					Embeddings: rt.Embeddings,
					Logger:     rt.Logger,
				},
				Book: bookview.Deps{
					Root:    rt.Root,
					Nodes:   rt.Nodes,
					Edges:   rt.Edges,
					Search:  bookview.NewSearcher(rt.Index.DB(), rt.Manifest, rt.Embedder, rt.Embeddings, rt.Nodes, rt.Edges, rt.Root),
					Related: bookview.NewRelated(rt.Edges, rt.Manifest, rt.Nodes),
					Meta:    rt.Meta,
					Logger:  rt.Logger,
				},
				AllowedHosts: deriveAllowedHosts(addr),
			})
		},
		StatusLine: statusLine,
	}
}

// viewOpenPath maps a --view value to the SPA route the browser opens: the
// reading view deep-links "/read", everything else opens the graph at the app
// root.
func viewOpenPath(view string) string {
	if view == "read" {
		return "/read"
	}

	return ""
}
