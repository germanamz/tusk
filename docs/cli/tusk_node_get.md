---
title: tusk node get
---

## tusk node get

Print the source file (markdown or HTML) for a node by id

### Synopsis

Print the source file (frontmatter/markup + body) for a node by id.

The node id is the workspace-relative path: markdown nodes drop the
extension (notes/hello.md has id "notes/hello"), while HTML nodes retain
it (page.html has id "page.html").

By default (no flags) the command prints the raw file to stdout
verbatim — useful for piping into editors, less, or another tusk command.
When --include, --fields, --format, or --json is passed the command emits
structured output instead (compact for TTY, JSON otherwise).

```
tusk node get <node-id> [flags]
```

### Examples

```
  # Print the raw file
  tusk node get notes/hello

  # Structured JSON envelope with only the body
  tusk node get notes/hello --include body --format json

  # Open in $EDITOR (round-trip through a temp file)
  tusk node get notes/hello > /tmp/hello.md && $EDITOR /tmp/hello.md
```

### Options

```
      --fields strings    project returned shape to these fields (comma-separated)
      --format string     output format: compact|json (default: compact for TTY, json otherwise)
  -h, --help              help for get
      --include strings   expand returned shape: body|edges|properties (comma-separated)
      --json              emit structured JSON (sugar for --format json)
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk node](tusk_node.md)	 - Manage individual nodes (create, get, render, list, modify, move, delete)

