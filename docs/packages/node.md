---
type: package
title: internal/node — node parsing and CRUD
import-path: github.com/germanamz/tusk/internal/node
status: stable
last-touched-by: Plan 7.c.1
---

# internal/node

Heart of the data layer. Parses markdown frontmatter, validates properties against the manifest, resolves `ref` properties + body wikilinks into edges, and provides the write-side service used by both CLI commands and MCP tools.

## Public surface

- `ParseFile(relPath, content) (*Node, error)` — frontmatter → `Node` shape; ID is the relPath sans extension.
- `Service` — `Create`, `Modify`, `Move`, `Delete`; serialized via the workspace lock.
- `ValidateProperties(*Node, NodeTypes) PropertyResult` — type / required / drift checks.
- `ResolveEdges(*Node, EdgeTypes) error` — frontmatter edge values → typed edges.
- `ResolveRefs(*Node, NodeTypes, RefLookup) RefResolutionResult` — refs + wikilinks → resolved edges.
- `ExtractWikilinks(body) []string` — fenced-code-aware body scanner.

## Notes

`ResolveRefs` falls back from `Properties` to `Edges` because `reindex.go` reorders the pipeline (`ResolveEdges` runs first there). `Service.Create` runs `ResolveRefs` before `ResolveEdges` to avoid the fallback. Two code paths with different value-source assumptions — handoff §residual to clean up by re-ordering reindex's pipeline.
