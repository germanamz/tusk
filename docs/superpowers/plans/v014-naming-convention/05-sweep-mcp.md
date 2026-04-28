# Phase 5 — Sweep `internal/mcp/` + `internal/portability/`

> Milestone: v0.14
> Phase: 5 of 8
> Spec: [`../../specs/2026-04-28-v0.14-naming-convention-design.md`](../../specs/2026-04-28-v0.14-naming-convention-design.md) §3

## Inherits From

Phase 2 has shipped. The implementer can rely on:

- All four rules are implemented as linters: `varnamelen` in
  `.golangci.yml`; `blankline`, `namederr`, `testhandle` in
  `cmd/tusk-lint`.
- Both `internal/mcp/` and `internal/portability/` are currently
  excluded by both linters via per-package entries in `.golangci.yml`
  and `pathfilter.go`.
- `STYLE.md` is the canonical reference.

Other sweep phases (3, 4, 6, 7) may have shipped or be in progress
in parallel — irrelevant to this phase's scope.

## Prerequisites

Phase 2. Parallelizable with Phases 3, 4, 6, 7.

## Goal

Bring every file in `internal/mcp/` and `internal/portability/`
(production and tests) into compliance with STYLE.md's four rules.
Remove all four exclusion entries (two packages × two linter configs)
from both linters.

## Tasks

1. **Remove the `internal/mcp/` and `internal/portability/`
   `varnamelen` exclusion rules from `.golangci.yml`.** Locate the
   rules whose `path` is `^internal/mcp/` and `^internal/portability/`
   respectively and delete both.

2. **Remove the `internal/mcp/` and `internal/portability/` regex
   entries from `internal/lint/pathfilter/pathfilter.go`.** Delete the
   two matching lines from the `excluded` slice.

3. **Run `make lint` to enumerate every violation in both packages.
   Apply fixes per STYLE.md.** `internal/mcp/` contains ~22 `.go`
   files including `tools.go` (1729 LoC), `server.go` (1043 LoC),
   `project_handlers_test.go` (619 LoC), `handlers_test.go`
   (578 LoC). `internal/portability/` contains 5 `.go` files
   (`encode.go`, `decode.go`, `portable.go`, plus tests). Expected
   violation classes:

   - **Rule 1 (varnamelen):** in `internal/mcp/`, `taskResponse` /
     `urgencyWeightsJSON` / `projectNameCache` and similar DTO types
     use short locals during marshaling — rename per the style
     guide. Per-tool handlers (`handleTaskCreate`, `handleTaskList`,
     etc.) use short locals — rename. Range vars `for _, b := range
     blocks` (`tools.go:666, 734, 750`) → `for _, block := range
     blocks`. In `internal/portability/`, the `*ImportError`
     receiver (`decode.go:29`) and the `dec`/`enc` locals around
     `json.NewDecoder` / `json.NewEncoder` (`decode.go:48`,
     `encode.go:18`) need renaming.
   - **Rule 2 (blankline):** every per-tool MCP handler does
     multiple service calls; insert blank lines around guards.
     `internal/portability/` has fewer guard sites but the same
     pattern.
   - **Rule 3 (namederr):** look for sequential `err :=` shadows in
     handler bodies and tool registration helpers.
   - **Rule 4 (testhandle):** rename `t *testing.T` to
     `test *testing.T` across all `*_test.go` files in both
     packages. Verify no `*testing.B` or `testing.TB` exists.

   Mechanical sweep; no behavior changes. Per-handler PRs are a
   reasonable split if `tools.go` produces an oversized diff.

4. **Run `make test` to verify behavior is preserved.** All MCP
   server tests, tool-handler tests, tool-registry tests, and
   `internal/portability/` encode/decode round-trip tests must pass.

5. **Run `make lint` to confirm zero violations** across both
   packages.

## User-visible behaviors (acceptance criteria)

- `make lint` passes with no exclusions for `internal/mcp/` or
  `internal/portability/`.
- `make test` passes — every MCP tool behavior unchanged.
- Every MCP tool (`tusk_task_create`, `tusk_task_list`,
  `tusk_task_modify`, `tusk_project_list`, `tusk_workflow_list`,
  `tusk_note_*`, etc.) produces byte-identical responses to
  pre-sweep behavior.
- The `[mcp.blocked_fields]` enforcement layer (v0.12) continues to
  block configured tool/field combinations.
- The MCP server's stdio and SSE transports continue to function.
- Workspace export/import via `internal/portability/` produces
  byte-identical JSON output for the same input and round-trips
  identically.

## Bridge code introduced

None.

## Bridge code removed

| Bridge from earlier phase | Removed by |
|---------------------------|------------|
| `internal/mcp/` `varnamelen` exclusion rule in `.golangci.yml` | Task 1 |
| `internal/portability/` `varnamelen` exclusion rule in `.golangci.yml` | Task 1 |
| `internal/mcp/` regex entry in `pathfilter.go` | Task 2 |
| `internal/portability/` regex entry in `pathfilter.go` | Task 2 |

## Changes Introduced

- **Modified:** `.golangci.yml` — two `varnamelen` exclusion rules
  deleted (`internal/mcp/` and `internal/portability/`).
- **Modified:** `internal/lint/pathfilter/pathfilter.go` — two
  regex entries deleted.
- **Modified:** every file in `internal/mcp/` and
  `internal/portability/` — mechanical renames, blank-line
  insertions, named-error shadow renames, test-handle parameter
  renames. **No behavior changes.**
- **No code outside `internal/mcp/`, `internal/portability/`,
  `.golangci.yml`, and `pathfilter.go` is modified in this phase.**
