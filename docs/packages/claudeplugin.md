---
type: package
title: internal/claudeplugin — the Claude Code plugin tusk installs
import-path: github.com/germanamz/tusk/internal/claudeplugin
status: stable
---

# internal/claudeplugin

Installs, inspects and removes the Claude Code plugin that `tusk claude install` writes, and builds the context block that plugin hands a session. The plugin gives a session started in a vault two things: the tusk MCP server, and a `tusk` block in its first message describing the vault.

## The plugin

It lives at `<workspace>/.claude/skills/tusk/`. Claude Code adopts a folder there as `tusk@skills-dir` when a session starts **in that folder**: it does not walk up from a subdirectory or look at the git root. The plugin has four files, rendered from the embedded `assets/`:

| Path | What it is |
| --- | --- |
| `.claude-plugin/plugin.json` | Manifest: name `tusk`, the tusk version, and `mcpServers.tusk` running `<bin> mcp`. |
| `hooks/hooks.json` | Names the hooks module. |
| `hooks/register.ts` | The hooks module: one `prompt.context` hook. |
| `.gitignore` | Keeps out the type declarations Claude Code writes into `.claude-plugin/types/`. |

`<bin>` (default `tusk` on PATH) and the version go in as JSON string literals, which are valid in both the manifest and the TypeScript.

The hook runs `<bin> claude context --plugin-version <version>` with a five-second timeout. Non-empty stdout becomes the `tusk` context block, replacing any earlier one. A non-zero exit, a timeout or empty output leaves the blocks as they were, and the first stderr line shows as a toast. Claude Code recomputes the blocks at session start, after `/clear` and after compaction, so the digest is read again each time. All content decisions live in Go; the hook only moves bytes, which keeps the early-access mod API confined to one small file.

Claude Code holds a project plugin's MCP server until the user approves it, so `Install` adds `plugin:tusk:tusk` to `enabledMcpjsonServers` in `.claude/settings.local.json`. The tools are listed as `mcp__plugin_tusk_tusk__tusk_*`.

## Public surface

- `Install(InstallOptions) (*Report, error)` writes the plugin, approves its MCP server, and moves a `.mcp.json` tusk server (`tusk mcp` or `tusk mcp serve`, any path) into the plugin. It preserves other servers and their order, and deletes the file if tusk was its only content. It's safe to re-run. It returns `ErrForeignPlugin` for a folder whose manifest isn't named `tusk`, unless `Force` is set.
- `Uninstall(root) (*Report, error)` removes the folder and the approval. It doesn't restore `.mcp.json`. It returns `ErrNotInstalled` or `ErrForeignPlugin`.
- `Status(root) (*StatusReport, error)` reports whether the plugin is installed or foreign, its version and binary, whether its MCP server is approved, and whether `.mcp.json` still runs a duplicate server.
- `Orientation(Snapshot) string` renders the always-present head of the block: vault name, declared node types by count, declared edge types and their edge count, aliases, and the index state (`IndexReady`, `IndexMissing`, `IndexUnreadable`).
- `Fit(orientation, []Section, budget) (string, error)` assembles the block within a byte budget (`DefaultMaxBytes` is 16384). Sections are listed highest priority first. Over budget, it cuts from the last section back: `Whole` sections are dropped outright, the others lose whole items from their end. A last line then says the digest was truncated. The orientation is never cut.
- `PluginDir(root)`, `PluginName`, `MCPServerID`, `DefaultBin`.

Edits to `.mcp.json` and `settings.local.json` go through a small order-preserving JSON object type (`jsonedit.go`), so changing one key leaves the rest of a user's file as it was.

## Callers

- `cmd/tusk/cmd_claude.go`: `tusk claude install | uninstall | status | context`. `context` opens the index without the rebuild other commands fall back to, because a session must never wait on one. It reads the node and edge types `tusk.toml` declares before the built-in sub-unit pack merges in, and composes the `[context]` digest through `internal/contextcompose`, ordered as pinned, missing pinned, recent, then each include alias.
- `cmd/tusk/cmd_init.go`: `tusk init --claude`.

## Testing

The Go tests cover rendering, install, uninstall, status, the JSON surgery, `Orientation` and `Fit`. The hooks module has its own tests in `testdata/register.test.ts`, run under Claude Code's test runner by `make claude-plugin-check`. That target is local-only because CI has no `claude` CLI. It installs the plugin into a scratch vault, copies the tests beside it, and runs `claude plugin validate` and `claude plugin test`.
