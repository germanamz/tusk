---
type: package
title: internal/contextcompose — warm-context digest composer
import-path: github.com/germanamz/tusk/internal/contextcompose
status: stable
---

# internal/contextcompose

Composes the warm-context digest exposed through `tusk context` and `tusk_context`. Replaces the cold-start "3–5 exploratory calls per session" pattern with a single composed payload built from the `[context]` block in `tusk.toml`.

## Public surface

- `Compose(ctx, Deps, Request) (*Result, error)` — runs the digest.
- `Result` — typed payload: `Pinned` (`[]query.ListRow`), `Recent` (`[]query.ListRow`), `Aliases` (`map[string]*aliasdispatch.DispatchResult`), `MissingPinned` (`[]string`).
- `Deps` — bundles `Manifest`, `Dispatcher` (alias dispatcher), `NodeService`, `WorkspaceRoot`, `Database`, `Edges`.
- `Request` — `Include []string` overrides the per-node expansion set (default `[body, edges]`).
- `SortedIncludeNames(*Result) []string` — deterministic order for the alias fan-out section.

## Sections

The digest is built in three steps:

1. **Pinned.** `manifest.Context.Pinned` is loaded via a single `query.ListRun` with an `id='foo' OR id='bar'` filter so the body/edges expansion pipeline runs once per call. IDs that don't resolve are surfaced via `Result.MissingPinned` and reported through doctor at run time.
2. **Recent.** `manifest.Context.Recent` (a single alias) is dispatched through `aliasdispatch.Dispatcher.Run`. Node-list and structural-query kinds are reshaped into `[]query.ListRow`; other kinds fall back to an empty Recent slice.
3. **Include.** `manifest.Context.Include` (named aliases) are dispatched in parallel via `golang.org/x/sync/errgroup.WithContext`. Concurrency is bound by the slice length; the workspace lock plus SQLite WAL mode handle contention.

## Notes

- `Compose` returns an empty `Result` (not nil) when the manifest declares no `[context]` block, so renderers can treat "no digest configured" uniformly.
- The per-node include override (`Request.Include`) applies only to the pinned and recent sections. Include-aliases use whatever expansion their own `[alias.<name>].args.include` declares.
- Pinned-row ordering is the manifest declaration order, not the SQL engine's order.
- `Result.Pinned` and `Result.Recent` use the JSON tag `omitempty` so the wire envelope omits empty sections instead of emitting null.

## Callers

- `cmd/tusk/cmd_context.go` — the `tusk context` CLI command.
- `internal/mcp/tools.go::registerContextTool` — the `tusk_context` MCP tool.
