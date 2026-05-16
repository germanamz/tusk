---
type: package
title: internal/mcp — MCP server
import-path: github.com/germanamz/tusk/internal/mcp
status: stable
---

# internal/mcp

MCP server. Bundles the open workspace + index + services into a `Runtime`, registers a tool per CLI verb on the mcp-go core, and serves over stdio (default) or SSE. Runs the embed-queue drainer + fsnotify watcher in the same process so the index stays warm across tool calls.

## Public surface

- `Open(workspaceDir string) (*Runtime, error)` — long-lived bundle.
- `(*Runtime).Close()`.
- `NewServer(*Runtime) *Server` — wraps mcp-go.
- `(*Server).RunBackground(ctx) error` — drainer + watcher.
- Tools: `tusk_status`, `tusk_node_get`, `tusk_node_list`, `tusk_query`, `tusk_doctor`, `tusk_node_create`, `tusk_node_modify`, `tusk_node_move`, `tusk_node_delete`, `tusk_edge_add`, `tusk_edge_remove`, `tusk_reindex`.

## Notes

Workspace-config commands stay CLI-only in v1.c — `tusk pack add` has no MCP equivalent yet (carried as 7.c.1 §10 ledger #10). Structured warnings via stderr text-line parsing remains a v1 expediency (Plans 7, 7.b, 7.c.1 residuals).
