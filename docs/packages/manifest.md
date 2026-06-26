---
type: package
title: internal/manifest — tusk.toml loader
import-path: github.com/germanamz/tusk/internal/manifest
status: stable
---

# internal/manifest

Loads and validates `tusk.toml`. Decodes `[workspace]`, `[node-types.X]`, `[edge-types.X]`, `[behaviors.X.Y]` sections. Synthesizes auto-generated edge types from `ref` properties (per Plan 7.c.1) and rejects collisions against explicit `[edge-types.X]` declarations.

## Public surface

- `Load(path string) (*Manifest, error)` — primary entry.
- `Validate(*Manifest) error` — exposed for test harnesses constructing manifests in-memory.
- `Manifest`, `NodeType`, `EdgeType`, `PropertyDecl` — typed shapes.
- `IsRefProperty(PropertyDecl) bool` — used by `internal/node/refs.go` to drive ref resolution.
- `Alias`, `AliasError`, `FlagSpec`, `VerbIntrospector` — types covering manifest-declared aliases.
- `ValidateAliases(*Manifest, VerbIntrospector)` — secondary pass that resolves each alias's verb against `internal/cliregistry` and stamps invalid aliases into `Manifest.AliasErrors`. Never returns an error; failures are surfaced through `internal/doctor`.
- `Context`, `ContextError` — types covering the `[context]` block consumed by `internal/contextcompose`.
- `ValidateContext(*Manifest, VerbIntrospector)` — tertiary pass run after `ValidateAliases` that resolves `recent = "<name>"`, parses `[context.recent]` inline aliases, and prunes unknown `include` names. Surfaces problems via `Manifest.ContextErrors`; never fails.

## Notes

Reserved property names (`type`, `title`) cannot be re-declared in `[node-types.X].properties`. The `id` field in frontmatter is NOT a property — node IDs are auto-derived from the workspace-relative path (see `internal/node/parse.go`).

### Hierarchy edges

An `[edge-types.X]` declaration may opt into the `tree=` / `parent=` / `root=` traversal-shortcut family by setting:

- `hierarchy = "<alias>"` — names this edge as a hierarchy. The alias is used in qualified shortcuts (`tree:<alias>=<id>`). Must be kebab-case; cannot equal the reserved keywords `tree`, `parent`, or `root`. Aliases are unique within a workspace.
- `hierarchy-default = true` — marks this edge as the target of unqualified shortcuts (`tree=<id>`). At most one edge per workspace may set this.

Multiple edges can each declare a distinct alias; this lets composed packs (e.g. kanban + superhuman-wbs) coexist without colliding on a single hierarchy edge.

**Back-compat:** A workspace that declares `[edge-types.parent]` without setting `hierarchy` is automatically treated as if it had `hierarchy = "parent"`. If no other edge has claimed `hierarchy-default = true`, the bare `parent` edge is also marked as default. This preserves pre-v1.3 behavior for existing workspaces.

#### Polymorphic `ordered`

Hierarchy edges (and any edge type that should expose stable child order) may declare `ordered` in `tusk.toml` as either a bool or a string:

- `ordered = true` — children are ordered by the source node's `order` property (the default key).
- `ordered = "<prop>"` — children are ordered by the named source-node property (e.g. `ordered = "rank"`).

After load, the resolved shape is `Ordered bool` + `OrderedBy string`; when `ordered = true`, `OrderedBy` is set to `"order"`. Edges are declared in frontmatter (the 2026-05-18 edges-from-frontmatter design): `tusk edge add` / `tusk_edge_add` mutate frontmatter directly and the index is rebuilt from it.

### Lease TTL — `[lease]`

The optional `[lease]` block configures the lease window applied to every concurrency-coordination path in tusk (today: the `embed_queue` Drain; Phase 4 onwards: the `file_state` Claim used by node writes). One value covers every lease-taking path.

```toml
[lease]
ttl_seconds = 90
```

- `ttl_seconds` (integer, optional) — lease window in seconds. When omitted, the resolver falls through to the env override and then the 60-second default. The loader rejects explicit `0` or negative values with `manifest: lease.ttl_seconds must be > 0`.

