---
title: tusk pack add
---

## tusk pack add

Copy a type pack's declarations into tusk.toml

### Synopsis

Copy a type pack's node and edge type declarations into tusk.toml.

The pack is a built-in name (kanban, tags, vault) or a URL. Built-in
names are fetched over the network from the project's published packs, so
adding one by name needs connectivity; pass a full URL (or a file:// URL)
to install from elsewhere.

Idempotent for a given pack: re-running with the same pack is a no-op
unless --force is set, in which case any colliding sections in tusk.toml
are removed before the pack is appended.

```
tusk pack add <name-or-url> [flags]
```

### Examples

```
  # Add the kanban pack and verify the manifest is still valid
  tusk pack add kanban
  tusk doctor

  # Re-add a pack that already exists, replacing collisions
  tusk pack add kanban --force
```

### Options

```
      --force   remove colliding sections from tusk.toml before appending the pack
  -h, --help    help for add
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk pack](tusk_pack.md)	 - Install and manage type packs

