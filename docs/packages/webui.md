---
type: package
title: internal/webui — shared local view-server scaffold
import-path: github.com/germanamz/tusk/internal/webui
status: stable
---

# internal/webui

Shared serving scaffold behind `tusk web`, the local, read-only vault web
app. `internal/webapp` composes the two view providers (`internal/graphview`
+ `internal/bookview`) behind one loopback server and builds on this package
for the parts that don't vary: a DNS-rebinding Host-header guard, a generic
SSE broadcast hub, the reindex/epoch change signal, and the file-level
neighbor projection graphview's node-detail payload is built from (bookview
re-implements its own instead — see Notes below). Neither view opens the
workspace itself; each receives an already-open handle through its own
`Deps`. The guard is now driven by `internal/webapp` (which serves the
embedded frontend through its own SPA handler); the SSE hub and change
source stay per-view.

## Public surface

- `HostGuard` / `NewHostGuard(allowedHosts []string) *HostGuard` — the
  DNS-rebinding Host-header allowlist. Loopback and `"localhost"` always
  pass; a caller-supplied host list extends it; a single `"*"` entry
  disables the guard entirely (the user accepted network exposure).
  `(*HostGuard).Wrap(next http.Handler) http.Handler` wraps a handler,
  answering 403 to a request whose Host header doesn't match.
  `(*HostGuard).Allowed(hostHeader string) bool` is the underlying
  predicate.
- `Hub` / `HubOptions` / `NewHub(opts HubOptions) *Hub` — a generic SSE
  broadcast hub. `HubOptions` names the SSE event (`EventName`; graph
  broadcasts as `"graph"`, book as `"change"`), the payload producer
  (`Payload func() ([]byte, error)`, invoked concurrently from every
  connecting client and from the poll loop, so it must be goroutine-safe),
  the `Changes ChangeSource` to poll, and `PollInterval` (defaults to 2s).
  `(*Hub).ServeStream` mounts as the stream route: an initial `Payload()`
  frame, then every broadcast until the request context ends.
  `(*Hub).Run(ctx)` polls `Changes` and broadcasts a fresh `Payload()`
  whenever the signal advances. `(*Hub).Broadcast([]byte)` fans a payload
  out, dropping the frame for any client whose buffered channel is full
  rather than blocking on a slow reader. `(*Hub).ClientCount() int` backs
  each command's status line.
- `ChangeSource` / `Signal` / `MetaReader` /
  `NewChangeSource(root string, meta MetaReader) ChangeSource` — the shared
  change-detection contract. `Signal{Generation, Epoch}` pairs the SQLite
  `reindex_gen` meta key with the `.tusk/epoch` sentinel, so a poller
  notices both an ordinary reindex and a reset/rebuild.
- `NodeLister` / `EdgeLister` / `Neighbor` /
  `Neighbors(nodes NodeLister, edges EdgeLister, nodeID string) ([]Neighbor, error)`
  — the file-level neighbor projection graphview uses for its node-detail
  payload. It fetches only the edges incident to `nodeID` (`ListBySource` +
  `ListByTarget`, never a full scan), resolves every distinct far end in one
  batched `ListByIDs`, emits a self-loop once as `"out"`, drops sub-unit and
  dangling far ends, and orders the result to match `ListAll`'s global
  `(SourceID, Type, TargetID)`. `Neighbor{Node, Edge, Direction}` carries the
  raw index rows rather than a view shape — graphview is the sole caller,
  projecting the result into its own `Neighbor` payload. Returns an empty,
  non-nil slice for a node with no neighbors, and an empty slice (not an
  error) for an unknown node id.

## Notes

- The Host guard is the package's core security control, and it's what
  lets `tusk web` default to loopback while still working when a user
  deliberately exposes it on the LAN. `internal/webapp` wraps the whole
  server in one guard: its allowlist is empty by default (loopback +
  `localhost` only); a confirmed non-loopback `--addr` adds the bound
  hostname, or `"*"` if the interface is `0.0.0.0`/`::`. Beyond the guard,
  this package sets no security headers — the single unified CSP lives in
  `internal/webapp` (needed because the reading view renders untrusted
  vault content as DOM), not here.
- `Neighbors` applies exactly one rule — a structural edge to a sub-unit
  far end is skipped, the file-level rule graphview has always used — and
  leaves everything else to the caller. graphview keeps structural edges
  to file nodes and stops there. bookview does not call `Neighbors` at
  all: it re-implements the same incident-edge walk itself (`linksOf` in
  `internal/bookview/links.go`) so it can roll a sub-unit far end up to
  its parent file — a rollup `Neighbors` cannot support, because it drops
  sub-unit far ends before returning, so by the time a caller holds the
  `[]Neighbor` slice the sub-unit identity the rollup needs is already
  gone. Both policies live in the view packages, not here.

Backs `tusk web` (the graph and reading views), through `internal/webapp`.