**Env override:** `TUSK_LEASE_TTL_SECONDS` takes precedence over the manifest value. A malformed or non-positive env value is ignored with a warning (operators should not have a process refuse to start because of a bad env var). The resolution order, highest precedence first, is:

1. `TUSK_LEASE_TTL_SECONDS` (positive integer).
2. `[lease] ttl_seconds` in `tusk.toml` (positive integer).
3. Default: 60 seconds.

**Read once at startup.** The TTL is resolved when the process starts; changing the env var or the manifest field requires a restart. There is no hot-reload and no lease renewal / heartbeat — a worker that misses its deadline simply lets the lease expire and a peer reclaims the work.

**Tuning hazard.** Setting the TTL very low (e.g. 1 s) risks lease expiry mid-flight: a long embed of a large node or a slow file write can lose its lease before completing, at which point a second worker reclaims the row and redoes the work. The default (60 s) comfortably covers every observed embed/write latency; only shorten it if you are confident every workload finishes inside the new window.

### Embed/reindex worker pool — `[embeddings] workers`

The `workers` field under `[embeddings]` caps the number of goroutines the process spawns to drain the embed and reindex queues.

```toml
[embeddings]
workers = 4
```

- `workers` (integer ≥ 0, optional) — pool size for the embed + reindex worker pool. When omitted, the resolver falls through to the env override and then the default `max(1, NumCPU/2)`.
- `workers = 0` — **opt out.** This instance does not start the embed or reindex worker pool. The MCP server still answers queries and mutations, the file watcher still runs, but nothing in this process drains the queue. **Some other instance (or a scheduled `tusk reindex`) must drive indexing** or the index will go stale.

**Env override:** `TUSK_EMBED_WORKERS` takes precedence over the manifest value. Malformed or negative env values are ignored with a warning. Resolution order, highest precedence first:

1. `TUSK_EMBED_WORKERS` (non-negative integer; `0` means opt out).
2. `[embeddings] workers` in `tusk.toml` (non-negative integer; `0` means opt out).
3. Default: `max(1, NumCPU/2)`.

The pool size is resolved once at process start; changes require a restart.

### Graph cluster lens — `[graph.cluster]`

The optional `[graph.cluster]` block configures how the `tusk graph` view groups nodes. The resolver is `internal/manifest/graph_cluster.go`; the resolved struct is `Manifest.GraphCluster`.

An absent block defaults to `by = "type"`, reproducing the original color-by-type behavior. Every key is optional except where noted.

```toml
[graph.cluster]
by               = "community"  # type (default) | property | ancestor | community
community-edges  = ["depends-on", "references"]
resolution       = 1.0
huddle           = true
hull             = true
```

| Key | Type | Default | Applies to | Description |
|---|---|---|---|---|
| `by` | string | `"type"` | all | Active producer. Accepted values: `type`, `property`, `ancestor`, `community`. |
| `property` | string | — | `by = "property"` | Frontmatter field whose value becomes the group key. Required when `by = "property"`. |
| `edge` | string | — | `by = "ancestor"` | Hierarchy edge-type name to walk. Required when `by = "ancestor"`. |
| `depth` | int | `0` | `by = "ancestor"` | Ancestor walk depth. `0` walks to the topmost ancestor (root). |
| `parent-is-source` | bool | `false` | `by = "ancestor"` | Set to `true` for parent→child edges (e.g. `contains`); leave `false` for child→parent edges (e.g. `parent`). |
| `huddle` | bool | `false` | all | Engages a layout force that pulls same-group nodes toward a fixed per-group anchor on a Fibonacci sphere. |
| `hull` | bool | `false` | all | Draws a translucent 3D convex-hull boundary around each group. Groups with fewer than 4 members are silently skipped. Orthogonal to `by`. |
| `community-edges` | []string | (all edges) | `by = "community"` | Edge type or kind names the community detector clusters on. Empty means all kept file-level edges. |
| `resolution` | float | `1.0` | `by = "community"` | Modularity gamma for the Louvain detector. Higher values favor more, smaller communities. Must be `> 0`. |

See `docs/packages/graphview.md` for architecture details and worked config examples.
