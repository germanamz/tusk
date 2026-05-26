# Type packs

A type pack is a bundle of node-type and edge-type declarations
shipped with Tusk that can be merged into the workspace manifest.
Use packs to seed a new workspace with sensible defaults instead of
writing the schema by hand.

## Installing

There is no MCP tool for pack installation — run from the shell:

```
tusk pack add <name>
```

This copies the pack's declarations into `./tusk.toml`. Re-running
the command is safe (idempotent merge).

## After installing

1. `tusk_reindex` — the new types now apply to existing files.
2. `tusk_doctor` — surfaces any drift introduced by the merge.

## Built-in pack types vs user-declared types

User-declared and pack-derived declarations live in distinct
namespaces. A user type `section` and a built-in `markdown:section`
(produced by the sub-unit pipeline) coexist without conflict; they
appear as distinct rows in `tusk_node_list` and `tusk_query` results
with different `source` values (`NULL` for user, `"markdown"` for
pack).

## Discovering packs

`tusk pack --help` from the shell lists available packs.
