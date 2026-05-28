---
title: tusk reindex
---

## tusk reindex

Walk the workspace and bring the index up to date with disk

### Synopsis

Walk the workspace and bring the SQLite index up to date with disk.

Reindex compares each file's mtime, size, and checksum against the index
and re-parses only changed files. Embedding refreshes for changed nodes
happen lazily — run "tusk watch" alongside, or in the background, to
drain the embedding queue.

Run reindex after editing files outside Tusk (vim, Obsidian, scripts) or
after changing node/edge declarations in tusk.toml.

Worker pool: the embed/reindex pool size resolves from TUSK_EMBED_WORKERS,
then [embeddings] workers in tusk.toml, then max(1, NumCPU/2). Setting
the pool size to 0 opts out: this invocation walks and enqueues but does
not drain — another instance (or a later tusk reindex run) must drain
the queue.

```
tusk reindex [flags]
```

### Examples

```
  # Catch the index up with disk
  tusk reindex

  # Pair with watch for continuous indexing while you author
  tusk watch &
  tusk reindex
```

### Options

```
  -h, --help   help for reindex
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first agent brain: index a markdown vault into a graph

