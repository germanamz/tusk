// Package webapp serves the unified `tusk web` app: the 3D graph view and the
// reading view behind one loopback HTTP server, one embedded frontend, one
// Host-header guard, and one Content-Security-Policy. It composes the graphview
// and bookview API providers — mounting their JSON + SSE routes under
// /api/graph/* and /api/read/* respectively — and never opens the workspace or
// imports internal/mcp itself; the command layer builds the two views' Deps
// from an open runtime and hands them here.
package webapp

import (
	"github.com/germanamz/tusk/internal/bookview"
	"github.com/germanamz/tusk/internal/graphview"
)

// DefaultAddr is the loopback bind address for `tusk web`. It reuses the graph
// view's historical port so the unified app answers where `tusk graph` always
// has.
const DefaultAddr = graphview.DefaultAddr

// Deps bundles the two view configurations plus the shared Host-header
// allowlist. The command layer builds graphview.Deps and bookview.Deps from an
// open runtime exactly as `tusk graph` and `tusk book` did; webapp owns the
// guard, CSP, static frontend, and healthz that the two views used to own
// individually.
type Deps struct {
	Graph graphview.Deps
	Book  bookview.Deps

	// AllowedHosts extends the Host-header guard beyond loopback and
	// "localhost". A confirmed non-loopback bind passes the bound hostname here;
	// a single "*" entry disables the guard (the user accepted network
	// exposure). Empty means loopback-only.
	AllowedHosts []string
}
