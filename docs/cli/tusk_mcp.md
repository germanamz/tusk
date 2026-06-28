---
title: tusk mcp
---

## tusk mcp

Run the long-running MCP server (stdio or SSE)

### Synopsis

Run the Tusk MCP server.

Transports:
  stdio   reads JSON-RPC over stdin, writes over stdout (default)
  sse     listens for SSE clients on --addr (default 127.0.0.1:8765, loopback)

The SSE transport exposes the full write surface (node/edge create, modify,
delete, and reset) with no authentication, so it binds loopback by default.
Binding a non-loopback address requires interactive confirmation.

The server holds the workspace open for the lifetime of the session, drains
the embed queue in the background, and watches the workspace for external
edits.

Worker pool: TUSK_EMBED_WORKERS overrides [embeddings] workers in tusk.toml;
both default to max(1, NumCPU/2). Setting the pool size to 0 turns this
instance into a read-only server — the embed drainer, reindex drainer, and
file watcher are all skipped, so another instance (or a scheduled tusk
reindex) must keep the index fresh.

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
      --addr string        SSE listen address (loopback by default; only used when --transport sse) (default "127.0.0.1:8765")
  -h, --help               help for mcp
      --transport string   transport: stdio | sse (default "stdio")
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first memory for agents: index a markdown + HTML vault into a graph

