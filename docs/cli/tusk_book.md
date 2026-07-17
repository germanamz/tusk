---
title: tusk book
---

## tusk book

Serve a read-only reading view of the vault

### Synopsis

Serve a local, read-only reading UI for the vault: rendered node
documents (with math, mermaid diagrams, and images), semantic search, and
graph-expansion navigation, kept live as files change.

The server binds to loopback by default. It does not open a browser
automatically: press space in this terminal to open it, or pass --open.

```
tusk book [flags]
```

### Examples

```
  # Serve on 127.0.0.1:7474 and press space to open
  tusk book

  # Open the browser automatically
  tusk book --open

  # Bind a specific loopback port
  tusk book --addr 127.0.0.1:9001
```

### Options

```
      --addr string   loopback listen address (default "127.0.0.1:7474")
  -h, --help          help for book
      --open          open the browser automatically at startup
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first memory for agents: index a markdown + HTML vault into a graph

