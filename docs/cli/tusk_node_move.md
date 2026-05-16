---
title: tusk node move
---

## tusk node move

Atomically rename a node and rewrite all referring edges

### Synopsis

Atomically rename a node file and rewrite every edge that points at it.

The new path is workspace-relative and must include a markdown extension.
All other node files that reference the old id are rewritten in the same
transaction, so the index never observes a broken state.

```
tusk node move <old-id> <new-rel-path> [flags]
```

### Examples

```
  # Move a note into a subdirectory
  tusk node move notes/hello notes/intros/hello.md

  # After move, confirm references were rewritten
  tusk edge list --to notes/intros/hello
```

### Options

```
  -h, --help   help for move
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk node](tusk_node.md)	 - Manage individual nodes (create, get, list, modify, move, delete)

