---
title: tusk node render
---

## tusk node render

Render a node's content as plain text (tags / markup stripped)

### Synopsis

Render a node's content as plain text.

HTML nodes have their tags stripped and entities decoded; markdown nodes have
their markup removed. The output is "just the words" — useful for piping a node
into a tool that wants prose, not markup.

The node id is the workspace-relative path: markdown nodes drop the extension
(notes/hello.md has id "notes/hello"), HTML nodes retain it (page.html has id
"page.html"). Render is read-only — it never touches files or index state.

```
tusk node render <node-id> [flags]
```

### Examples

```
  # Strip markdown markup
  tusk node render notes/hello

  # Strip HTML tags
  tusk node render page.html
```

### Options

```
  -h, --help   help for render
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk node](tusk_node.md)	 - Manage individual nodes (create, get, render, list, modify, move, delete)

