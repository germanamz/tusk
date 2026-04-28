# Phase 4 — Sweep `internal/tui/`

> Milestone: v0.14
> Phase: 4 of 8
> Spec: [`../../specs/2026-04-28-v0.14-naming-convention-design.md`](../../specs/2026-04-28-v0.14-naming-convention-design.md) §3

## Inherits From

Phase 2 has shipped. The implementer can rely on:

- All four rules are implemented as linters: `varnamelen` in
  `.golangci.yml`; `blankline`, `namederr`, `testhandle` in
  `cmd/tusk-lint`.
- The `internal/tui/` package is currently excluded by both linters
  via per-package entries in `.golangci.yml` and `pathfilter.go`.
- `STYLE.md` is the canonical reference for all four rules and the
  style guide.

Other sweep phases (3, 5, 6, 7) may have shipped or be in progress
in parallel — irrelevant to this phase's scope.

## Prerequisites

Phase 2. Parallelizable with Phases 3, 5, 6, 7.

## Goal

Bring every file in `internal/tui/` (production and tests) into
compliance with STYLE.md's four rules. Remove the `internal/tui/`
exclusion entries from both linters.

## Tasks

1. **Remove the `internal/tui/` `varnamelen` exclusion rule from
   `.golangci.yml`.** Locate the rule under
   `linters.exclusions.rules` whose `path` is `^internal/tui/` and
   delete it. Other per-package rules stay untouched.

2. **Remove the `internal/tui/` regex entry from
   `internal/lint/pathfilter/pathfilter.go`.** Delete the matching
   line from the `excluded` slice. Other entries stay.

3. **Run `make lint` to enumerate every violation in
   `internal/tui/`. Apply fixes per STYLE.md.** The package contains
   ~50 `.go` files including `commands.go` (1450 LoC), `render.go`
   (1231 LoC), `tree_markdown_test.go` (959 LoC), `config.go`
   (823 LoC), `commands_test.go` (1491 LoC). Expected violation
   classes:

   - **Rule 1 (varnamelen):** receivers across cobra command
     builders use `a *App` — rename to `app *App`. Range vars on
     tasks/projects/notes use single chars — rename to role words.
     Help-text builders may use short locals — rename per style
     guide.
   - **Rule 2 (blankline):** `runCreate`, `runList`, `runGet`,
     `runModify`, `runStart`, etc. each contain multiple
     error-producing service calls; insert blank lines around the
     guards.
   - **Rule 3 (namederr):** look for sequential `err :=` shadows in
     the `runX` handlers and the rendering helpers. Rename to typed
     names where the rule fires.
   - **Rule 4 (testhandle):** rename `t *testing.T` to
     `test *testing.T` across all `*_test.go` in `internal/tui/`.
     Verify no `*testing.B` or `testing.TB` exists.

   Mechanical sweep; no behavior changes. Split per-file if the
   diff is too large for one PR — `commands.go` and `render.go` are
   the largest candidates for splitting.

4. **Run `make test` to verify behavior is preserved.** All
   `internal/tui/` tests, including the e2e tests that exercise CLI
   command output, must pass.

5. **Run `make lint` to confirm zero violations.**

## User-visible behaviors (acceptance criteria)

- `make lint` passes with no exclusions for `internal/tui/`.
- `make test` passes — every CLI command behavior unchanged.
- `tusk task create`, `tusk task list`, `tusk task get`, `tusk task
  modify`, `tusk task tree`, `tusk task next`, etc. all produce
  byte-identical output to pre-sweep behavior (verified by the
  existing e2e snapshot tests).
- Cobra help text for every command remains unchanged.

## Bridge code introduced

None.

## Bridge code removed

| Bridge from earlier phase | Removed by |
|---------------------------|------------|
| `internal/tui/` `varnamelen` exclusion rule in `.golangci.yml` | Task 1 |
| `internal/tui/` regex entry in `pathfilter.go` | Task 2 |

## Changes Introduced

- **Modified:** `.golangci.yml` — `internal/tui/` `varnamelen`
  exclusion rule deleted.
- **Modified:** `internal/lint/pathfilter/pathfilter.go` —
  `internal/tui/` regex entry deleted.
- **Modified:** every file in `internal/tui/` — mechanical
  renames, blank-line insertions, named-error shadow renames,
  test-handle parameter renames. **No behavior changes.**
- **No code outside `internal/tui/`, `.golangci.yml`, and
  `pathfilter.go` is modified in this phase.**
