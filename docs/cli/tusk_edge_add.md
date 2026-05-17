---
title: tusk edge add
---

## tusk edge add

Add a typed edge from one node to another

### Synopsis

Add a typed edge from one node to another.

The edge kind must be declared in tusk.toml. CLI-added edges are
attributed to a synthetic "__cli__" source path so the next reindex of
either involved file does not clobber them.

When --ordinal is unset (-1), the next free ordinal is auto-assigned
across the same (source, type) group of CLI-added edges. Pass --ordinal
to control placement explicitly — useful for ordered edges where the
intended ordering key is the target (e.g. WBS child ordering under a
shared parent).

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

  # Order children explicitly under a shared parent
  tusk edge add --type wbs-parent --source wbs/proj/s1 --target wbs/proj --ordinal 0
  tusk edge add --type wbs-parent --source wbs/proj/s2 --target wbs/proj --ordinal 1
```

### Options

```
  -h, --help            help for add
      --ordinal int     edge ordinal (>= 0); -1 (default) auto-assigns the next free value for this (source, type) group (default -1)
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

