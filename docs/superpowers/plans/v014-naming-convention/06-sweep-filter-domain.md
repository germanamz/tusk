# Phase 6 — Sweep `filter/` + `domain/` + `syntax/`

> Milestone: v0.14
> Phase: 6 of 8
> Spec: [`../../specs/2026-04-28-v0.14-naming-convention-design.md`](../../specs/2026-04-28-v0.14-naming-convention-design.md) §3

## Inherits From

Phase 2 has shipped. The implementer can rely on:

- All four rules are implemented as linters.
- The `filter/`, `domain/`, and `syntax/` packages are each
  currently excluded by both linters via separate per-package
  entries in `.golangci.yml` and `pathfilter.go`.
- `STYLE.md` is the canonical reference.

Other sweep phases (3, 4, 5, 7) may have shipped or be in progress
in parallel — irrelevant to this phase's scope.

## Prerequisites

Phase 2. Parallelizable with Phases 3, 4, 5, 7.

## Goal

Bring every file in `filter/`, `domain/`, and `syntax/` (production
and tests) into compliance with STYLE.md's four rules. Remove the
six exclusion entries (three packages × two linter configs) from
both linters.

## Tasks

1. **Remove the `filter/`, `domain/`, and `syntax/` `varnamelen`
   exclusion rules from `.golangci.yml`.** Locate the rules whose
   `path` is `^filter/`, `^domain/`, and `^syntax/` respectively and
   delete all three.

2. **Remove the `filter/`, `domain/`, and `syntax/` regex entries
   from `internal/lint/pathfilter/pathfilter.go`.** Delete the three
   matching lines from the `excluded` slice.

3. **Run `make lint` to enumerate every violation in `filter/`,
   `domain/`, and `syntax/`. Apply fixes per STYLE.md.** The
   packages contain smaller files than prior sweeps but more of
   them — `filter/` has 22 files (parser, lexer, validators,
   resolvers); `domain/` has 36 files (entity definitions,
   validators, urgency overrides helpers, taxonomy validators);
   `syntax/` has 10 files (token, AST, modifier, parse_fields).
   Expected violation classes:

   - **Rule 1 (varnamelen):** parser state machines and AST nodes
     in `filter/` use short locals (`t` for token, `p` for
     position, etc.) — rename per the style guide. `domain/`
     validators and helpers use short locals around field-by-field
     iteration. `syntax/` has range vars across AST and tag
     iteration (`syntax/ast.go:37, 48` use `for _, t := range
     fs.Tags`; `syntax/errors.go:32` uses `for i, e := range
     errs`; `syntax/modifier_test.go:17` uses `for _, b := range
     []byte{...}`) and many `t *testing.T` parameters.
   - **Rule 2 (blankline):** parser error paths typically do
     `tok, err := next(...)` then `if err != nil` — these are
     missing the blank-line separator throughout.
   - **Rule 3 (namederr):** less common in these packages (most
     functions return early on a single err). Verify by running
     the analyzer.
   - **Rule 4 (testhandle):** rename `t *testing.T` to
     `test *testing.T` across all `*_test.go` files in all three
     packages.

   Mechanical sweep; no behavior changes. The per-file diffs are
   smaller than service/ or internal/tui/ sweeps; one PR for all
   three packages should still be reviewable.

4. **Run `make test` to verify behavior is preserved.** Critical:
   the filter parser tests cover edge cases (boolean operators,
   tag include/exclude, UDA fields, urgency keys) — every test
   must pass. Domain tests cover taxonomy validation, urgency
   overrides math, workflow validation — all must pass. The
   `syntax/` token/AST/modifier tests cover lex behavior and
   filter-set semantics — all must pass.

5. **Run `make lint` to confirm zero violations** in `filter/`,
   `domain/`, and `syntax/`.

## User-visible behaviors (acceptance criteria)

- `make lint` passes with no exclusions for `filter/`, `domain/`,
  or `syntax/`.
- `make test` passes — every filter parser, urgency calculation,
  taxonomy validation, workflow transition, and `syntax/` lex/AST
  behavior unchanged.
- Filter expressions (`status=pending,active`, `priority=2..4`,
  `due=today`, `+tag`, `-tag`, `parent=<short_id>`,
  `tree=<short_id>`, `uda.<key>=<value>`) continue to parse and
  evaluate identically.
- Urgency scoring continues to produce identical scores for the
  same inputs.
- Taxonomy level validation continues to enforce/reject the same
  inputs.
- `syntax/` modifier registration and tag-prefix recognition
  continue to behave identically.

## Bridge code introduced

None.

## Bridge code removed

| Bridge from earlier phase | Removed by |
|---------------------------|------------|
| `filter/` `varnamelen` exclusion rule in `.golangci.yml` | Task 1 |
| `domain/` `varnamelen` exclusion rule in `.golangci.yml` | Task 1 |
| `syntax/` `varnamelen` exclusion rule in `.golangci.yml` | Task 1 |
| `filter/` regex entry in `pathfilter.go` | Task 2 |
| `domain/` regex entry in `pathfilter.go` | Task 2 |
| `syntax/` regex entry in `pathfilter.go` | Task 2 |

## Changes Introduced

- **Modified:** `.golangci.yml` — three `varnamelen` exclusion
  rules deleted (`filter/`, `domain/`, `syntax/`).
- **Modified:** `internal/lint/pathfilter/pathfilter.go` — three
  regex entries deleted.
- **Modified:** every file in `filter/`, `domain/`, and `syntax/`
  — mechanical renames, blank-line insertions, named-error shadow
  renames, test-handle parameter renames. **No behavior changes.**
- **No code outside `filter/`, `domain/`, `syntax/`,
  `.golangci.yml`, and `pathfilter.go` is modified in this phase.**
