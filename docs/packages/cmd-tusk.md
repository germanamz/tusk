---
type: package
title: cmd/tusk — CLI entry point
import-path: github.com/germanamz/tusk/cmd/tusk
status: stable
last-touched-by: Plan 7.c.4
---

# cmd/tusk

CLI entry point. Wires every subcommand (`init`, `node`, `edge`, `pack`, `reindex`, `query`, `doctor`, `status`, `mcp`, `watch`) onto a Cobra root command. Each subcommand opens the workspace, calls into the appropriate `internal/...` service, and renders results either as tabular text or JSON depending on flags.

## Public surface

- `main()` — root entry; panics on bootstrap error.
- One file per subcommand (`cmd_init.go`, `cmd_node_create.go`, etc.) following a `newXCmd() *cobra.Command` factory pattern.
- `behavior_registry.go` — central registration of in-tree behavior pack kinds (currently only `workflow`).

## Notes

Smoke tests in `*_test.go` exercise each subcommand end-to-end against a temp workspace. Helper `chdir(test, dir)` in `cmd_test_helpers_test.go` handles cwd setup and registers `t.Cleanup` internally — never call it as `previous := chdir(...); defer previous()`.
