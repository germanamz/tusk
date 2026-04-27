# Phase 3 — Migrate inline_expansion + portability test files

> Initiative: Repo-Root Tusk Workspace
> Spec: `docs/superpowers/specs/2026-04-27-repo-root-tusk-workspace-design.md`

## Prerequisites

- Phase 1 (Harness Foundation) complete.

Phase 2 is **not** strictly required for this phase — the work here
does not touch MCP code. If Phases 2 and 3 land in separate commits
on the same branch, they can be reordered or run in parallel by
different implementer agents. In the planned commit sequence, Phase 2
lands first.

## Inherits From

The codebase at the start of this phase has:

- Phase 1's harness extensions: `newCmd`, `Env.WithHome`,
  `Env.WithoutDBArg`, `Env.WithoutFormat`, default `workDir =
  t.TempDir()`.
- (If Phase 2 has landed: `MCPEnv` exists; legacy `mcpEnv` deleted.
  Not a dependency for this phase.)
- `tests/e2e/inline_expansion_test.go::runWithStdin` (around line 378)
  — to be deleted.
- `tests/e2e/portability_test.go::runTusk` and `mustRunTusk` (around
  lines 22-45) — to be deleted.
- `tests/e2e/portability_test.go::freshDBPath`, `decodeWorkspace`,
  `stripVolatile` — kept (pure data helpers).

## Goal

Migrate `inline_expansion_test.go::TestCLI_InlineExpansion_Stdin` to
the harness's `Step.Stdin` mechanism, and migrate every test in
`portability_test.go` to use `Env.Run` directly. Delete the now-unused
`runWithStdin`, `runTusk`, and `mustRunTusk` helpers.

## Tasks

### 1. Migrate `TestCLI_InlineExpansion_Stdin` to `runScenarios`

In `tests/e2e/inline_expansion_test.go`, replace the existing
`TestCLI_InlineExpansion_Stdin` function (lines ~287-376) with a
`runScenarios`-based implementation. The two cases (`annotate_stdin`
and `description_stdin`) become `Scenario` entries. Each scenario's
stdin-piping step uses `Step{Stdin: "...", Args: []string{...}}`.

Sketch:

```go
func TestCLI_InlineExpansion_Stdin(t *testing.T) {
    scenarios := []Scenario{
        {
            Name: "annotate_stdin",
            Steps: []Step{
                {Args: []string{"task", "create", "Stdin annotate target"}},
                {
                    Stdin: "piped note body",
                    Args:  []string{"task", "annotate", "$0.short_id", "@-"},
                },
                {
                    Args: []string{"task", "get", "$0.short_id"},
                    AssertText: func(t *testing.T, output string) {
                        assertContains(t, output, "piped note body")
                    },
                    AssertJSON: func(t *testing.T, parsed any) {
                        // adjust based on existing JSON assertion shape
                    },
                },
            },
        },
        {
            Name: "description_stdin",
            Steps: []Step{
                {
                    Stdin: "piped description body",
                    Args:  []string{"task", "create", "Stdin desc task", "description=@-"},
                },
                {
                    Args: []string{"task", "get", "$0.short_id"},
                    AssertText: func(t *testing.T, output string) {
                        assertContains(t, output, "piped description body")
                    },
                },
            },
        },
    }
    runScenarios(t, binPath, scenarios)
}
```

Match the existing assertion semantics from the pre-migration form.

### 2. Delete `runWithStdin`

After Task 1, `runWithStdin` (lines ~378-end of `inline_expansion_test.go`)
has no remaining callers. Delete it. Remove any imports that become
unused (`bufio`, `bytes`, etc., depending on what's left).

### 3. Migrate every test in `portability_test.go`

Each of the 6 test functions currently calls `mustRunTusk(t, dbPath,
args...)` or `runTusk(t, dbPath, stdin, args...)`. The migration
replaces those with `Env`-driven invocations.

Test functions:

- `TestPortability_RoundTrip` (line 85) — single `Env`. Export from a
  populated DB, decode the JSON, assert structural shape.
- `TestPortability_StdinStdout` (line 123) — single `Env`. Pipe via
  `Step.Stdin` (or, if `Env.Run` doesn't fit the stdin shape for a
  given test, set `env.step.stdin` directly before `env.Run` — that
  field is package-private in `harness.go`).
- `TestPortability_SchemaVersionError` (line 154) — single `Env`.
  Pipe a malformed JSON dump on stdin, assert the import errors.
- `TestPortability_FKValidationError` (line 183) — single `Env`.
  Same shape as schema-version test.
- `TestPortability_CollisionWithoutReplace` (line 225) — two `Env`s
  (export from one workspace, attempt to import into a populated
  second workspace, assert collision error).
- `TestPortability_DryRunDoesNotMutate` (line 247) — single `Env`.
  Run import with `--dry-run`, assert no rows inserted.

Each `Env` is constructed via `newEnv(t, binPath, "flag", "json")` (or
`"text"` if the test currently asserts text output) — pick the
`(dbMode, format)` pair that matches the test's current behavior. The
tests do not need the matrix runner; they're single-mode tests.

For roundtrip tests that need two DBs, construct two `Env`s in the
test body. Each `Env` has its own `dbPath` and `workDir`. Pass the
export file path between them as a command-line argument
(unchanged from the pre-migration form).

### 4. Delete `runTusk` and `mustRunTusk`

After Task 3, these helpers have no remaining callers. Delete them
from `tests/e2e/portability_test.go`. Keep `freshDBPath`,
`decodeWorkspace`, and `stripVolatile`.

Remove any imports that become unused (`bytes`, `os/exec`,
`path/filepath` may still be needed for `freshDBPath` — verify).

### 5. Run `make test-e2e`

All inline-expansion and portability tests must pass. If a test
fails, the migration introduced a behavioral difference — debug
before declaring the phase complete.

Common pitfalls:

- Misordering scenario steps so `$0.short_id` references the wrong
  step.
- Forgetting that `runScenarios` runs each scenario across the
  `(dbMode, format)` matrix — assertions that previously ran once
  per case now run four times. The original test already ran across
  the matrix via its own loop, so net coverage is unchanged.
- Stdin-piping inside `Env.Run`: the harness reads `e.step.stdin`,
  which `runScenarios` sets from `Step.Stdin`. Tests that call
  `env.Run` directly (not through `runScenarios`) must set
  `env.step.stdin` before each call.

## User-visible behaviors that must still work

- `TestCLI_InlineExpansion_Stdin` covers the same stdin-driven
  inline expansion paths it covered before (annotate `@-` body and
  `description=@-`).
- All 6 portability tests pass: roundtrip JSON dump+restore,
  stdin/stdout piping, schema-version mismatch error, foreign-key
  validation error, ID-collision error, dry-run-doesn't-mutate.
- The cross-workspace export/import flow (collision test) still
  produces the same error message it does today.

## Bridge code

None.

## Changes Introduced

**Deleted code:**
- `tests/e2e/inline_expansion_test.go::runWithStdin`.
- `tests/e2e/portability_test.go::runTusk`.
- `tests/e2e/portability_test.go::mustRunTusk`.

**Modified files:**
- `tests/e2e/inline_expansion_test.go` —
  `TestCLI_InlineExpansion_Stdin` rewritten in `runScenarios` style.
- `tests/e2e/portability_test.go` — 6 test functions migrated to
  `Env`-driven calls.

**Kept:**
- `tests/e2e/portability_test.go::freshDBPath`.
- `tests/e2e/portability_test.go::decodeWorkspace`.
- `tests/e2e/portability_test.go::stripVolatile`.

**No new files, env vars, schema migrations, or dependencies.**
