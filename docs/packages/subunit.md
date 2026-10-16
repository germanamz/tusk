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
- `FirstHeading(source []byte) string` — the title of the first top-level level-one heading (ATX or setext), cut the way a section title is; `""` when there is none. `node.ParseFileAs` titles a file adopted under the workspace default type with it (#790).
- `Unit` — one sub-document node. `Address` is the id suffix; `ContentHash` is the embed-payload fingerprint; `Hash` is the fallback id for kinds with no address rule; `BlockID` is the id an Obsidian `^block-id` marker gives the unit (see Block ids).
- `Sync.ApplyFile(ctx, fileRow, units)` — diffs the parsed units against existing rows **by address**, inserts/deletes/updates rows, rewrites `contains` edges, rewrites the outbound wikilink edges of every unit whose links differ from the stored ones, and enqueues embeds for new or content-changed leaves (sections are never enqueued). The reindex worker calls it for every page it re-parses, and `node.Service` calls it after `Create` and `Modify`, whose lease writes make the next reindex skip the page (#782).
- `DeriveEdges` — one edge per `Unit.Links` target per wikilink edge type. The markdown walker fills `Links` from the unit's byte span in the source with code blanked (`wikilink.MaskCodeIn`), so a link inside a code span or code block never becomes an edge (#788). A list item reads only its own paragraphs, not a sub-list nested between them. The HTML walker fills `Links` from the unit's text. Both go through [`internal/wikilink`](wikilink.md), the scanner the file-level edges use too.

## Structural address grammar

A sub-unit row id is `<fileID>#<address>`.

- **Section path** mirrors the heading outline: `S1`, `S1.1`, `S1.2.1`, `S2`. Dots show depth; multiple `H1`s are allowed; the path reflects nesting order, not the literal heading level (an `H3` directly under an `H1` is `S1.1`).
- **Leaves** reset per enclosing section (or document root), per kind, 1-based in document order:
  - paragraph `P`, code box `B`, blockquote `Q`, list item `L` (lists flatten → one sequential counter), table cell `T<k>R<row>C<col>` (header row is `R0`).
- **Root-level** content (before the first heading, or a heading-free note) gets a bare leaf suffix: `P1`, `B1`, `T1R0C0`, …
- **Fallback to hash:** a kind with no registered address rule falls back to a 12-hex content hash; a defensive pass disambiguates any colliding fallback ids with `-N`. Structural addresses start with an uppercase letter (`S/P/B/Q/L/T`); hash fallbacks are lowercase hex, so the two never collide on the first character.

Examples: `notes/standup#S1.2P3`, `notes/standup#S1.1T1R2C0`, `notes/note#P1`.

## Block ids

An Obsidian block reference, `[[note#^quote-1]]`, links to the unit a `^quote-1` marker names (#819). Ids are Latin letters, digits and dashes. `Parse` reads markers from the source with code blanked, so a marker inside code names nothing, and sets `Unit.BlockID`:

- A marker ending a paragraph, list item or quote, after a space or alone on the block's last line (`A quote worth keeping. ^quote-1`), names that unit and is cut from its text. The embed payload, title and `content_hash` are the text's without it, so adding or renaming a marker costs no vector. A marker that is all of a unit's text names nothing and stays.
- A top-level paragraph that is nothing but a marker, Obsidian's form for a list, quote, table or code block, names the first unit of the block before it (a list's first item, a table's header cell) and is not a unit itself. One after a heading, a rule or nothing names nothing and is dropped. A unit keeps the id it ends with over one on the line after it.

Each such row's `name_key` holds `wikilink.BlockKey` of the id (`^` and the id in lower case), which `index.ResolveLinks` matches. A section's `name_key` holds its heading key, so a block reference never matches a section.

## Stability contract

- **Section addresses are positional and independent of heading text.** Editing a heading keeps its address (and every descendant address); only the section row's title/text, `name_key` and `content_hash` change. A link that should follow a heading names it by text instead (`[[note#Some heading]]`): each section row's `name_key` holds `wikilink.HeadingKey` of its heading (`Unit.Heading`, read the same by the markdown and HTML walkers), which `index.ResolveLinks` matches (#808). Any edit *inside* a section also turns over the section's `content_hash` (it covers the full subtree text) without touching its address.
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

Each sub-unit row stores `content_hash` = sha256 of the **embed payload** (for leaves — a table cell's payload is `"<column-header>: <cell-text>"`), or sha256 of the section's `Text` for sections — for the markdown walker that is the heading **plus the full descendant body**, and for the HTML walker the heading alone. The hash does not decide edges: a list item's or table cell's text is goldmark's rendered text, which drops the backticks around a code span, so wrapping a link in backticks changes what the unit links to and leaves its hash alone. For leaves it drives the sync re-embed decision and is the key the embedding store dedupes on: identical content anywhere in the workspace is embedded once and shared.

## How indexing works

`Parse` walks the markdown once, assigning each unit its address and `content_hash`. `Sync.ApplyFile` then diffs those units against the file's existing sub-unit rows **by address**:

- address only in the new set → insert the row; enqueue an embed (leaves only).
- address in both, `content_hash` changed → update the row; re-enqueue the embed (leaves only).
- address in both, only ordinal, properties, line range or heading or block key changed → update the row; the payload is byte-identical, so the vector still holds and nothing is re-enqueued. An edit above a passage shifts its lines this way, and rows written before line ranges existed (NULL lines) fill this way.
- address in both, unchanged → leave the row untouched.
- address only in the old set → delete the row (its edges and embedding mapping cascade away).

Outbound wikilink edges converge separately. `ApplyFile` reads the file's stored edges in one query (`EdgeRepo.ListBySourcePath`), derives each unit's edges from its `Links`, and rewrites only the units whose two sets differ. A unit's links are shaped the way the file's are: a relative path becomes the id it names, and a name (`[[auth-rfc]]`) or any `#fragment` is stored as written with the row's `Link` set, for `index.ResolveLinks` to resolve (#791, #808). The diff compares such a row by its link text rather than its target, since the stored row points at a node and the derived one still holds the text. A new unit with no links writes nothing, an edit that leaves a unit's links alone writes nothing for it, and a forced re-process heals any unit whose stored edges drifted from its source.

The embed drain (`internal/embed`) reads a leaf's `content_hash`; if a vector for that content already exists it just records the node→content mapping with no model call, otherwise it embeds once and stores the shared vector (see `internal/embed` and `internal/index` for the `embeddings` / `node_embeddings` tables). So a restructure that shifts addresses but not content costs no re-embedding, identical content across the workspace is embedded once, and `tusk doctor` measures embedding coverage by those mappings — sections, which are never embedded, are not flagged.

## Notes

Identity was a content hash (`<fileID>#<12hex>`, disambiguated with `-N`) before the 2026-06 structural-addressing change. Reserved node/edge/property names for the sub-document kinds live in the sibling `internal/typepacks/subdocument` pack.
