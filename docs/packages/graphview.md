---
type: package
title: internal/graphview — 3D graph view API provider
import-path: github.com/germanamz/tusk/internal/graphview
status: stable
---

# internal/graphview

Provides the read-only, live-updating 3D graph of the vault as a set of JSON + SSE routes, mounted by `internal/webapp` under `/api/graph/*` (`const APIBase = "/api/graph"`). It is no longer a standalone server: `internal/webapp` owns the loopback bind, the single Host-header guard, the unified CSP, the embedded frontend, one healthz, and the SPA history fallback, and calls `RegisterRoutes(mux, base)` to graft graphview's routes onto the shared mux. graphview receives an open workspace handle via `Deps` and never opens the workspace itself. Snapshots stream to the browser over SSE; the client — the graph modules of the one unified Vite SPA, under `web/src/graph/` — renders them with `3d-force-graph` and three.js.

graphview still builds on `internal/webui` for the parts that don't vary: the SSE broadcast hub and the reindex/epoch change source. It no longer touches the Host guard or the static-asset handler — those moved up to `internal/webapp`, which composes graphview and bookview behind one loopback server (see `docs/packages/webapp.md`). The file-level neighbor projection behind `/api/graph/node`'s neighbor list stays graphview-specific: `webui.Neighbors` has graphview as its sole caller (bookview re-implements its own incident-edge walk instead, since `Neighbors` drops sub-unit far ends — see `docs/packages/webui.md`). What's left here is graph-specific: the snapshot/cluster/semantic-layout machinery below, plus a thin projection of `webui.Neighbors` into graphview's own `Neighbor` payload (see `neighborsOf` in `node.go`).

## Public surface

- `New(deps Deps) *Server` — constructs the API provider; does not bind a port.
- `(*Server).Run(ctx context.Context)` — starts the SSE change-detection loop.
- `(*Server).RegisterRoutes(mux *http.ServeMux, base string)` — grafts graphview's routes onto a caller-supplied mux under `base` (`internal/webapp` passes `APIBase`). Replaces the old `Handler()`; graphview no longer builds its own mux, Host guard, or CSP.
- `(*Server).ClientCount() int` — connected SSE clients; `internal/webapp` aggregates it with bookview's for the CLI status line.
- `APIBase = "/api/graph"` — the mount prefix `internal/webapp` grafts graphview under.
- **Payload types** (serialized as JSON to the browser):
  - `Graph` — the full snapshot: `Generation`, `Epoch`, `Nodes []GraphNode`, `Edges []GraphEdge`, `Cluster ClusterMeta`.
  - `GraphNode` — one file-level node: `ID`, `Type`, `Group`, `Title`, `Path`, `Tags`, `Degree`, `InDegree`.
  - `GraphEdge` — one file-level edge: `Source`, `Target`, `Type`, `Kind`.
  - `ClusterMeta` — the active cluster lens state: `By`, `Property`, `Huddle`, `Hull`.
  - `NodeDetail` — the `/api/graph/node/{id}` inspect payload (properties + rendered HTML + neighbors).
  - `SubunitGraph` — the `/api/graph/subunits/{id...}` drill-down payload.
  - `EmbeddingsResponse` — the `/api/graph/embeddings` payload (one vector per file node): `Model`, `Dim`, `Signature`, `Vectors map[string][]float32`. Drives the semantic layout (see below).
- **Dependency interfaces** (satisfied by `*index` repos): `NodeSource`, `EdgeSource`, `NodeRenderer`, `Querier`, `ChangeSource`, `EmbeddingSource`.
- `NewQuerier`, `NewRenderer` — concrete implementations wired when `tusk web` composes the graph API provider (`internal/webapp`). `ChangeSource` and `Signal` are aliases of `webui.ChangeSource`/`webui.Signal`; the change source itself is built with `webui.NewChangeSource`, not a graphview constructor.

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

## Semantic layout

A second layout **mode**, selected by a drawer toggle (`web/src/graph/controls.ts`). The graph defaults to **Structure** — the force layout above, where proximity means "linked and/or same group". **Semantic** mode instead positions each file node by the *similarity of its embedding*, so notes that mean similar things sit near each other even when they live in different link-communities. The cluster-lens color and hull overlays keep working over either layout, so colouring by link-`community` over semantic *position* shows where link structure and meaning agree or diverge.

```
GET /api/graph/embeddings ──▶ {nodeId: unit-vector[768]}  server: mean-pool a file's
  (one vector per file node, EmbeddingSource)             chunk vectors, then L2-normalize
        │
        ▼
  umap-js in a Web Worker ──▶ 3D coords ──▶ pin fx/fy/fz   client: project once, pin nodes,
  (web/src/graph/layout.worker.ts, web/src/graph/scene.ts) do NOT register the huddle force
```

**Server** (`embeddings.go`): `GET /api/graph/embeddings` resolves `node_embeddings ⋈ embeddings` for the file nodes via `EmbeddingSource.ListByNodeIDs`, **mean-pools** each file's chunk vectors and **L2-normalizes** (a file is chunked for embedding, so it owns 1..N vectors; zero-norm files are omitted). `Signature` is a sha256 over the *emitted* nodes' content hashes — a stable cache key that changes only when embedded content does. The endpoint is nil-tolerant: no `EmbeddingSource`, or no embeddings on disk, yields an empty payload and the client disables Semantic mode with a hint. Embeddings exist only if an embedding provider (`[embeddings]`) has run — see `docs/packages/embed.md` and `docs/packages/index.md`.

**Client**: a Web Worker runs `umap-js` (768-dim → 3D, seeded for determinism, then centred and scaled to view space). `scene.ts` pins each node's `fx/fy/fz` to its projected coordinate — re-applied on every snapshot, since `carryPositions` deliberately drops pins — and skips the huddle-force registration in Semantic mode (huddle pulls toward community anchors and would fight the projection). The projection is cached by `Signature`, so SSE re-snapshots re-pin from cached coordinates rather than recomputing UMAP; a `layoutRequestGen` token discards a stale projection if the user toggles back to Structure mid-compute. Nodes with no vector (e.g. expanded drill-down sub-units, which are embedded per sub-unit but carry no file-level vector) are parked on a deterministic shell outside the projected cloud. The toggle is runtime-only and defaults to Structure; there is no `tusk.toml` default for the layout mode.

## Notes

- graphview no longer embeds a `dist/` of its own. The graph client is part of the one unified Vite SPA in `web/` (graph modules under `web/src/graph/`), built by `make web` / `make frontend` into `internal/webapp/dist` and `//go:embed`-ed there. Never edit the built `dist/` by hand; run `make web` to rebuild. Dependabot frontend PRs get the built `dist/` rebuilt and pushed automatically by `.github/workflows/dependabot-frontend-dist.yml`.
- Hull meshes are built by `web/src/graph/hulls.ts` using `ConvexGeometry` from `three@0.180.0`. The overlay is throttled (~250 ms between recomputes during engine ticks) and rebuilt once when the simulation settles. Groups with fewer than 4 members are skipped (a convex hull is undefined below 4 non-coplanar points).
- `make docs` must be re-run after editing the command help strings for `tusk web` (or its deprecated `graph`/`book` aliases); the pre-push docs-drift hook rejects stale `docs/cli/` or `man/` trees.
