# Phase 7 — Sweep `repository/` + `sqlite/` + `cmd/` + `tests/e2e/` + root files

> Milestone: v0.14
> Phase: 7 of 8
> Spec: [`../../specs/2026-04-28-v0.14-naming-convention-design.md`](../../specs/2026-04-28-v0.14-naming-convention-design.md) §3

## Inherits From

Phase 2 has shipped. The implementer can rely on:

- All four rules are implemented as linters.
- `repository/`, `sqlite/`, `cmd/`, `tests/e2e/`, and the root
  package (`client.go`) are each currently excluded by both
  linters via separate per-package entries in `.golangci.yml` and
  `pathfilter.go`.
- `STYLE.md` is the canonical reference.

Other sweep phases (3, 4, 5, 6) may have shipped or be in progress
in parallel — irrelevant to this phase's scope.

## Prerequisites

Phase 2. Parallelizable with Phases 3, 4, 5, 6.

## Goal

Bring the remaining production and test packages into compliance
with STYLE.md's four rules. Remove the five package-level
exclusion entries from both linters. After this phase (and
Phases 3–6), every package in the repository is clean.

## Tasks

1. **Remove the five `varnamelen` exclusion rules from
   `.golangci.yml`** (paths `^repository/`, `^sqlite/`, `^cmd/`,
   `^tests/e2e/`, `^client\.go$`). Other rules stay untouched.

2. **Remove the corresponding five regex entries from
   `internal/lint/pathfilter/pathfilter.go`.**

3. **Run `make lint` to enumerate every violation across these
   packages. Apply fixes per STYLE.md.**

   - `repository/` (10 files) — interface definitions only; mostly
     parameter renames and short locals in test helpers.
   - `sqlite/` (~30 files) — repository implementations; expect
     short locals around `sql.Tx`, `sql.Rows`, and prepared
     statements. `task_test.go` (1093 LoC) is the largest test file.
   - `cmd/` — only `cmd/tusk/main.go` (360 LoC) and
     `main_test.go` (86 LoC). Plus `cmd/tusk-lint/main.go`
     introduced in Phase 1. The latter must already be clean since
     it was written under the convention; verify with the linter
     anyway.
   - `tests/e2e/` — black-box CLI tests; heavy `t *testing.T`
     usage.
   - Root files: `client.go` (8553 bytes), `client_test.go`
     (2804 bytes).

   Expected violation classes mirror prior sweeps. **Critical for
   `cmd/tusk-lint/`:** the analyzers themselves use AST walking
   patterns where `n` for `ast.Node`, `t` for `types.Type`, and
   `s` for `ast.Stmt` are common — these all rename per the style
   guide (`node`, `astType`, `stmt`).

4. **Run `make test` to verify behavior is preserved.** The e2e
   suite is the most exercising — every CLI scenario, every output
   format, every DB-config combination must continue to pass.
   `make test-race` should also pass (no new race conditions
   introduced by mechanical renames).

5. **Run `make lint` to confirm zero violations** across all five
   packages.

## User-visible behaviors (acceptance criteria)

- `make lint` passes with no exclusions for `repository/`,
  `sqlite/`, `cmd/`, `tests/e2e/`, or the root package.
- `make test` and `make test-race` pass — every repository
  implementation, every CLI scenario, every entry-point behavior
  unchanged.
- The default DB path resolution (`~/.local/share/tusk/tusk.db`),
  flag/env override (`--db` > `TUSK_DB`), and walk-up config
  discovery continue to work identically.
- Migration application from a fresh DB continues to produce a
  schema identical to pre-sweep.
- The `tusk` binary's CLI surface and the `tusk-lint` binary
  both build and run identically.

## Bridge code introduced

None.

## Bridge code removed

| Bridge from earlier phase | Removed by |
|---------------------------|------------|
| `repository/` `varnamelen` exclusion rule in `.golangci.yml` | Task 1 |
| `sqlite/` `varnamelen` exclusion rule in `.golangci.yml` | Task 1 |
| `cmd/` `varnamelen` exclusion rule in `.golangci.yml` | Task 1 |
| `tests/e2e/` `varnamelen` exclusion rule in `.golangci.yml` | Task 1 |
| `client.go` `varnamelen` exclusion rule in `.golangci.yml` | Task 1 |
| `repository/` regex entry in `pathfilter.go` | Task 2 |
| `sqlite/` regex entry in `pathfilter.go` | Task 2 |
| `cmd/` regex entry in `pathfilter.go` | Task 2 |
| `tests/e2e/` regex entry in `pathfilter.go` | Task 2 |
| Root-package regex entry in `pathfilter.go` | Task 2 |

## Changes Introduced

- **Modified:** `.golangci.yml` — five `varnamelen` exclusion
  rules deleted.
- **Modified:** `internal/lint/pathfilter/pathfilter.go` — five
  regex entries deleted (the slice may now be empty if all other
  sweep phases have also shipped).
- **Modified:** every file in `repository/`, `sqlite/`, `cmd/`,
  `tests/e2e/`, and `client.go` / `client_test.go` at the repo
  root — mechanical renames, blank-line insertions, named-error
  shadow renames, test-handle parameter renames. **No behavior
  changes.**
- **No code outside the listed packages, `.golangci.yml`, and
  `pathfilter.go` is modified in this phase.**
