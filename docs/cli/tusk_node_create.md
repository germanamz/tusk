---
title: tusk node create
---

## tusk node create

Create a new node file and index it

### Synopsis

Create a new node file and index it.

Writes a markdown file with YAML frontmatter at the given workspace-relative
path, validates the declared type against tusk.toml, and inserts the node
into the index in a single locked transaction. Body content can be piped on
stdin; if stdin is a terminal, the body is empty.

Property values from --prop are parsed as int, then bool, then string. Use
--prop key=value to set multiple values (repeatable).

Edges are set the same way as properties: pass --prop <edge-name>=<target-id>
or write the key directly in the frontmatter of the file you supply. The
reindex pass will materialize the edge from the markdown frontmatter.

```
tusk node create [flags]
```

### Examples

```
  # Create a ticket node with two properties
  tusk node create --type ticket --path tickets/T-001.md \
      --prop priority=1 --prop status=open

  # Pipe body content from another tool
  echo "Investigation notes" | tusk node create \
      --type note --path notes/2026-05-16.md

  # Create, then attach an edge to another node
  tusk node create --type ticket --path tickets/T-002.md
  tusk edge add --type blocks --source tickets/T-002 --target tickets/T-001
```

### Options

```
  -h, --help               help for create
      --path string        workspace-relative path with extension (e.g. notes/hello.md)
      --prop stringArray   set property: --prop key=value (repeatable)
      --title string       optional node title
      --type string        node type (e.g. ticket, note)
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk node](tusk_node.md)	 - Manage individual nodes (create, get, render, list, modify, move, delete)

