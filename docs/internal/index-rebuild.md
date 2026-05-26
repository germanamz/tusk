# Index Rebuild on Schema Change

This page documents the schema-version contract that lets tusk
detect an incompatible on-disk index and rebuild it from source.

## When to bump `SchemaVersion`

`internal/index.SchemaVersion` is the on-disk schema generation.
Bump it whenever the schema shape changes in a way that an
in-place migration cannot bridge:

- New `NOT NULL` columns added to existing tables
- New `CHECK` constraints
- Modified `UNIQUE` constraints
- Changed index predicates
- Any DDL that the existing `CREATE TABLE IF NOT EXISTS` path
  cannot apply to an existing table

Adding tables, adding indexes, and other purely additive changes
do not require a bump.

## Rebuild flow

`index.Open` reads `meta.schema_version` and compares it against
`SchemaVersion`. A mismatch returns `*index.SchemaVersionError`
(wrapped `ErrSchemaIncompatible`).

`internal/workspace/indexopen.OpenOrRebuild` catches the sentinel,
deletes the on-disk file, re-opens (writing the current
`SchemaVersion` into a fresh database), and runs `reindex.Run` to
repopulate from source. Every CLI command and the MCP runtime go
through this helper.

## User experience

First invocation after upgrade emits one log line:

```
index schema changed in this version, rebuilding…
```

…then runs a full reindex. Cost is identical to
`tusk reindex --force`. Subsequent invocations open instantly.
