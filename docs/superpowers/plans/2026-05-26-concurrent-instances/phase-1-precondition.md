# Phase 1 — Precondition: drop `body` from `node_modify`

**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** none (base codebase).
**Parallelization:** N/A — single task.

## Execution Rules

These apply to every task in this phase. Implementer agents must honor them.

1. **One task = one PR.** This phase has one task; it produces one PR.
2. **The task must be independently shippable.** Build green, full Go test
   suite green (lefthook pre-commit enforces this), lint clean.
3. **No bridge code is required.** The change is a removal that the rest of
   the work stream depends on; nothing here is provisional.

## Goal

Narrow `tusk_node_modify` and `node.Service.Modify` to frontmatter mutations
only. This is the invariant the concurrency design relies on (spec
§ *Precondition: Drop `body` from `node_modify`*): body changes belong to
out-of-band FS writes caught by the watcher, not to a tusk-mediated tool
call.

Today the surface drifts in three places:
- The MCP tool `tusk_node_modify` registers an optional `body` parameter
  (`internal/mcp/tools.go:1032`) and the handler assigns it onto
  `node.ModifyInput` (`internal/mcp/tools.go:1051-1054`).
- `node.ModifyInput` carries a `Body *[]byte` field
  (`internal/node/service.go:37`).
- `Service.Modify` swaps the body when the field is non-nil
  (`internal/node/service.go:387-392`).
- The CLI command `tusk node modify` already exposes only `--prop` and
  `--unset` (`cmd/tusk/cmd_node_modify.go`); no CLI change is required.

## Tasks

| #     | Title                                       | Prereqs |
|-------|---------------------------------------------|---------|
| 1.1   | Drop `body` from `node_modify` (MCP + service) | none |

## Changes Introduced

- **Modified interfaces:**
  - `node.ModifyInput` loses the `Body *[]byte` field.
  - MCP tool `tusk_node_modify` loses its `body` input parameter.
  - `Service.Modify` no longer accepts body replacement; the on-disk body
    passes through unchanged.
- **No new files. No schema changes. No new dependencies. No new env vars.**
- **No bridge code introduced.**

## Acceptance Criteria

After this phase ships, the following user-visible behaviors must still
hold:
- `tusk_node_modify` continues to set, change, and unset frontmatter
  properties.
- `tusk node modify` CLI continues to work with its existing flag set
  unchanged.
- The full Go test suite passes.
- `make lint` and `make vet` pass.
- Calling `tusk_node_modify` with a `body` argument is now an MCP
  schema-level rejection (unknown parameter) — verifiable by manual test.
