---
type: package
title: internal/pathref — workspace paths named in inline code
import-path: github.com/germanamz/tusk/internal/pathref
status: stable
---

# internal/pathref

Finds the workspace paths a page names in inline code, so a source file can find the pages that describe it. An edge type opts in with `paths = true` (see [`internal/manifest`](manifest.md)); [`internal/node`](node.md) runs the extraction for the pages whose type the edge type's `from` allows, and the mentions land in the `path_refs` table of [`internal/index`](index.md). A path ref is an edge whose target is a path rather than a node, which is why it has its own table: source trees usually sit in `[workspace] ignore`, so their files have no node rows.

## Public surface

- `Candidate(span string) (string, bool)` decides whether a code span is, as a whole, a workspace-relative path, and returns it cleaned.
- `Clean(value string) (string, bool)` normalizes a path someone asks about (a `names-path` value, a file the plugin hook saw edited). It is looser than `Candidate`: `Makefile` and a leading `./` are fine.
- `Ancestors(target string) []string` returns the target and every directory above it, nearest first.
- `Markdown(body, file []byte, bodyOffset int, scheme linenum.Scheme) []Mention` walks a markdown body's code spans and returns the candidates with their file lines.
- `Spans(spans []string) []Mention` runs the same test over HTML `<code>` text, with no lines.
- `Disk` answers `Anchored(target)` and `Present(target)` against the workspace on disk, matching each segment's case exactly.
- `RulesVersion` tags the rules; reindex folds it into the marker that forces a re-process.

## What counts as a path

`Candidate` trims the span and drops a trailing line reference (`:42`, `:40-52`, `:40:7`). It then rejects the span when any of these hold:

- It contains whitespace, a control character, or one of `` *?[]{}<>|"'`$\;,=()!&#%^: ``. Those mark globs, shell syntax, URLs, quoting, anchors and assignments.
- It starts with `/`, `./`, `../`, `~` or `-`, or one of its segments is `..`. A relative path like `core/service.go` would need the page's context to resolve, so only workspace-relative paths count.
- It has no letter or digit, or it holds neither a `/` nor a `.`.

That last rule keeps bare identifiers such as `ExtractWikilinks` out of the table. It also means an extensionless root file like `Makefile` is never recorded (#780 tracks options).

Fenced and indented code blocks hold no code spans, so they never contribute. A span that wraps a line is rejected for its whitespace.

## Text now, disk later

The stored refs depend on page text alone. Two machines that clone a vault and run `tusk reindex` get the same rows, whatever is on disk. The disk is consulted only when someone reads the refs: `tusk doctor`, `tusk query --include paths`, and `tusk claude refs`.

At read time a ref counts only when it is **anchored**: its first segment exists at the workspace root, and is a directory when more segments follow. `server/ledger/old.go` is anchored while `server/` exists, so it can be reported missing after `old.go` goes away. `application/json` and `github.com/x/y` never anchor, so they stay silent. The cost is that a filter glob starting with a wildcard (`names-path=**`) can also match spans that only look like paths; anchoring the glob in a folder (`names-path=server/**`) avoids it.

A ref is **present** when every segment exists with exact case. `Disk` reads each directory once and compares names byte for byte, because `os.Stat` is case-insensitive on APFS and NTFS and would resolve `server/Ledger` to `server/ledger`. A directory that can't be read counts as present, since nothing proves the path missing. The reindex orphan reaper uses the same check (#686).

## Lines

Markdown mentions carry the 1-based file line of the span's first byte, counted from the top of the file under `[workspace] line-numbering`, the same numbering sub-unit line ranges use. The goldmark segments index into the body, so `Markdown` takes the whole file and the body's offset in it. HTML keeps no positions, so its mentions have line 0.
