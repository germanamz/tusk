---
type: package
title: internal/wikilink — wikilink extraction and resolution
import-path: github.com/germanamz/tusk/internal/wikilink
status: stable
---

# internal/wikilink

Finds `[[wikilinks]]` in markdown text and resolves their targets to node ids. It is a leaf package so that [`internal/node`](node.md) (file-level edges, ref resolution, move rewrites) and [`internal/subunit`](subunit.md) (sub-unit edges) share one definition of where a link target starts and ends. Before it existed `subunit` imported `node` for the scanner, which kept the node service from running the sub-unit pass (#782).

## Public surface

- `Extract(body) []string` — the unique link targets in a markdown `body`, in first-seen order, skipping code. Targets are alias-stripped and trimmed, and keep any `#section` fragment.
- `ExtractText(text) []string` — the same scan over text with no markdown code left in it: a slice of `MaskCode`'s output, or an HTML page's text. It reads every byte, backticks included.
- `MaskCode(body) []byte` — a copy of `body` with the content of every inline code span and code block (fenced, tilde-fenced, indented) replaced by spaces. Line breaks stay and the length doesn't change, so an offset found in the mask points at the same byte of `body`.
- `MaskCodeIn(doc, source) []byte` — `MaskCode` for a body the caller already parsed with goldmark, so the sub-unit parser masks the same tree its units come from.
- `SplitAlias(inner) (target, alias)` — splits a link's inner text at the first `|`. `[[id|Label]]` links to `id`; the alias comes back verbatim with its leading `|`, so a rewrite can swap the target and keep the display text byte-for-byte (#690).
- `SplitFragment(target)`, `IsName(target)`, `IsRelative(target)` — classify a target by the form it names its note in (see below).
- `Normalize(target, sourcePath)` — what an edge stores for a link written in the file at `sourcePath` before any name is looked up: a relative path becomes the id it names, anything else stays as written. `RelativeTo(sourcePath, id)` renders the relative link back, for `tusk node move`.
- `Resolve(target, sourcePath, files)` — the id a target names, with the candidates when a name matches none or several files. `files` is a `Files`: `*Names` answers from memory, and `index.FileNames` answers one name at a time from the index.
- `Names` (`NewNames`, `Add`, `Remove`, `Clone`, `Path`, `ResolveName`) — an in-memory index of file nodes by name, which reindex and doctor load once per pass and a move compares before and after.
- `FileName(relPath)`, `NameKey(name)`, `MatchName(...)` — a file's name (its base name without the extension), its case-folded key (what `nodes.name_key` stores), and the match decision both `Files` implementations share.
- `Case` / `ParseCase` — the `[workspace] link-case` setting: `insensitive` (the default) or `sensitive`.

## How a target names its note

A target names its note in one of three forms, the three Obsidian's link format setting writes (#791). A path (`[[notes/auth-rfc]]`) is the node id itself. A relative path (`[[../notes/auth-rfc]]`, `[[./auth-rfc]]`) starts from the linking file's folder; one that climbs out of the workspace names nothing. A name (`[[auth-rfc]]`) has no folder: the file whose id is exactly the name wins, so a link that resolved before names existed still does, and otherwise the name is matched against every file's name and one match resolves it. A `#fragment` is cut off before resolution and put back on the id after.

Name lookup depends on the whole set of files, so it is not done while a file is parsed. Edge writers store a name with its own text as the target and the text in the row's link column, and `index.ResolveLinks` points those rows at their files after each reindex drain and each write. That makes the result independent of the order files are read in, and a file added or removed outside tusk changes what a name finds without re-reading the files that link to it.

## Code never links

A `[[target]]` inside code is an example of the syntax, not a reference. That covers an inline code span, a fenced or tilde-fenced block, and an indented block. A page that documents wikilinks would otherwise link to its examples, and doctor would flag every one as a dangling edge (#788). The web reader already worked this way: its markdown-it wikilink rule never sees text inside code, so an edge now exists exactly where the reader shows a link.

What counts as code is goldmark's CommonMark reading, with the Table and TaskList extensions the sub-unit and path-ref parsers use. A run of N backticks opens a span that closes at the next run of exactly N, and an unmatched run is literal text. So a triple backtick written inside inline code is part of that span; it no longer opens a fence that swallows the rest of the page. A fence that is never closed runs to the end of the document, as it renders.

All three consumers go through the mask. File-level edges use `Extract`. The sub-unit parser cuts each unit's links out of the masked source by the unit's byte span, because a unit's text is goldmark's rendered text and has already lost the backticks around a code span. `tusk node move` finds links in the mask and rewrites the original bytes, so it leaves code as written.
