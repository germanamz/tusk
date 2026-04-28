# Phase 2 — Custom analyzers (rules 2, 3, 4)

> Milestone: v0.14
> Phase: 2 of 8
> Spec: [`../../specs/2026-04-28-v0.14-naming-convention-design.md`](../../specs/2026-04-28-v0.14-naming-convention-design.md) §1, §2

## Inherits From

Phase 1 has shipped. The implementer can rely on:

- `STYLE.md` exists at the repo root with all four rules documented.
- `cmd/tusk-lint/main.go` exists with `multichecker.Main()` and an
  empty analyzer registry (bridge code).
- `Makefile` has `lint-tusk` and `lint-go` targets; `make lint`
  depends on both.
- `.golangci.yml` has `varnamelen` enabled with a full-codebase path
  exclusion under `linters.exclusions.rules`.
- `golang.org/x/tools` is in `go.mod`.

## Prerequisites

Phase 1.

## Goal

Implement the three custom `go/analysis` analyzers (`blankline`,
`namederr`, `testhandle`), wire them into `cmd/tusk-lint`, and add a
shared path-filter helper that all three honor. After this phase,
all four rules are linter-enforced but every existing directory is
still excluded — `make lint` continues to find zero violations
against the unmodified codebase.

## Tasks

1. **Implement `internal/lint/blankline/` (rule 2).** Create:
   - `analyzer.go` — exports `var Analyzer = &analysis.Analyzer{...}`.
     Logic: walk every function body. For each `*ast.AssignStmt`
     where one of the LHS identifiers is named `err` (or matches the
     named-error pattern `<noun>Err`) and is followed in the same
     block by an `*ast.IfStmt` whose condition is `err != nil` (or
     `<noun>Err != nil`), report a diagnostic if there is no blank
     line between them. Same check for the line after the `if`
     statement's closing brace and the next statement.
   - `analyzer_test.go` — uses
     `analysistest.Run(test, testdata, blankline.Analyzer, "a")`.
     Note the `test *testing.T` parameter name: `internal/lint/` is
     **not** path-excluded, so this phase's own analyzer test files
     must comply with rule 4 (`*testing.T` → `test`) from the moment
     they land. Apply the same naming to the testhandle and namederr
     test files in tasks 2 and 3.
   - `testdata/src/a/a.go` — fixture file with both passing and
     failing patterns. Fixture files are loaded by `analysistest`
     and must contain intentional violations; they are not part of
     the main build and are not linted by `make lint`. Failing
     patterns annotated with `// want "blankline: missing blank
     line after error assignment"` (or similar message) per the
     standard `analysistest` format.

2. **Implement `internal/lint/namederr/` (rule 3).** Create:
   - `analyzer.go` — exports `var Analyzer = &analysis.Analyzer{...}`.
     Logic: walk every function body. Within each lexical scope
     (`*ast.BlockStmt`), count `*ast.AssignStmt` statements that
     declare a variable named `err` via `:=`. If the count is ≥ 2,
     emit a diagnostic on every such assignment instructing the
     implementer to use a typed name (`<noun>Err`). Exact message:
     `"namederr: 'err' is shadowed N times in this scope; rename all
     instances to typed names (e.g. fooErr, barErr)"`. The first
     occurrence is also flagged — see spec §1 rule 3.
   - `analyzer_test.go` and `testdata/src/a/a.go` mirroring the
     blankline structure.

3. **Implement `internal/lint/testhandle/` (rule 4).** Create:
   - `analyzer.go` — exports `var Analyzer = &analysis.Analyzer{...}`.
     Logic: walk every `*ast.FuncDecl` and `*ast.FuncType`. For each
     parameter, resolve its type. If the type matches one of the
     entries in this hardcoded table:

     ```go
     var requiredNames = map[string]string{
         "*testing.T":  "test",
         "*testing.B":  "bench",
         "testing.TB":  "harness",
     }
     ```

     and the parameter's name does not match the required name, emit
     a diagnostic. Exact message: `"testhandle: parameter of type %s
     must be named %q, got %q"`.
   - `analyzer_test.go` and `testdata/src/a/a.go`. The testdata must
     include the `testing` package import; configure the test fixture
     to build with `analysistest`'s standard library support.

