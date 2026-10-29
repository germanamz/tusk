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

A doctor alias returns the same issue shape and `error_count` / `warning_count` / `advice_count` as `tusk_doctor` (`DoctorIssuesPayload` builds both), and `tusk run <doctor-alias>` exits 1 when an error is present, like `tusk doctor`.

Per-verb adapters are hand-written (no reflection). Each adapter has a `Build` closure that turns the alias's `Args map[string]any` into the typed `<Verb>Request`, and a `Run` closure that invokes the matching service entry point (`query.ListRun`, `query.Run`, `node.GetRun`, `query.EdgeListRun`, `doctor.RunWithMigration`, `status.Run`). The `edge list` adapter reads `sub-units` and passes the manifest's edge types, so `type` takes an inverse label as the CLI flag does (#850), and the `node get` adapter reads `edge-type`, `direction` and `sub-units`, like the CLI flags of the same names (#848). The `query` adapter reads `count` and `group-by` (#851): `ResultPayload` then returns the `query.CountResult` itself, `{"count": N}` plus any groups. Every node-row payload goes through the request's `fields` projection (`query.ProjectRows`/`ProjectRow`); the `node get` adapter parses the fields with `query.ParseFields` and keeps them on `NodeGetResult.Fields`, since `node.GetRun` has no package to parse them with.

TOML integers decode as `int64` when the destination is `map[string]any`; the adapter helpers (`optionalInt`, `optionalFloat`) coerce `int64` and exact-integer `float64` values to `int` so MCP callers (whose JSON numbers arrive as `float64`) and CLI callers (whose TOML numbers arrive as `int64`) both work.

Alias validation lives in `internal/manifest` (`ValidateAliases`), including the check that `args` is a table rather than a list; the dispatcher trusts the aliases it sees and surfaces only build-time / runtime errors from the service layer.

## Callers

- `cmd/tusk/cmd_run.go` — the `tusk run <alias>` CLI command.
- `internal/mcp/tools.go::registerRunTool` — the `tusk_run` MCP tool.
