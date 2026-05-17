---
title: tusk node get
---

## tusk node get

Print the markdown file for a node by id

### Synopsis

Print the full markdown file (frontmatter + body) for a node by id.

The node id is the workspace-relative path without extension (e.g. a node
file at notes/hello.md has id "notes/hello"). Output goes to stdout
verbatim — useful for piping into editors, less, or another tusk command.

```
tusk node get <node-id> [flags]
```

### Examples

```
  # Print a node
  tusk node get notes/hello

  # Open in $EDITOR (round-trip through a temp file)
  tusk node get notes/hello > /tmp/hello.md && $EDITOR /tmp/hello.md
```

### Options

```
  -h, --help   help for get
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk node](tusk_node.md)	 - Manage individual nodes (create, get, list, modify, move, delete)

