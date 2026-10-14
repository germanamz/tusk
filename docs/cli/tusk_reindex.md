---
title: tusk reindex
---

## tusk reindex

Walk the workspace and bring the index up to date with disk

### Synopsis

Walk the workspace and bring the SQLite index up to date with disk.

Reindex skips any file whose mtime and size are unchanged since the last
pass and re-parses only the rest. Pass --force to re-read, re-hash, and
re-process every file regardless.

When [embeddings] is configured, reindex then embeds the nodes it queued. If
the embedding backend can't be reached (Ollama stopped, or a CI runner without
it), the index is still built: the summary says how many nodes stay queued,
reindex exits 0, and a later reindex, "tusk watch" or "tusk mcp" embeds them.
Pass --strict-embed to exit 1 in that case, or --no-embed to skip embedding.

Run reindex after editing files outside Tusk (vim, Obsidian, scripts) or
after changing node/edge declarations in tusk.toml. For a schema change on a
running daemon, prefer "tusk reload" (or the tusk_reload MCP tool): it swaps
the in-memory schema and kicks a reindex that re-validates the index against
the new node-types, edge-types, and behaviors, surfacing violation counts in
the reindex output and "tusk doctor".

Worker pool: the embed/reindex pool size resolves from TUSK_EMBED_WORKERS,
then [embeddings] workers in tusk.toml, then max(1, NumCPU/2). Setting
the pool size to 0 opts out: this invocation walks and enqueues but does
not drain — another instance (or a later tusk reindex run) must drain
the queue.

Sub-unit addresses: each markdown sub-unit is indexed under a structural
address appended to the file id, e.g. notes/doc#S1.2P3. Editing prose in place
keeps a unit's address and re-embeds only the changed content; restructuring
shifts addresses but reuses unchanged vectors, so a reorder does not re-embed.

```
tusk reindex [flags]
```

### Examples

```
  # Catch the index up with disk
  tusk reindex

  # Build the index without embedding (no Ollama needed)
  tusk reindex --no-embed

  # Fail when embeddings can't be refreshed
  tusk reindex --strict-embed

  # Pair with watch for continuous indexing while you author
  tusk watch &
  tusk reindex
```

### Options

```
      --force          re-process every file even if its mtime and size are unchanged
  -h, --help           help for reindex
      --no-embed       build the structural index only; leave the embedding queue for a later run
      --strict-embed   exit 1 when the embedding backend is unreachable
```

### Options inherited from parent commands

```
  -v, --verbose         emit debug-level logs to stderr
  -C, --workspace dir   run as if started in dir: find tusk.toml from there instead of the working directory (default $TUSK_WORKSPACE)
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first memory for agents: index a markdown + HTML vault into a graph

