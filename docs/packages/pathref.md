---
type: package
title: internal/pathref — workspace paths a page names
import-path: github.com/germanamz/tusk/internal/pathref
status: stable
---

# internal/pathref

Finds the workspace paths a page names, so a source file can find the pages that describe it. A page names a path in inline code (`` `server/ledger/core/service.go` ``), in a link or image (`[the service](../server/ledger/core/service.go)`), or, under a paths-only edge type, in a frontmatter value. An edge type opts in with `paths = true` (see [`internal/manifest`](manifest.md)); [`internal/node`](node.md) runs the extraction for the pages whose type the edge type's `from` allows, and the mentions land in the `path_refs` table of [`internal/index`](index.md). A path ref is an edge whose target is a path rather than a node, which is why it has its own table: source trees usually sit in `[workspace] ignore`, so their files have no node rows.

## Public surface

- `Candidate(span string) (string, bool)` decides whether a code span is, as a whole, a workspace-relative path, and returns it cleaned.
- `Link(sourcePath, destination string) (string, bool)` resolves a link destination written on the page at `sourcePath` into the workspace path it points at. `node.ResolveHTMLLinks` uses it too, so HTML link edges and path refs resolve the same way.
- `Clean(value string) (string, bool)` normalizes a path someone asks about (a `names-path` value, a file the plugin hook saw edited). It is looser than `Candidate`: `Makefile` and a leading `./` are fine.
- `Declared(value string) (string, bool)` normalizes a frontmatter value under a paths-only edge type. It takes what `Clean` takes, minus a control character or a `:`.
- `Ancestors(target string) []string` returns the target and every directory above it, nearest first.
- `Markdown(sourcePath string, body, file []byte, bodyOffset int, scheme linenum.Scheme) []Mention` walks a markdown body's code spans, links and images, and returns the paths with their file lines.
- `HTML(sourcePath string, code, links []string) []Mention` does the same for an HTML page: the text of its `<code>` elements outside `<pre>`, and its `<a href>` and `<img src>` values. No lines.
- `Disk` answers `Anchored(target)`, `Present(target)` and `Resolves(target)` against the workspace on disk, matching each segment's case exactly.
- `RulesVersion` tags the rules; reindex folds it into the marker that forces a re-process.

## What counts as a path

### Inline code

`Candidate` trims the span and drops a trailing line reference (`:42`, `:40-52`, `:40:7`). It then rejects the span when any of these hold:

- It contains whitespace, a control character, or one of `` *?[]{}<>|"'`$\;,=()!&#%^: ``. Those mark globs, shell syntax, URLs, quoting, anchors and assignments.
- It starts with `/`, `./`, `../`, `~` or `-`, or one of its segments is `..`. A relative path like `core/service.go` would need the page's context to resolve, so only workspace-relative paths count.
- It has no letter or digit.
- It holds neither a `/` nor a `.`, and isn't one of the well-known extensionless names: `Makefile`, `GNUmakefile`, `makefile`, `Dockerfile`, `Containerfile`, `Jenkinsfile`, `Vagrantfile`, `Procfile`, `Justfile`, `justfile`, `Rakefile`, `Gemfile`, `Brewfile`, `LICENSE`, `LICENCE`, `COPYING`, `NOTICE`, `AUTHORS`, `CODEOWNERS` and `VERSION`.

The last rule keeps bare identifiers such as `ExtractWikilinks` out of the table, while a root file people name bare still counts. The list is matched exactly, so `MAKEFILE` doesn't count; the anchored check below silences a name the workspace root doesn't hold.

Fenced and indented code blocks hold no code spans, so they never contribute. A span that wraps a line is rejected for its whitespace.

### Links and images

A link or image destination is an explicit reference, so `Link` applies no shape test: `[make](../Makefile)` counts. It resolves a path-relative destination against the page's directory, the way a markdown renderer reads it, and a root-relative one (`/server/x.go`) against the workspace root. It drops the query string and fragment and decodes percent-escapes. It rejects an external URL (any scheme, such as `https:` or `mailto:`), a protocol-relative one (`//host/x`), an in-page anchor (`#intro`), an empty destination, and one that climbs above the workspace root.

