# Phase 3 — Sweep `service/`

> Milestone: v0.14
> Phase: 3 of 8
> Spec: [`../../specs/2026-04-28-v0.14-naming-convention-design.md`](../../specs/2026-04-28-v0.14-naming-convention-design.md) §3

## Inherits From

Phase 2 has shipped. The implementer can rely on:

- All four rules are implemented as linters: `varnamelen` (rule 1)
  in `.golangci.yml`; `blankline`, `namederr`, `testhandle`
  analyzers in `cmd/tusk-lint`.
- The `service/` package is currently excluded by both linters via
  per-package entries in `.golangci.yml` and `pathfilter.go`.
- `STYLE.md` is the canonical reference for all four rules and the
  style guide.

Other sweep phases (4, 5, 6, 7) may have shipped or be in progress
in parallel — that is irrelevant to this phase's scope. Each sweep
phase removes its own package's exclusion entries; there is no
shared state between sweep phases.

## Prerequisites

Phase 2. Parallelizable with Phases 4, 5, 6, 7.

## Goal

Bring every file in `service/` (production and tests) into
compliance with STYLE.md's four rules. Remove the `service/`
exclusion entries from both linters. After this phase, `make lint`
runs all four rules against `service/` with no exclusions and
reports zero violations.

## Tasks

1. **Remove the `service/` `varnamelen` exclusion rule from
   `.golangci.yml`.** Locate the rule under
   `linters.exclusions.rules` whose `path` is `^service/` and delete
   the rule. The other per-package rules stay untouched.

2. **Remove the `service/` regex entry from
   `internal/lint/pathfilter/pathfilter.go`.** Delete the line
   `regexp.MustCompile(`^github\.com/germanamz/tusk/service(/|$)`),`
   from the `excluded` slice. Other entries stay.

3. **Run `make lint` to enumerate every violation in `service/`.
   Apply fixes per STYLE.md.** The `service/` package contains ~44
   `.go` files including `task.go` (2174 LoC), `task_test.go`
   (1754 LoC), and many smaller files. Expected violation classes:

   - **Rule 1 (varnamelen):** single-character locals (`t`, `p`,
     `c`, `n`, `b`, `d`), single-character receivers
     (`s *TaskService`), single-character range vars. Rename per
     the style guide:
     - Domain entities → role words: `task`, `project`, `player`,
       `parent`, `child`, `bundle`, `descendant`, `block`.
     - Receivers → role word matching the type: `*TaskService` →
       `service`; `*UrgencyEngine` → `engine`. When two service
       types appear in the same scope, qualify
       (`taskService` / `projectService`).
     - Loop indices → `index`, `subindex`, or contextual.
     - Generic type params → role-named (`Element`, `Item`, `Key`,
       `Value`).
   - **Rule 2 (blankline):** insert blank lines between
     error-producing assignments and their `if err != nil` guards,
     and between guard closing braces and the next statement.
   - **Rule 3 (namederr):** rename shadowed `err` to typed names.
     Canonical site to verify the implementation:
     `service/task.go:325–339` in `listInBundle` — three sequential
     `err :=` shadows that should become `blockingErr`,
     `blockedByErr`, `annotationErr`, `tagErr`.
   - **Rule 4 (testhandle):** rename `t *testing.T` to
     `test *testing.T` across every `*_test.go` file in `service/`.
     Grep first to confirm whether any `*testing.B` or `testing.TB`
     parameters exist in this package — if so, rename to `bench`
     and `harness` respectively.

   This is a mechanical sweep; **no behavior changes**. If the diff
   is too large for one PR, split per-file (`task.go` separately
   from `task_test.go`, etc.). The phase still ships when every
   file in `service/` is clean.

4. **Run `make test` to verify behavior is preserved.** All existing
   service-layer tests must pass. Failures indicate an accidental
   semantic change during the sweep — investigate and fix.

5. **Run `make lint` to confirm zero violations.** With the
   `service/` exclusion entries removed, all four rules now apply
   to `service/`. No violations should remain.

## User-visible behaviors (acceptance criteria)

- `make lint` passes with no exclusions for `service/`.
- `make test` passes — every service-layer behavior unchanged.
- All MCP tools backed by `TaskService`, `ProjectService`,
  `WorkflowService`, `RelationService`, `TagService`, `NoteService`,
  `PlayerService`, `UrgencyEngine` continue to work identically (no
  API or behavior changes).
- All CLI commands continue to work identically.

## Bridge code introduced

None.

## Bridge code removed

| Bridge from earlier phase | Removed by |
|---------------------------|------------|
| `service/` `varnamelen` exclusion rule in `.golangci.yml` | Task 1 |
| `service/` regex entry in `pathfilter.go` | Task 2 |

## Changes Introduced

- **Modified:** `.golangci.yml` — `service/` `varnamelen` exclusion
  rule deleted (one rule of ten).
- **Modified:** `internal/lint/pathfilter/pathfilter.go` —
  `service/` regex entry deleted (one entry of ten).
- **Modified:** every file in `service/` — mechanical identifier
  renames, blank-line insertions, named-error shadow renames,
  test-handle parameter renames. **No behavior changes.**
- **No code outside `service/`, `.golangci.yml`, and `pathfilter.go`
  is modified in this phase.**
