# Edge types

An edge has a typed kind (declared in `tusk.toml` under
`[edge-types.<name>]`), a source node, and a target node.

Edges live in the **source node's frontmatter**. Any top-level
frontmatter key whose name matches a declared edge type becomes an
edge after reindex. There are no out-of-band edge rows.

## Declaring an edge type

```toml
[edge-types.blocks]
from        = ["ticket"]              # or ["*"] for any source type
to          = ["ticket"]              # or ["*"] for any target type
cardinality = "many-to-many"          # one-to-one | one-to-many | many-to-one | many-to-many
```

After this declaration, a ticket node's frontmatter can write:

```yaml
type: ticket
blocks:
  - tickets/T-002
  - tickets/T-003
```

…and reindex materializes one edge per target.

## Optional knobs

- **`ordered = "<property>"`** — sort siblings by this property on each
  source node. Example: a WBS hierarchy ordered by an `order: int` on
  each child node.

  ```toml
  [edge-types.wbs-parent]
  from        = ["wbs-node"]
  to          = ["wbs-node"]
  cardinality = "many-to-one"
  ordered     = "order"
  hierarchy   = "wbs"
  ```

- **`hierarchy = "<alias>"`** — marks this edge type as a hierarchy.
  Unlocks `tree=`/`parent=`/`root=` filter shortcuts (and their
  qualified `tree:<alias>=` forms). See
  `tusk_help(topic: "filter")`. The child names its parent by
  default; set **`parent-is-source = true`** when the parent names its
  children (`has-part: [a, b]`). **`max-depth = N`** stops `tree=` and
  `root=` N levels out; unset, they walk the whole tree.

- **`inverse = "<label>"`** — names this edge walked backwards. Filters
  take the label as an edge type (`blocked-by->` is `blocks<-`), and
  `tusk_edge_list` takes it as `type`, swapping `from` and `to`. The
  edge is still written on its source, so `tusk_edge_add` and
  `tusk_edge_remove` refuse the label and name the write to make. A
  label can't also be an edge type, and two edge types can't share one.

- **`wikilinks = true`** — body wikilinks (`[[path/to/target]]`)
  materialize as edges of this type. A navigational shorthand useful
  for prose; distinct from typed frontmatter edges. A wikilink inside
  inline code or a code block is an example, not a link, and makes no
  edge. A target may also be relative to the page (`[[../notes/x]]`)
  or a bare file name, as Obsidian writes it (`[[x]]`): the note whose
  id is the name wins, else the one file with that name. A name two
  files share stays unresolved and `tusk_doctor` reports it as
  `ambiguous-wikilink`. `[workspace] link-case` picks whether names
  match in any letter case (default) or exactly. A `#fragment` links
  to a section: a structural address (`[[x#S1.2]]`), or heading text
  (`[[x#Open questions]]`, case and punctuation ignored; the first
  matching heading, or `[[x#Parent#Child]]` for one inside another).
  `[[#Heading]]` names a heading of the page itself. `[[x#^id]]` links
  to the paragraph, list item or block an Obsidian `^id` marker names
  (any case). A heading or id the note lacks leaves the edge on the
  note, and `tusk_doctor` reports `missing-heading` / `missing-block`.

- **`paths = true`** records the workspace paths a page names as path
  refs of this type: inline code (`` `server/ledger/core/service.go` ``),
  where the whole span must be a workspace-relative path (a trailing
  `:42` or `:40-52` is dropped; a bare `Makefile`, `Dockerfile` or
  `LICENSE` counts too), and links and images
  (`[the service](../server/ledger/core/service.go)`), resolved against
  the page's folder. `from` picks which pages are scanned, and a paths
  type may leave out `to` and `cardinality`. Path refs target paths, not
  nodes, so source trees can stay in `[workspace] ignore`. Find the pages
  that name a file with `names-path=<path>` (see
  `tusk_help(topic: "filter")`); `tusk_doctor` warns with `path-missing`
  when a named path is gone.

  ```toml
  [edge-types.describes]
  from  = ["technical"]
  paths = true
  ```

  With no `to`, the type allows no node targets, so its frontmatter
  values are paths too: `describes: [server/ledger/core/service.go]`
  is a path ref on that line, and `tusk_edge_add` /
  `tusk_edge_remove` maintain it. A type that declares `to` keeps
  reading frontmatter values as node ids.

## Using tusk_edge_add vs editing the file

`tusk_edge_add` is a convenience: it mutates the source file's
frontmatter exactly as if you opened the file and added the key by
hand, then refreshes the index for that file. `tusk_edge_remove` is
the symmetric operation. The file remains the source of truth —
`git pull && tusk_reindex` reproduces the full edge graph from disk.

## Derived edges

`ref` and `list-of(ref)` properties on a node type produce edges
automatically; you do not need to declare an `[edge-types.<name>]`
block for them. See `tusk_help(topic: "node-types")`.
