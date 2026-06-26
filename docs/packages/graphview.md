---
type: package
title: internal/graphview — 3D graph view server
import-path: github.com/germanamz/tusk/internal/graphview
status: stable
---

# internal/graphview

Serves a read-only, live-updating 3D graph of the vault over a loopback HTTP server. Receives an open workspace handle via `Deps` and never opens the workspace itself. Snapshots are streamed to the browser over SSE; the client (embedded Vite bundle in `dist/`) renders them with `3d-force-graph` and three.js.

## Public surface

- `New(deps Deps) *Server` — constructs the server; does not bind a port.
- `(*Server).Run(ctx context.Context)` — starts the SSE change-detection loop.
- `(*Server).Handler() http.Handler` — returns the HTTP mux (served by the caller).
- `DefaultAddr string` — the default loopback bind address (`127.0.0.1:7373`).
- **Payload types** (serialized as JSON to the browser):
  - `Graph` — the full snapshot: `Generation`, `Epoch`, `Nodes []GraphNode`, `Edges []GraphEdge`, `Cluster ClusterMeta`.
  - `GraphNode` — one file-level node: `ID`, `Type`, `Group`, `Title`, `Path`, `Tags`, `Degree`, `InDegree`.
  - `GraphEdge` — one file-level edge: `Source`, `Target`, `Type`, `Kind`.
  - `ClusterMeta` — the active cluster lens state: `By`, `Property`, `Huddle`, `Hull`.
  - `NodeDetail` — the `/api/node/{id}` inspect payload (properties + rendered HTML + neighbors).
  - `SubunitGraph` — the `/api/node/{id}/subunits` drill-down payload.
- **Dependency interfaces** (satisfied by `*index` repos): `NodeSource`, `EdgeSource`, `NodeRenderer`, `Querier`, `ChangeSource`.
- `NewQuerier`, `NewRenderer`, `NewChangeSource` — concrete implementations wired from `cmd/tusk/cmd_graph.go`.

## Cluster lens architecture

The cluster lens assigns each node a single `groupKey` server-side; the client renders it through three independent visual channels:

```
producer ──▶ groupKey ──┬──▶ Color   one hue per distinct group (legend)
(config: by)            ├──▶ Huddle  cluster force pulls same-group nodes together
                        └──▶ Hull    translucent convex-hull boundary per group
```

The producer is selected by `[graph.cluster] by = ...` in `tusk.toml`; `ClusterMeta.By` carries the active value to the client. `ClusterMeta.Huddle` and `ClusterMeta.Hull` carry the two opt-in channel flags. The channels are orthogonal to the producer: any combination of `huddle` and `hull` works with any `by` value.

`GraphNode.Group` is the wire field for the group key. An empty string means no group (neutral grey, no cluster pull, no hull membership). For `by = "type"` (the default), `Group == Type`, so the default config reproduces the original color-by-type behavior exactly.

The active cluster producer is resolved by `internal/manifest.GraphCluster` (see `docs/packages/manifest.md`); `snapshot()` in `snapshot.go` is the single site where the producer runs and `Graph.Cluster` is assembled.

## Worked `[graph.cluster]` example

**Community detection with all three channels:**

```toml
[graph.cluster]
by = "community"          # type | property | ancestor | community
community-edges = ["depends-on", "references"]
resolution = 1.0
huddle = true             # pull same-group nodes together
hull  = true              # wrap each group in a translucent boundary
```

**Property-based grouping (simpler):**

```toml
[graph.cluster]
by       = "property"
property = "team"         # frontmatter field whose value becomes the group key
huddle   = true
```

**Ancestor grouping (color by top-level branch):**

```toml
[graph.cluster]
by    = "ancestor"
edge  = "parent"
depth = 1                 # 0 = walk to root; positive = stop at that depth
```

An absent `[graph.cluster]` block, or `by = "type"`, reproduces the original behavior: one hue per node type, no huddle, no hull.

## Notes

- The embedded client bundle lives in `dist/` (built by `make web`); it is `//go:embed`-ed into the binary and served as static assets. Never edit `dist/` by hand; run `make web` to rebuild.
- Hull meshes are built by `web/src/hulls.ts` using `ConvexGeometry` from `three@0.180.0`. The overlay is throttled (~250 ms between recomputes during engine ticks) and rebuilt once when the simulation settles. Groups with fewer than 4 members are skipped (a convex hull is undefined below 4 non-coplanar points).
- `make docs` must be re-run after editing the `Long` help string in `cmd/tusk/cmd_graph.go`; the pre-push docs-drift hook rejects stale `docs/cli/` or `man/` trees.
