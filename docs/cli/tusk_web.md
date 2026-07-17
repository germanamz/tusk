---
title: tusk web
---

## tusk web

Serve the unified web app: 3D graph + reading views

### Synopsis

Serve a local, read-only web app for the vault, combining the 3D graph
view and the reading view behind one server, and keep it live as files change.

Switch between the two views from the top bar, or deep-link a view directly:
"/" opens the graph, "/read" opens the reader. A light/dark theme toggle in the
header follows your system by default and remembers your choice.

The graph view groups nodes by a configurable cluster lens (set under
[graph.cluster] in tusk.toml), sizes and brightens them by degree, and lets you
filter by type or edge kind, inspect neighbors, and walk sub-units. The reading
view renders node documents (math, mermaid diagrams, images), offers semantic
search, and surfaces graph-expansion neighbors.

The server binds to loopback by default. It does not open a browser
automatically: press space in this terminal to open it, or pass --open.

```
tusk web [flags]
```

### Examples

```
  # Serve on 127.0.0.1:7373 and press space to open
  tusk web

  # Open the browser automatically on the reading view
  tusk web --open --view read

  # Bind a specific loopback port
  tusk web --addr 127.0.0.1:9000
```

### Options

```
      --addr string   loopback listen address (default "127.0.0.1:7373")
  -h, --help          help for web
      --open          open the browser automatically at startup
      --view string   initial view to open: graph|read (default "graph")
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first memory for agents: index a markdown + HTML vault into a graph

