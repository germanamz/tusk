---
type: package
title: internal/aliasdispatch — manifest-declared alias dispatcher
import-path: github.com/germanamz/tusk/internal/aliasdispatch
status: stable
---

# internal/aliasdispatch

Dispatches manifest-declared aliases (`[alias.<name>]` blocks in `tusk.toml`) against the canonical read-verb service layer. Aliases bind a read-only verb (`node list`, `node get`, `query`, `edge list`, `doctor`, `status`) to a fixed set of arguments so an agent or operator can invoke a frequent query by name rather than retyping the filter / sort / take flags.

## Public surface

- `Dispatcher`, `Deps`, `DispatchResult`, `VerbAdapter` — typed shapes.
- `NewDispatcher(Deps) *Dispatcher` — constructs a dispatcher seeded with the built-in adapters for the six read verbs.
- `(d *Dispatcher) Run(ctx context.Context, alias manifest.Alias) (*DispatchResult, error)` — invokes the matching adapter and returns the typed result.
- `(d *Dispatcher) ListAliases() []manifest.Alias` — enumerates the aliases in `Deps.Manifest.Aliases` in sorted order; consumed by `tusk run --list`.
- Result-kind constants: `KindNodeList`, `KindNodeGet`, `KindQuery`, `KindEdgeList`, `KindDoctor`, `KindStatus`.

## Notes

Per-verb adapters are hand-written (no reflection). Each adapter has a `Build` closure that turns the alias's `Args map[string]any` into the typed `<Verb>Request`, and a `Run` closure that invokes the matching service entry point (`query.ListRun`, `query.Run`, `node.GetRun`, `index.EdgeListRun`, `doctor.RunWithMigration`, `status.Run`).

TOML integers decode as `int64` when the destination is `map[string]any`; the adapter helpers (`optionalInt`, `optionalFloat`) coerce `int64` and exact-integer `float64` values to `int` so MCP callers (whose JSON numbers arrive as `float64`) and CLI callers (whose TOML numbers arrive as `int64`) both work.

Alias validation lives in `internal/manifest` (`ValidateAliases`); the dispatcher trusts the aliases it sees and surfaces only build-time / runtime errors from the service layer.

## Callers

- `cmd/tusk/cmd_run.go` — the `tusk run <alias>` CLI command.
- `internal/mcp/tools.go::registerRunTool` — the `tusk_run` MCP tool.