Every resolved destination is recorded, a link to another page included. A broken link to a note therefore shows up as `path-missing` in doctor too. A link that names a markdown page by its node id (`../notes/howto` for `notes/howto.md`, the form HTML hrefs to markdown nodes take and the one `tusk node move` writes) is not broken; see `Resolves` below. A link to a page doesn't become a node edge; only wikilinks and HTML hrefs do that, through `wikilinks = true`.

In markdown, `Markdown` reads inline links, reference links and images, including a link whose text is a code span (the span and the link name the same path on the same line, so it is recorded once). Autolinks always carry a scheme, so they never name a workspace path. In HTML, `HTML` reads `<a href>` and `<img src>`.

Because a link resolves against the page's directory, moving a page can change what its links name. `node.Rename` re-derives the moved page's refs from its bytes at the new path rather than carrying the old rows over.

### Frontmatter values

Under a **paths-only** edge type (`paths = true` with no `to`; see `manifest.EdgeType.PathsOnly`), a frontmatter value names a path, not a node: `describes: [server/ledger/core/service.go]`. A paths-only type allows no node targets, so the two readings can't collide. `node.ResolveEdges` moves each value that `Declared` accepts into `Node.PathValues` instead of `Node.Edges`, and `node.PathRefs` records it with the line it is written on. A value `Declared` rejects (a URL, an absolute path, one that climbs out) stays an edge, where doctor reports it as it would any bad target. So does every value on a page whose type the edge type's `from` doesn't allow. A paths type that also declares `to` keeps reading its frontmatter values as node ids.

`tusk edge add` and `edge remove` write the same frontmatter, so they work on a paths-only type too: `edge add` checks the target with `Declared` instead of looking it up as a node. A node move leaves these values as written, the way it leaves links and code spans.

## Text now, disk later

The stored refs depend on page text alone. Two machines that clone a vault and run `tusk reindex` get the same rows, whatever is on disk. The disk is consulted only when someone reads the refs: `tusk doctor`, `tusk query --include paths`, `tusk node get --include paths`, and `tusk claude refs`.

At read time a ref counts only when it is **anchored**: its first segment exists at the workspace root, and is a directory when more segments follow. `server/ledger/old.go` is anchored while `server/` exists, so it can be reported missing after `old.go` goes away. `application/json` and `github.com/x/y` never anchor, so they stay silent. The cost is that a filter glob starting with a wildcard (`names-path=**`) can also match spans that only look like paths; anchoring the glob in a folder (`names-path=server/**`) avoids it. Links and frontmatter values follow the same rule, since the rows don't record where a path came from: a link or a declared value whose first segment isn't at the root (`[x](../sever/x.go)`, a folder not created yet) stays out of the listings and doctor until it is, though `names-path` still matches it.

A ref is **present** when every segment exists with exact case. `Disk` reads each directory once and compares names byte for byte, because `os.Stat` is case-insensitive on APFS and NTFS and would resolve `server/Ledger` to `server/ledger`. A directory that can't be read counts as present, since nothing proves the path missing. The reindex orphan reaper uses the same check (#686).

Readers of path refs (doctor's `path-missing` and the `exists` flag on `--include paths`) ask `Resolves` instead: present, or naming a markdown page by its node id, so `notes/howto` resolves when `notes/howto.md` is there. `Present` itself stays exact, because the reaper checks real file paths and a `page.html.md` must not keep a deleted `page.html` alive.

## Lines

Markdown mentions carry the 1-based file line where they start, counted from the top of the file under `[workspace] line-numbering`, the same numbering sub-unit line ranges use. A code span's line is its first byte; a link's or image's is the first byte of its text, or the first line of its paragraph when it has none (`[](x.go)`). The goldmark segments index into the body, so `Markdown` takes the whole file and the body's offset in it. A frontmatter value's line is where its scalar is written. HTML keeps no positions, so its mentions have line 0.
