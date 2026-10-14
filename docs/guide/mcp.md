# The MCP server and the Claude Code plugin

`tusk mcp` runs an MCP server backed by the same indexing engine as the CLI, exposing the graph verbs as tools. Agents should prefer these tools over shelling out to `tusk`: they run in the warm daemon with the index already open. Stdio is the default transport; SSE is available on a loopback port.

```bash
tusk mcp                                          # stdio (Claude Code, Cursor, Codex, …)
tusk mcp --transport sse --addr 127.0.0.1:8765    # SSE
```

It holds the workspace open for the lifetime of the session: a single SQLite handle, an embed-queue drainer, and an fsnotify watcher all live in the same process so the index stays warm across tool calls.

The server finds its workspace by walking up from the directory it starts in to the nearest `tusk.toml`, and exits with `no tusk.toml in <dir> or any parent directory` when there isn't one. A client that starts servers somewhere else (Claude Desktop starts them in `/`) points it at the vault with `--workspace`:

```bash
tusk mcp --workspace ~/notes
```

`-C` is the short form, and `TUSK_WORKSPACE` does the same for a client whose config sets `env` but not `args`. The flag wins over the variable, and the variable over the starting directory. Every other command takes the same flag. Per-client configuration, including which clients start the server in your project folder, is in the README under [Connect your agent](../../README.md#connect-your-agent).

## The Claude Code plugin

Run this once in the workspace root (or pass `--claude` to `tusk init`):

```bash
tusk claude install
```

It writes a Claude Code plugin to `.claude/skills/tusk/`, which Claude Code loads for any session started in that folder. The plugin does two things:

- It runs the tusk MCP server, so the agent has the `tusk_*` tools. Claude Code holds a project plugin's server until each user approves it, so install approves it for you in `.claude/settings.local.json`, your uncommitted per-user settings. If `.mcp.json` already runs a tusk server, install removes that entry so the tools aren't registered twice.
- When a conversation starts, it adds a `tusk` block to the first message, next to CLAUDE.md. The block opens with a short orientation (the declared node types with counts, edge types, aliases, whether the index is current), followed by the `[context]` digest when `tusk.toml` declares one. `/clear` and compaction read it again. It's capped at `[context] max-bytes` (16384 by default); over that, alias sections go first, then recent nodes, then pinned ones. Run `tusk claude context` to see exactly what a session gets.

When an edge type sets `paths = true`, the plugin also reminds the agent of the pages that name a file right after it edits that file (see [path refs](workspace.md#pages-name-the-files-they-describe)).

The plugin folder can be committed, so teammates get it on clone if they have `tusk` on their PATH. Each of them approves the MCP server once, by running `tusk claude install` or in `/mcp`. Run `tusk claude install` again after upgrading tusk, because sessions warn when the plugin is older than the binary. `tusk claude status` reports what's installed, and `tusk claude uninstall` removes it.

To wire up only the MCP server by hand:

```bash
claude mcp add tusk -- tusk mcp
```

## Available MCP tools

| Tool | What it does |
|------|--------------|
| `tusk_status` | node counts by type, edge count, queue depth, last reindex |
| `tusk_doctor` | health check: every issue has a severity (`error` / `warning` / `advice`); `error_count > 0` means something is broken |
| `tusk_node_get` / `tusk_node_list` | read by id or filter |
| `tusk_node_render` | render a node's content as plain text (HTML tags / markdown markup stripped) |
| `tusk_node_create` / `tusk_node_modify` / `tusk_node_move` / `tusk_node_delete` | write |
| `tusk_edge_add` / `tusk_edge_remove` / `tusk_edge_list` | edge CRUD |
| `tusk_query` | structural + optional `semantic` ranking |
| `tusk_context` | composed warm-context digest (pinned nodes, recent activity, aliases) |
| `tusk_run` | invoke a manifest-declared alias by name |
| `tusk_reindex` | force a full walk |
| `tusk_reload` | hot-reload `tusk.toml`: validate + swap the schema, no restart |
| `tusk_reset` | drop and rebuild the index from files (`confirm: true`) |
| `tusk_pack_add` | merge a built-in type pack's node/edge types into `tusk.toml` and hot-reload the schema |
| `tusk_help` | overview and topic deep dives (workflow, node-types, edge-types, manifest, filter, query, packs) |

Workspace bootstrap (`tusk init`), the Claude Code plugin (`tusk claude …`) and the web app (`tusk web`, graph + reading views) stay CLI-only.
