# Node types

A node is one markdown file with YAML frontmatter declaring its `type`
and properties. Every type must be declared in `./tusk.toml` under
`[node-types.<type>]` before `tusk_node_create` (or a manual file
write + `tusk_reindex`) will accept it.

## Anatomy

```
---
type: ticket           # required — picks the node-type schema
title: Fix login bug   # reserved; optional
priority: high         # property (declared in node-type)
blocks:                # edge key (declared in edge-types)
  - tickets/T-002
---

# Fix login bug

...body...
```

Top-level frontmatter keys split into two namespaces, enforced by the
manifest at load time:

- **Property keys** (e.g. `priority`, `title`) — declared in
  `[node-types.<type>].properties` in `tusk.toml`.
- **Edge keys** (e.g. `blocks`) — names that match an
  `[edge-types.<name>]` declaration. The value is a target node id
  (scalar) or list of target ids; reindex turns each into an indexed
  edge. See `tusk_help(topic: "edge-types")`.

Reserved keys: `type` (required), `title`, and any `status-property`
declared by an active behavior pack.

## Declaring a type

```toml
[node-types.ticket]
properties = [
    { name = "priority",  type = "string" },
    { name = "due",       type = "date" },
    { name = "estimate",  type = "int" },
    { name = "tags",      type = "list-of", item-type = "ref", to = "tag" },
]
```

Property `type` values: `string`, `int`, `float`, `bool`, `date`,
`datetime`, `duration`, `ref` (typed reference to another node), and
`list-of` (with `item-type`).

`ref` properties become **derived edges** at reindex time — they show
up in `tusk_edge_list` and graph expansion automatically. No separate
`[edge-types.<name>]` block is required for them.

## After editing tusk.toml

Run `tusk_reindex`. Existing nodes that match the new schema validate
cleanly; any that don't surface as drift in `tusk_doctor`.

## Sub-units

If `[workspace].sub-units = true`, the markdown headings inside each
file are also parsed as sub-unit nodes (one per heading, type
`section`, kind `subunit`, source `markdown`). These appear in
`tusk_query` and `tusk_node_list` alongside the file rows. They share
their parent file's path.
