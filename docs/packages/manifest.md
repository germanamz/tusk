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

After load, the resolved shape is `Ordered bool` + `OrderedBy string`; when `ordered = true`, `OrderedBy` is set to `"order"`. See the 2026-05-18 edges-from-frontmatter design (`docs/superpowers/specs/2026-05-18-edges-from-frontmatter-design.md`) for the wire format and rationale.