4. **Add a shared path-filter helper.** Create
   `internal/lint/pathfilter/pathfilter.go`. Exports a single
   function `func Excluded(pkgPath string) bool` that consults a
   compiled-in **slice of per-package regexes** (matching the
   per-package exclusion rules added to `.golangci.yml` in Phase 1).
   Per-package entries are intentional: parallel sweep phases (3–7)
   each remove a different entry, so branches do not merge-conflict
   on a single shared alternation. Verify the module path from
   `go.mod` before hardcoding.

   ```go
   package pathfilter

   import "regexp"

   var excluded = []*regexp.Regexp{
       // each entry removed by the corresponding sweep phase;
       // any residual entries removed in Phase 8.
       regexp.MustCompile(`^github\.com/germanamz/tusk/service(/|$)`),
       regexp.MustCompile(`^github\.com/germanamz/tusk/internal/tui(/|$)`),
       regexp.MustCompile(`^github\.com/germanamz/tusk/internal/mcp(/|$)`),
       regexp.MustCompile(`^github\.com/germanamz/tusk/internal/portability(/|$)`),
       regexp.MustCompile(`^github\.com/germanamz/tusk/filter(/|$)`),
       regexp.MustCompile(`^github\.com/germanamz/tusk/domain(/|$)`),
       regexp.MustCompile(`^github\.com/germanamz/tusk/syntax(/|$)`),
       regexp.MustCompile(`^github\.com/germanamz/tusk/repository(/|$)`),
       regexp.MustCompile(`^github\.com/germanamz/tusk/sqlite(/|$)`),
       regexp.MustCompile(`^github\.com/germanamz/tusk/cmd(/|$)`),
       regexp.MustCompile(`^github\.com/germanamz/tusk/tests/e2e(/|$)`),
       regexp.MustCompile(`^github\.com/germanamz/tusk$`), // root package (client.go)
   }

   func Excluded(pkgPath string) bool {
       for _, regex := range excluded {
           if regex.MatchString(pkgPath) {
               return true
           }
       }
       return false
   }
   ```

   Each analyzer's `Run` function calls
   `pathfilter.Excluded(pass.Pkg.Path())` and short-circuits with no
   diagnostics if true. The slice entries are **bridge code** —
   sweep phases remove their corresponding entry; Phase 8 removes
   any remaining entries (or deletes the helper if the slice is
   empty).

5. **Register all three analyzers in `cmd/tusk-lint/main.go`.**
   Replace the bridge-code empty registry from Phase 1 task 3 with:

   ```go
   package main

   import (
       "golang.org/x/tools/go/analysis/multichecker"

       "github.com/germanamz/tusk/internal/lint/blankline"
       "github.com/germanamz/tusk/internal/lint/namederr"
       "github.com/germanamz/tusk/internal/lint/testhandle"
   )

   func main() {
       multichecker.Main(
           blankline.Analyzer,
           namederr.Analyzer,
           testhandle.Analyzer,
       )
   }
   ```

   Remove the Phase 1 bridge-code comment.

6. **Verify analyzers detect documented violations and `make lint`
   still passes.** Run `go test ./internal/lint/...` to verify the
   testdata fixtures fire as expected. Run `make lint` against the
   unmodified codebase — must report zero violations because every
   existing package is excluded by the path filter. Run
   `tusk-lint -blankline ./internal/lint/blankline/testdata/...`
   manually to confirm the per-analyzer flag works.

## User-visible behaviors (acceptance criteria)

- `tusk-lint -blankline ./...`, `tusk-lint -namederr ./...`,
  `tusk-lint -testhandle ./...` each run independently and report
  zero violations against the unmodified codebase (path-excluded).
- `tusk-lint ./...` (all three) reports zero violations against the
  unmodified codebase.
- `go test ./internal/lint/...` passes — every analyzer's testdata
  fires the documented diagnostics.
- `make lint`, `make test`, `make build` continue to pass.

## Bridge code introduced

| Bridge | Location | Removal target |
|--------|----------|----------------|
| Per-package exclusion entries in `excluded` slice (twelve entries) | `internal/lint/pathfilter/pathfilter.go` | Each entry removed by the corresponding sweep phase (3–7); any residual entries removed in Phase 8 task 2 |

## Bridge code removed

| Bridge from earlier phase | Replaced by |
|---------------------------|-------------|
| Phase 1 empty analyzer registry in `cmd/tusk-lint/main.go` | This phase task 5 — three analyzers registered |

## Changes Introduced

- **New files:** `internal/lint/blankline/{analyzer.go,
  analyzer_test.go, testdata/src/a/a.go}`.
- **New files:** `internal/lint/namederr/{analyzer.go,
  analyzer_test.go, testdata/src/a/a.go}`.
- **New files:** `internal/lint/testhandle/{analyzer.go,
  analyzer_test.go, testdata/src/a/a.go}`.
- **New file:** `internal/lint/pathfilter/pathfilter.go` (shared
  path-filter helper).
- **Modified:** `cmd/tusk-lint/main.go` — registers three analyzers,
  imports new packages.
- **Possibly modified:** `go.mod` / `go.sum` — additional
  `golang.org/x/tools/go/analysis/analysistest` test imports may
  pull transitive dependencies.
- **No production code outside `internal/lint/` and
  `cmd/tusk-lint/` is modified in this phase.**
