---
title: tusk node delete
---

## tusk node delete

Delete a node file and remove it from the index

### Synopsis

Delete a node file from disk and remove it from the index.

Edges pointing at the deleted node remain in the index as dangling
references; "tusk doctor" will surface them. Use "tusk node move" if you
want to rename rather than remove.

```
tusk node delete <node-id> [flags]
```

### Examples

```
  # Delete a stale note
  tusk node delete notes/2024-old

  # See which edges are now dangling
  tusk doctor
```

### Options

```
  -h, --help   help for delete
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk node](tusk_node.md)	 - Manage individual nodes (create, get, list, modify, move, delete)

