# Phase 1 — Convention doc + linter scaffold + rule 1

> Milestone: v0.14
> Phase: 1 of 8
> Spec: [`../../specs/2026-04-28-v0.14-naming-convention-design.md`](../../specs/2026-04-28-v0.14-naming-convention-design.md) §1, §2

## Prerequisites

Base codebase only. No prior phases required.

## Goal

Land the written convention (`STYLE.md`), the linter binary skeleton
(`cmd/tusk-lint`), the Makefile integration, and `varnamelen`
configured in `.golangci.yml` with a path exclusion that covers every
existing directory. After this phase, `make lint` runs both
`golangci-lint` and `tusk-lint` in CI and finds zero new violations
(everything is excluded). The codebase compiles and tests pass.

## Tasks

1. **Write `STYLE.md` at the repo root.** Structure it in three
   sections matching the spec §1:
   - **Rules** (numbered 1–4): No single-character identifiers; blank
     lines around `if err != nil` guards; named errors on `err`
     shadow; standardized test-handle parameter names
     (`*testing.T` → `test`, `*testing.B` → `bench`, `testing.TB` →
     `harness`). Include a before/after Go example for each rule and
     a one-line rationale. Rule 3 must state explicitly: when shadow
     occurs, **every** instance gets a typed name including the first
     — keeps the block visually uniform.
   - **Style guide** (advisory, not linter-enforced): receivers,
     generic type parameters, loop indices. Match the spec's wording.
   - **Enforcement summary**: which linter checks which rule
     (`varnamelen` → rule 1; `tusk-lint -blankline` → rule 2;
     `tusk-lint -namederr` → rule 3; `tusk-lint -testhandle` → rule 4).

2. **Cross-reference STYLE.md from `CONTRIBUTING.md`.** Add one line
   in the appropriate section (likely near the existing setup or
   commit-conventions content): "For code style, see
   [STYLE.md](STYLE.md)." Do not move existing CONTRIBUTING.md content.

3. **Create `cmd/tusk-lint/main.go`** with the `multichecker` shell
   and an empty analyzer registry. The file must compile and run
   without panicking. Use
   `golang.org/x/tools/go/analysis/multichecker`. The body is:

   ```go
   package main

   import "golang.org/x/tools/go/analysis/multichecker"

   func main() {
       multichecker.Main()
   }
   ```

   Add `golang.org/x/tools` to `go.mod` if not already present. This
   is **bridge code** — the empty registry will be populated in Phase
   2 task 4. Tag it in a code comment: `// Analyzers registered in
   Phase 2 of the v0.14 milestone.`

4. **Add the `lint-tusk` target to the `Makefile`** and make `lint`
   depend on both `lint-go` and `lint-tusk`. The existing `lint`
   target (which runs `golangci-lint run ./...`) becomes `lint-go`.

   ```makefile
   lint: lint-go lint-tusk

   lint-go:
   	golangci-lint run ./...

   lint-tusk:
   	go run ./cmd/tusk-lint ./...
   ```

   Verify `make lint` runs without errors against the unmodified
   codebase. The `tusk-lint` invocation will report no violations
   because no analyzers are registered yet.

5. **Configure `varnamelen` in `.golangci.yml`** with strict settings
   and **one path-exclusion rule per package**. The schema is
   golangci-lint v2 (the existing config uses `version: "2"`);
   exclusions live under `linters.exclusions.rules` and per-linter
   settings live under `linters.settings.<linter>`. Per-package rules
   are intentional: parallel sweep phases (3–7) each remove a
   different rule, so branches do not merge-conflict on a single
   shared alternation. Final shape:

   ```yaml
   version: "2"

   linters:
     enable:
       - varnamelen
     settings:
       varnamelen:
         min-name-length: 2
         check-receiver: true
         check-return: true
         check-type-param: true
         ignore-names: []
         ignore-decls: []
     exclusions:
       rules:
         # existing errcheck rules — preserve verbatim
         - linters: [errcheck]
           path: _test\.go
         - linters: [errcheck]
           text: 'Error return value of `.*\.Close` is not checked'
         # new — one varnamelen exclusion per package (each removed
         # by the corresponding sweep phase; remaining rule deleted in
         # Phase 8)
         - linters: [varnamelen]
           path: ^service/
         - linters: [varnamelen]
           path: ^internal/tui/
         - linters: [varnamelen]
           path: ^internal/mcp/
         - linters: [varnamelen]
           path: ^internal/portability/
         - linters: [varnamelen]
           path: ^filter/
         - linters: [varnamelen]
           path: ^domain/
         - linters: [varnamelen]
           path: ^syntax/
         - linters: [varnamelen]
           path: ^repository/
         - linters: [varnamelen]
           path: ^sqlite/
         - linters: [varnamelen]
           path: ^cmd/
         - linters: [varnamelen]
           path: ^tests/e2e/
         - linters: [varnamelen]
           path: ^client\.go$
   ```

   Path segments use anchored slashes (`^service/` not `^service`) so
   the regex cannot match prefix-collision directories. The
   `^client\.go$` rule is a file pattern (root-package code lives in
   a single file); its symmetric counterpart in Phase 2's pathfilter
   matches the module-root import path. Each rule is **bridge code**
   — sweep phases remove their corresponding rule; any remaining
   rules are removed in Phase 8.

6. **Verify CI green end-to-end.** Run the full local validation
   suite: `make build`, `make test`, `make lint`. All three must
   succeed. The pre-commit hooks (lefthook) must also pass when
   committing the phase changes.

## User-visible behaviors (acceptance criteria)

- `STYLE.md` exists at the repo root and documents all four rules
  plus the style guide.
- `CONTRIBUTING.md` links to `STYLE.md` from at least one location.
- `go build ./cmd/tusk-lint` produces a working binary that exits
  zero with no input.
- `make lint` runs both `lint-go` and `lint-tusk` and exits zero
  against the unmodified codebase.
- `make test` and `make build` continue to pass (no behavior changes
  to the codebase under audit).
- `.golangci.yml` enables `varnamelen` with `min-name-length: 2` and
  the documented path exclusion.

## Bridge code introduced

| Bridge | Location | Removal target |
|--------|----------|----------------|
| Empty analyzer registry | `cmd/tusk-lint/main.go` | Phase 2 task 5 |
| Per-package `varnamelen` exclusion rules (one per package, twelve in total) | `.golangci.yml` `linters.exclusions.rules` | Each rule removed by the corresponding sweep phase (3–7); any residual rules removed in Phase 8 task 1 |

## Changes Introduced

- **New file:** `STYLE.md` (repo root) — convention reference.
- **New file:** `cmd/tusk-lint/main.go` — multichecker shell.
- **Modified:** `CONTRIBUTING.md` — one-line cross-reference to STYLE.md.
- **Modified:** `Makefile` — adds `lint-tusk` target; `lint` depends
  on `lint-go` + `lint-tusk`.
- **Modified:** `.golangci.yml` — enables `varnamelen` linter, adds
  twelve per-package exclusion rules, adds
  `linters.settings.varnamelen` block.
- **Possibly modified:** `go.mod` / `go.sum` — adds
  `golang.org/x/tools` dependency if not already present.
- **No code in `service/`, `internal/`, `domain/`, `filter/`,
  `repository/`, `sqlite/`, or any other production package is
  modified in this phase.**
