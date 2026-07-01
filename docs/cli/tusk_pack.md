---
title: tusk pack
---

## tusk pack

Install and manage type packs

### Synopsis

Manage type packs.

A type pack is a bundle of node-type and edge-type declarations that
"tusk pack add" copies into the workspace manifest. Use packs to seed a
new workspace with sensible defaults (e.g. the "kanban" pack with a
ticket workflow) instead of writing the schema by hand.

A pack is named or a URL. Built-in names (kanban, tags, vault) resolve
to the project's published pack files and are fetched over the network,
so adding one by name needs connectivity; pass a full URL (or a file://
URL for a local copy) to install from elsewhere.

### Options

```
  -h, --help   help for pack
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first memory for agents: index a markdown + HTML vault into a graph
* [tusk pack add](tusk_pack_add.md)	 - Copy a type pack's declarations into tusk.toml

