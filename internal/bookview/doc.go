// Package bookview provides the `tusk book` reading UI's read-only HTTP API:
// it serves vault nodes as formatted documents (math, diagrams, images,
// navigable wikilinks) and drives the existing semantic-search and
// graph-expansion machinery from the browser.
//
// It is a pure API provider. A parent server mounts its routes under a URL
// prefix via RegisterRoutes and owns the surrounding policy — the Host-header
// guard, security headers, static frontend, and healthz all live in the parent.
//
// Every route is a read; the package never writes to the vault. It never opens
// the workspace either — the command layer hands it an already-open one through
// Deps, so handlers stay testable with fakes. Rendering markdown to HTML is the
// frontend's job: the node route returns the raw body and the browser renders it.
//
// The SSE hub and change source are shared with `tusk graph` via internal/webui.
package bookview
