---
type: package
title: internal/wikilink — wikilink extraction
import-path: github.com/germanamz/tusk/internal/wikilink
status: stable
---

# internal/wikilink

Finds `[[wikilinks]]` in markdown text. It is a leaf package so that [`internal/node`](node.md) (file-level edges, ref resolution, move rewrites) and [`internal/subunit`](subunit.md) (sub-unit edges) share one definition of where a link target starts and ends. Before it existed `subunit` imported `node` for the scanner, which kept the node service from running the sub-unit pass (#782).

## Public surface

- `Extract(body) []string` — the unique link targets in a markdown `body`, in first-seen order, skipping code. Targets are alias-stripped and trimmed, and keep any `#section` fragment.
- `ExtractText(text) []string` — the same scan over text with no markdown code left in it: a slice of `MaskCode`'s output, or an HTML page's text. It reads every byte, backticks included.
- `MaskCode(body) []byte` — a copy of `body` with the content of every inline code span and code block (fenced, tilde-fenced, indented) replaced by spaces. Line breaks stay and the length doesn't change, so an offset found in the mask points at the same byte of `body`.
- `MaskCodeIn(doc, source) []byte` — `MaskCode` for a body the caller already parsed with goldmark, so the sub-unit parser masks the same tree its units come from.
- `SplitAlias(inner) (target, alias)` — splits a link's inner text at the first `|`. `[[id|Label]]` links to `id`; the alias comes back verbatim with its leading `|`, so a rewrite can swap the target and keep the display text byte-for-byte (#690).

## Code never links

A `[[target]]` inside code is an example of the syntax, not a reference. That covers an inline code span, a fenced or tilde-fenced block, and an indented block. A page that documents wikilinks would otherwise link to its examples, and doctor would flag every one as a dangling edge (#788). The web reader already worked this way: its markdown-it wikilink rule never sees text inside code, so an edge now exists exactly where the reader shows a link.

What counts as code is goldmark's CommonMark reading, with the Table and TaskList extensions the sub-unit and path-ref parsers use. A run of N backticks opens a span that closes at the next run of exactly N, and an unmatched run is literal text. So a triple backtick written inside inline code is part of that span; it no longer opens a fence that swallows the rest of the page. A fence that is never closed runs to the end of the document, as it renders.

All three consumers go through the mask. File-level edges use `Extract`. The sub-unit parser cuts each unit's links out of the masked source by the unit's byte span, because a unit's text is goldmark's rendered text and has already lost the backticks around a code span. `tusk node move` finds links in the mask and rewrites the original bytes, so it leaves code as written.
