---
type: package
title: internal/node — node parsing and CRUD
import-path: github.com/germanamz/tusk/internal/node
status: stable
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
- `MaterializeWikilinks(*Node, EdgeTypes)` — appends extracted body wikilinks to every edge type declared with `wikilinks = true`. Replaces the former hardcoded `references` special case; the name of the target edge no longer matters and zero, one, or many edges may opt in. Called by both `Service` and the reindexer.
- `AddEdgeToFrontmatter(root, sourceID, edgeType, targetID, edgeTypes) error` — inserts an edge target under the edge-type key in the source's frontmatter, respecting cardinality (scalar for one-to-one / many-to-one; list for one-to-many / many-to-many). Atomic read-mutate-write; callers must hold the workspace lock.
- `RemoveEdgeFromFrontmatter(root, sourceID, edgeType, targetID, edgeTypes) error` — idempotent inverse; drops the key when the last target is removed.
- `ReindexSource(root, edgeRepo, edgeTypes, sourceID) error` — re-parses the source file and upserts its resolved edges into the index under the source's real path. Used by `tusk edge add` / `tusk edge remove` to refresh the index after a frontmatter rewrite without waiting for the watcher.

## Notes

`ResolveRefs` falls back from `Properties` to `Edges` because `reindex.go` reorders the pipeline (`ResolveEdges` runs first there). `Service.Create` runs `ResolveRefs` before `ResolveEdges` to avoid the fallback. Two code paths with different value-source assumptions — handoff §residual to clean up by re-ordering reindex's pipeline.

The wikilink scanner only skips triple-backtick fenced code blocks; inline single-backtick spans are NOT skipped, so wikilink-shaped tokens in prose (even inside backticks) materialize as edges of every edge type declared with `wikilinks = true`. A worse edge case: prose that mentions a literal triple-backtick token inside inline code (e.g., describing the fence detector itself) is parsed as an unterminated fence, which silently drops the remainder of the body from wikilink extraction. Discovered while writing the migration handoffs — workaround is to spell it out in words.
