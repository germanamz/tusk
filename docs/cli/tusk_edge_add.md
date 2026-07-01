---
title: tusk edge add
---

## tusk edge add

Add a typed edge from one node to another

### Synopsis

Add a typed edge from one node to another by writing the edge into the
source node's frontmatter.

The edge kind must be declared in tusk.toml's `[edge-types.<name>]`. The
source's node type must be in the edge's "from" list, and the target's
node type must be in the edge's "to" list.

What this command actually does:

  1. Reads the source file's current frontmatter.
  2. Adds the target under the edge-name key, respecting cardinality:
       * one-to-one / many-to-one: scalar string; rejects on conflict.
       * one-to-many / many-to-many: list; appends if absent (dedup).
  3. Atomically rewrites the file with the new frontmatter.
  4. Reindexes the source file so the new edge is queryable immediately.

Idempotent: adding an edge that already exists is a no-op. To replace a
single-target edge, run "tusk edge remove" first.

The change is durable: the edge lives in git-tracked markdown, not in the
index database. Running "rm .tusk/index.db && tusk reindex" recovers the
same graph state.

```
tusk edge add [flags]
```

### Examples

```
  # Mark T-001 as blocking T-002
  tusk edge add --type blocks --source tickets/T-001 --target tickets/T-002

  # Add multiple edges as part of a script
  tusk edge add --type mentions --source tickets/T-003 --target notes/2026-05-16
  tusk edge add --type owned-by --source tickets/T-003 --target people/alice
```

### Options

```
  -h, --help            help for add
      --source string   source node id (workspace-relative path without extension)
      --target string   target node id
      --type string     edge type (must be declared in tusk.toml)
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk edge](tusk_edge.md)	 - Manage edges between nodes (add, remove, list)

