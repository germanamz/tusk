# Phase 8 — Lock-in

> Milestone: v0.14
> Phase: 8 of 8
> Spec: [`../../specs/2026-04-28-v0.14-naming-convention-design.md`](../../specs/2026-04-28-v0.14-naming-convention-design.md) §3 (Phase C — lock-in)

## Inherits From

Phases 1–7 have all shipped. The implementer can rely on:

- Every package in the repository is in compliance with all four
  STYLE.md rules.
- All twelve per-package `varnamelen` exclusion rules in
  `.golangci.yml` have been deleted by their corresponding sweep
  phases. (Verify — if any remain, that means a sweep phase did not
  ship correctly; halt and reconcile before proceeding.)
- All twelve per-package regex entries in
  `internal/lint/pathfilter/pathfilter.go` have been deleted. The
  `excluded` slice should be empty.
- `STYLE.md` is published; `make lint` runs both linters and
  reports zero violations against the entire codebase with no
  exclusions in place.

## Prerequisites

Phases 1, 2, 3, 4, 5, 6, 7 — all must be complete.

## Goal

Remove residual exclusion infrastructure and add structural
regression guards so the convention cannot quietly degrade in
future PRs.

## Tasks

1. **Verify all per-package `varnamelen` exclusions are gone, then
   confirm the structural state.** Run:

   ```bash
   grep -A 3 'linters: \[varnamelen\]' .golangci.yml
   ```

   This must produce **no output**. If any rule remains, halt this
   phase and identify which sweep phase failed to remove its rule
   — the missing sweep must complete first.

2. **Verify the `pathfilter` exclusion list is empty.** Open
   `internal/lint/pathfilter/pathfilter.go` and confirm the
   `excluded` slice has zero entries. If entries remain, halt and
   identify the missing sweep. With an empty slice the helper is a
   no-op (`Excluded` always returns false); leave the helper in
   place — it remains available for future per-package rollouts
   without re-introducing the bridge code.

3. **Add the regression assertion to CI.** Create a Makefile target
   `lint-style-locked` that fails if either of the following holds:

   ```makefile
   lint-style-locked:
   	@if grep -rq -- '// nolint:varnamelen' --include='*.go' . ; then \
   		echo "Found // nolint:varnamelen directive — STYLE.md rule 1 must not be suppressed."; \
   		exit 1; \
   	fi
   	@if grep -A 1 'exclusions:' .golangci.yml | grep -q 'linters: \[varnamelen\]' ; then \
   		echo "Found varnamelen exclusion rule in .golangci.yml — must remain absent after v0.14 lock-in."; \
   		exit 1; \
   	fi
   ```

   Make `lint` depend on `lint-style-locked` (in addition to
   `lint-go` and `lint-tusk`). Verify the target passes against the
   current state and fails when a directive is intentionally added
   then removed.

4. **Update STYLE.md to mark the convention as fully enforced.**
   Add a one-line status note at the top: "Status: enforced
   repository-wide as of v0.14." If STYLE.md has an enforcement
   summary section, append a "no exclusions in `.golangci.yml`,
   no `// nolint:varnamelen` directives anywhere; both guarded by
   `make lint-style-locked` in CI."

5. **Verify CI green end-to-end.** Run `make build`, `make test`,
   `make test-race`, `make lint`. All must pass. The
   `lint-style-locked` target runs as part of `make lint` and must
   pass.

## User-visible behaviors (acceptance criteria)

- `.golangci.yml` contains zero `linters: [varnamelen]` exclusion
  rules. The `errcheck` exclusion rules from before v0.14 remain
  unchanged.
- `internal/lint/pathfilter/pathfilter.go`'s `excluded` slice is
  empty.
- `make lint` passes against the full codebase with no exclusions.
- `make lint-style-locked` is wired into CI through `make lint`
  and fails if either:
  - A `// nolint:varnamelen` directive is added anywhere in the
    Go source tree.
  - A `linters: [varnamelen]` exclusion rule is added back to
    `.golangci.yml`.
- STYLE.md indicates "enforced repository-wide as of v0.14."
- `make build`, `make test`, `make test-race` all pass — no
  behavior changes from the v0.14 milestone.

## Bridge code introduced

None.

## Bridge code removed

| Bridge from earlier phase | Removed by |
|---------------------------|------------|
| Any residual per-package `varnamelen` exclusion rules in `.golangci.yml` | Task 1 (verification) — should already be empty after Phases 3–7 |
| Any residual per-package regex entries in `pathfilter.go` `excluded` slice | Task 2 (verification) — should already be empty after Phases 3–7 |

The path-filter helper itself (`pathfilter.go`) stays as no-op
infrastructure; future per-package rollouts (e.g., a new linter or
a new strict rule) can re-populate it without re-introducing
bridge code.

## Changes Introduced

- **Modified:** `Makefile` — adds `lint-style-locked` target;
  `lint` now depends on `lint-go`, `lint-tusk`, and
  `lint-style-locked`.
- **Modified:** `STYLE.md` — status note added.
- **Possibly modified:** `.golangci.yml` and
  `internal/lint/pathfilter/pathfilter.go` — only if Phases 3–7
  left residual entries. The expected state is no further edits
  to either file in this phase.
- **No code in `service/`, `internal/`, `domain/`, `filter/`,
  `repository/`, `sqlite/`, `cmd/`, `tests/`, or root files is
  modified in this phase.**
