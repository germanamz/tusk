---
type: package
title: internal/wikilink — wikilink extraction
import-path: github.com/germanamz/tusk/internal/wikilink
status: stable
---

# internal/wikilink

Finds `[[wikilinks]]` in markdown text. It is a leaf package so that [`internal/node`](node.md) (file-level edges, ref resolution, move rewrites) and [`internal/subunit`](subunit.md) (sub-unit edges) share one definition of where a link target starts and ends. Before it existed `subunit` imported `node` for the scanner, which kept the node service from running the sub-unit pass (#782).

## Public surface

- `Extract(body) []string` — the unique link targets in `body`, in first-seen order, skipping triple-backtick fenced code blocks. Targets are alias-stripped and trimmed, and keep any `#section` fragment.
- `SplitAlias(inner) (target, alias)` — splits a link's inner text at the first `|`. `[[id|Label]]` links to `id`; the alias comes back verbatim with its leading `|`, so a rewrite can swap the target and keep the display text byte-for-byte (#690).

## Notes

The scanner only skips triple-backtick fenced code blocks. Inline single-backtick spans are not skipped, so wikilink-shaped tokens in prose (even inside backticks) materialize as edges of every edge type declared with `wikilinks = true`. A worse edge case: prose that mentions a literal triple-backtick token inside inline code (for example, describing the fence detector itself) is parsed as an unterminated fence, which silently drops the rest of the body from extraction. The workaround is to spell it out in words.
