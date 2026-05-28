---
title: tusk mcp
---

## tusk mcp

Run the long-running MCP server (stdio or SSE)

### Synopsis

Run the Tusk MCP server.

Transports:
  stdio   reads JSON-RPC over stdin, writes over stdout (default)
  sse     listens for SSE clients on --addr (default :8765)

The server holds the workspace open for the lifetime of the session, drains
the embed queue in the background, and watches the workspace for external
edits.

Worker pool: TUSK_EMBED_WORKERS overrides [embeddings] workers in tusk.toml;
both default to max(1, NumCPU/2). Setting the pool size to 0 turns this
instance into a read-only server — the embed and reindex drainers are not
started, so another instance (or a scheduled tusk reindex) must keep the
index fresh. The file watcher still runs and still enqueues work for
whichever instance is draining.

```
tusk mcp [flags]
```

### Examples

```
  # Default: stdio transport (Claude Code, Cursor, Zed)
  tusk mcp

  # SSE transport bound to loopback for browser-based clients
  tusk mcp --transport sse --addr 127.0.0.1:8765

  # Verify the workspace is healthy first
  tusk doctor && tusk mcp
```

### Options

```
      --addr string        SSE listen address (only used when --transport sse) (default ":8765")
  -h, --help               help for mcp
      --transport string   transport: stdio | sse (default "stdio")
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first agent brain: index a markdown vault into a graph

