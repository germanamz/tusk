---
title: tusk
---

## tusk

Local-first agent brain: index a markdown vault into a graph

### Synopsis

Tusk turns a directory of markdown files into a schema-validated,
semantically-indexed graph. Files (markdown + tusk.toml) are the source of
truth; git is the history; tusk is the indexer and retrieval engine.

Run "tusk init" to create a workspace, "tusk node create" to add content,
"tusk reindex" or "tusk watch" to keep the index live, "tusk query" /
"tusk node list" to retrieve, and "tusk mcp" to expose the same surface to
an MCP-compatible agent.

Most read/write verbs are also exposed as MCP tools (tusk_query,
tusk_node_create, …) so agents can use the same surface without a shell.

### Examples

```
  # Bootstrap a fresh vault and verify it
  tusk init --name my-brain
  tusk doctor

  # Index existing markdown files on disk
  tusk reindex

  # Run the MCP server for Claude Code / Cursor
  tusk mcp
```

### Options

```
  -h, --help      help for tusk
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk doctor](tusk_doctor.md)	 - Surface validation warnings, dangling edges, and index health issues
* [tusk edge](tusk_edge.md)	 - Manage edges between nodes (add, remove, list)
* [tusk init](tusk_init.md)	 - Initialize a Tusk workspace in the current directory
* [tusk mcp](tusk_mcp.md)	 - Run the long-running MCP server (stdio or SSE)
* [tusk node](tusk_node.md)	 - Manage individual nodes (create, get, list, modify, move, delete)
* [tusk pack](tusk_pack.md)	 - Install and manage built-in type packs
* [tusk query](tusk_query.md)	 - Run a structural, semantic, or hybrid query against the index
* [tusk reindex](tusk_reindex.md)	 - Walk the workspace and bring the index up to date with disk
* [tusk run](tusk_run.md)	 - Run a manifest-declared alias by name
* [tusk status](tusk_status.md)	 - Print a one-screen workspace summary
* [tusk watch](tusk_watch.md)	 - Watch the workspace for external edits and keep the index in sync

