---
title: tusk reset
---

## tusk reset

Drop the local index and rebuild it from source files

### Synopsis

Delete the local SQLite index (.tusk/index.db and its WAL/SHM
sidecars) and rebuild it from scratch by walking the workspace.

Unlike "tusk reindex" — which never deletes and only re-parses changed files —
reset destroys the index entirely, so it recovers from a corrupt or wedged
index. The markdown files are the source of truth, so nothing is lost; the
rebuild re-derives every node, edge, and embedding from disk.

Reset re-embeds every node, which can be expensive with a local embedding
model. It requires confirmation; pass --yes to skip the prompt.

```
tusk reset [flags]
```

### Examples

```
  # Drop and rebuild the index, with confirmation
  tusk reset

  # Non-interactive (e.g. scripts/agents)
  tusk reset --yes
```

### Options

```
  -h, --help   help for reset
      --yes    skip the confirmation prompt
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first memory for agents: index a markdown + HTML vault into a graph

