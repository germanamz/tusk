# Type packs

A type pack is a bundle of node-type and edge-type declarations
shipped with Tusk that can be merged into the workspace manifest.
Use packs to seed a new workspace with sensible defaults instead of
writing the schema by hand.

## Installing

Call `tusk_pack_add(pack: "<name>")`. It merges the pack's declarations
into `./tusk.toml` and hot-reloads the schema in one step. Re-running is
safe; pass `force: true` to replace a section that would otherwise
collide. (The same operation is `tusk pack add <name>` from the shell.)

## After installing

`tusk_pack_add` reindexes as part of the reload, so the new types apply
to existing files immediately. Run `tusk_doctor` to surface any drift the
merge introduced.

## Built-in pack types vs user-declared types

User-declared and pack-derived declarations live in distinct
namespaces. A user type `section` and a built-in `markdown:section`
(produced by the sub-unit pipeline) coexist without conflict; they
appear as distinct rows in `tusk_node_list` and `tusk_query` results
with different `source` values (`NULL` for user, `"markdown"` for
pack).

## Discovering packs

The built-in packs are `vault`, `tags`, and `kanban`. Pass any of those
names to `tusk_pack_add`, or a full `http(s)://…/pack.toml` URL.
