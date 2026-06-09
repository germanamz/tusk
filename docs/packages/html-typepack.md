---
type: package
title: internal/typepacks/html — HTML sub-document type pack
import-path: github.com/germanamz/tusk/internal/typepacks/html
status: stable
---

# internal/typepacks/html

A public constants/declarations package for HTML content. It mirrors `internal/typepacks/subdocument` **verbatim** — the same six reserved node types (`section`, `paragraph`, `list-item`, `code-block`, `blockquote`, `table-cell`), the same `contains` / `contained-by` edge types, and the same per-type typed properties (`section.heading-level`, `list-item.checkbox`, `code-block.lang`, `table-cell.{header,row,column,column-header}`). The single difference is `Source()`: this pack returns `"html"`, the subdocument pack returns `"markdown"`.

This package has **no observable schema effect**. It installs nothing and merges nothing. The manifest stores node/edge types in flat, source-blind maps, so a "merge" of this pack would re-install the exact declarations `mergeSubdocumentPack` already installs — a confirmed no-op. There is deliberately **no** `mergeHTMLPack` and **no** `htmlPackApplied` flag. The only load-bearing markdown/html distinction in the feature is the per-row `source` column set on sub-unit rows in Phase 5; this package is merely the canonical Go home for the `"html"` source identifier (`Source()`) and the reserved-name lists Phase 5 consumes.

`ReservedProperties` carries **no** `data` key. The drift exemption for the HTML signals bag (`node.HTMLSignalsKey`) is owned entirely by Phase 4 (`htmlReservedDrift`, keyed on the parsed node's user-declared type), not by this declarations pack.

## Public surface

- `Source() string` — returns `"html"`.
- `ReservedNodeTypes []string` / `ReservedEdgeTypes []string` — verbatim mirror of `subdocument.Reserved*`.
- `ReservedProperties map[string][]string` — per-node-type reserved property names; verbatim mirror of `subdocument.ReservedProperties` (no extra keys).
- `SortedReservedNodeTypes() []string` / `SortedReservedEdgeTypes() []string` — freshly allocated, lexicographically sorted copies.
- `NodeTypes() map[string]manifest.NodeType` / `EdgeTypes() manifest.EdgeTypes` — re-exported from `manifest.SubdocumentNodeTypes` / `manifest.SubdocumentEdgeTypes` (the HTML schema is byte-identical to the markdown sub-document schema; only `Source()` differs).

## Notes

As of this phase the package is a pure declarations surface: no `.html` file is parsed, no `source = "html"` row exists, and `manifest.MergeBuiltinPacks` is unchanged. The HTML file parse (`node.ParseHTMLFile`, Phase 3), the extension dispatch and drift exemption (Phase 4), and HTML sub-unit emission with the per-row `source` column (Phase 5) land later; Phase 5 consumes `html.Source()` as the source identifier. The canonical node/edge declarations live in the `manifest` package (re-exported here) to avoid an import cycle between `html` and `manifest`, matching the subdocument pack.
