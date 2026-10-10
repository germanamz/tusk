---
type: package
title: internal/subunit — markdown sub-unit parsing & structural addressing
import-path: github.com/germanamz/tusk/internal/subunit
status: stable
---

# internal/subunit

Parses a markdown file body into sub-document units (sections, paragraphs, list items, code blocks, blockquotes, table cells), assigns each a **structural address**, and syncs them into the index as `kind='subunit'` rows under their parent file. A pure transformation layer plus the index-facing sync; it does not embed (that is `internal/embed`).

## Public surface

- `Parse(source []byte) ([]Unit, error)` — goldmark-backed walk producing a deterministic, depth-first `[]Unit`. Each unit carries its `Address`, `ContentHash`, `ParentAddress`, `Ordinal`, kind-specific `Properties`, the synthesized `EmbedPayload`, and its byte span in `source` (`StartOffset` / `EndOffset`).
- `AssignLines(units, file, bodyOffset, scheme)` — turns each unit's byte span into `StartLine` / `EndLine`, counted over the whole file under an `internal/linenum` scheme. `bodyOffset` is where the parsed body starts in the file (`node.Node.BodyOffset`), so frontmatter lines count.
- `Unit` — one sub-document node. `Address` is the id suffix; `ContentHash` is the embed-payload fingerprint; `Hash` is the fallback id for kinds with no address rule.
- `Sync.ApplyFile(ctx, fileRow, units)` — diffs the parsed units against existing rows **by address**, inserts/deletes/updates rows, rewrites `contains` edges, re-derives outbound wikilink edges for inserted and content-changed units, and enqueues embeds for new or content-changed leaves (sections are never enqueued). The reindex worker calls it for every page it re-parses, and `node.Service` calls it after `Create` and `Modify`, whose lease writes make the next reindex skip the page (#782).
- `DeriveEdges` — wikilink extraction from a unit's text, through [`internal/wikilink`](wikilink.md), the scanner the file-level edges use too.

## Structural address grammar

A sub-unit row id is `<fileID>#<address>`.

- **Section path** mirrors the heading outline: `S1`, `S1.1`, `S1.2.1`, `S2`. Dots show depth; multiple `H1`s are allowed; the path reflects nesting order, not the literal heading level (an `H3` directly under an `H1` is `S1.1`).
- **Leaves** reset per enclosing section (or document root), per kind, 1-based in document order:
  - paragraph `P`, code box `B`, blockquote `Q`, list item `L` (lists flatten → one sequential counter), table cell `T<k>R<row>C<col>` (header row is `R0`).
- **Root-level** content (before the first heading, or a heading-free note) gets a bare leaf suffix: `P1`, `B1`, `T1R0C0`, …
- **Fallback to hash:** a kind with no registered address rule falls back to a 12-hex content hash; a defensive pass disambiguates any colliding fallback ids with `-N`. Structural addresses start with an uppercase letter (`S/P/B/Q/L/T`); hash fallbacks are lowercase hex, so the two never collide on the first character.

Examples: `notes/standup#S1.2P3`, `notes/standup#S1.1T1R2C0`, `notes/note#P1`.

## Stability contract

- **Section addresses are positional and independent of heading text.** Editing a heading keeps its address (and every descendant address); only the section row's title/text and `content_hash` change. Any edit *inside* a section also turns over the section's `content_hash` (it covers the full subtree text), refreshing the section's outbound wikilink edges without touching its address.
- **Leaf addresses are stable under in-place edits:** rewriting a paragraph keeps `S1.2P3`; its `content_hash` turns over, driving a re-embed.
- **Addresses shift under restructure** (insert/delete/reorder a block, move a heading). Because vectors are content-addressed (`internal/index` + `internal/embed`), a shifted-but-unchanged unit reuses its vector with no model call.

## Line ranges

Every markdown unit records where it sits so a query hit can point into the file. Lines are 1-based and inclusive, and a range covers the lines the unit's text came from:

- a section runs from its heading line (the text line of a setext heading) to the last line of its body, subsections included; an empty section is its heading, plus the underline for setext;
- a paragraph covers its own lines; a list item runs from its marker to the end of its own text (nested lists and code are units of their own);
- a fenced code block includes both fences, or runs to its last line when it is never closed; an indented code block and a blockquote cover their lines;
- a table cell is its row's line;
- trailing blank lines are never part of a range.

goldmark records a start position on every block, so `Parse` derives spans from the AST. It keeps only a fenced block's content lines, so the parser finds the closing fence on the line after the content. A fence-looking line there that opens the next block (when the block's container closed first) is not taken as the closing fence. Spans are computed over the CRLF-normalized source and mapped back to the bytes on disk, and `AssignLines` numbers them under the workspace's `[workspace] line-numbering` scheme. Line numbers never feed a hash, so a passage that only moves never re-embeds. The HTML walker keeps no source positions (`x/net/html` has none), so HTML units carry no lines.

## content_hash

Each sub-unit row stores `content_hash` = sha256 of the **embed payload** (for leaves — a table cell's payload is `"<column-header>: <cell-text>"`), or sha256 of the section's `Text` for sections — for the markdown walker that is the heading **plus the full descendant body** (sections are never embedded, but their outbound wikilink edges are derived from that full text — the hash must turn over on any edit inside the section or the sync diff would leave the section's edge set stale; the HTML walker's section `Text` is heading-only, and its section edges derive from that same text, so hash and edge set stay in lockstep on both paths). For leaves it drives the sync re-embed decision and is the key the embedding store dedupes on: identical content anywhere in the workspace is embedded once and shared.

## How indexing works

`Parse` walks the markdown once, assigning each unit its address and `content_hash`. `Sync.ApplyFile` then diffs those units against the file's existing sub-unit rows **by address**:

- address only in the new set → insert the row; derive its outbound wikilink edges; enqueue an embed (leaves only).
- address in both, `content_hash` changed → update the row; re-derive its outbound wikilink edges; re-enqueue the embed (leaves only).
- address in both, only ordinal, properties, or line range changed → update the row; the payload is byte-identical, so the vector and edges still hold and nothing is re-enqueued. An edit above a passage shifts its lines this way, and rows written before line ranges existed (NULL lines) fill this way.
- address in both, unchanged → leave the row untouched.
- address only in the old set → delete the row (its edges and embedding mapping cascade away).

The embed drain (`internal/embed`) reads a leaf's `content_hash`; if a vector for that content already exists it just records the node→content mapping with no model call, otherwise it embeds once and stores the shared vector (see `internal/embed` and `internal/index` for the `embeddings` / `node_embeddings` tables). So a restructure that shifts addresses but not content costs no re-embedding, identical content across the workspace is embedded once, and `tusk doctor` measures embedding coverage by those mappings — sections, which are never embedded, are not flagged.

## Notes

Identity was a content hash (`<fileID>#<12hex>`, disambiguated with `-N`) before the 2026-06 structural-addressing change. Reserved node/edge/property names for the sub-document kinds live in the sibling `internal/typepacks/subdocument` pack.
