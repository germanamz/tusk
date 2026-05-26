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
  `tusk_help(topic: "filter")`.

- **`wikilinks = true`** — body wikilinks (`[[path/to/target]]`)
  materialize as edges of this type. A navigational shorthand useful
  for prose; distinct from typed frontmatter edges.

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
