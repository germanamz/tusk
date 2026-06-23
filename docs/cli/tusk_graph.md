---
title: tusk graph
---

## tusk graph

Serve an interactive 3D graph view of the vault

### Synopsis

Serve a local, read-only 3D graph of the vault (nodes + edges) and
keep it live as files change.

The server binds to loopback by default. It does not open a browser
automatically: press space in this terminal to open it, or pass --open.

```
tusk graph [flags]
```

### Examples

```
  # Serve on 127.0.0.1:7373 and press space to open
  tusk graph

  # Open the browser automatically
  tusk graph --open

  # Bind a specific loopback port
  tusk graph --addr 127.0.0.1:9000
```

### Options

```
      --addr string   loopback listen address (default "127.0.0.1:7373")
  -h, --help          help for graph
      --open          open the browser automatically at startup
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first memory for agents: index a markdown + HTML vault into a graph

