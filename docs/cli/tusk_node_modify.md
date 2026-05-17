---
title: tusk node modify
---

## tusk node modify

Modify a node's frontmatter properties

### Synopsis

Modify a node's frontmatter properties without touching its body.

Use --prop key=value (repeatable) to set values and --unset key
(repeatable) to remove them. Values are typed the same way as in
"node create": int, then bool, then string.

The operation runs under the workspace lock so concurrent watcher reindex
cannot interleave.

```
tusk node modify <id> [flags]
```

### Examples

```
  # Change a ticket's status and priority
  tusk node modify tickets/T-001 --prop status=in-progress --prop priority=2

  # Remove a property entirely
  tusk node modify tickets/T-001 --unset blocked-by
```

### Options

```
  -h, --help                help for modify
      --prop stringArray    set property: --prop key=value (repeatable)
      --unset stringArray   unset property: --unset key (repeatable)
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk node](tusk_node.md)	 - Manage individual nodes (create, get, list, modify, move, delete)

