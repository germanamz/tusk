// Package bookview hosts the `tusk book` reading UI: a local, read-only HTTP
// server that serves vault nodes as formatted documents (math, diagrams,
// images, navigable wikilinks) and drives the existing semantic-search and
// graph-expansion machinery from the browser.
//
// Every route is a read; the package never writes to the vault. It never opens
// the workspace either — the command layer hands it an already-open one through
// Deps, so handlers stay testable with fakes. Rendering markdown to HTML is the
// frontend's job: /api/node returns the raw body and the browser renders it.
//
// The serving scaffold (Host-header guard, SSE hub, change source, static
// assets) is shared with `tusk graph` via internal/webui.
package bookview
