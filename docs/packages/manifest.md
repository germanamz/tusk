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

## Notes

Reserved property names (`type`, `title`) cannot be re-declared in `[node-types.X].properties`. The `id` field in frontmatter is NOT a property — node IDs are auto-derived from the workspace-relative path (see `internal/node/parse.go`).
