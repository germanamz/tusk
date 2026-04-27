# Phase 5 — Cutover: regression test, repo-root tusk.toml, plumbing removal

> Initiative: Repo-Root Tusk Workspace
> Spec: `docs/superpowers/specs/2026-04-27-repo-root-tusk-workspace-design.md`

## Prerequisites

- Phase 1 (Harness Foundation) complete.
- Phase 2 (MCPEnv migration) complete.
- Phase 3 (inline + portability migration) complete.
- Phase 4 (config_test.go migration) complete.

Every test in the e2e suite must route through `Env` or `MCPEnv` —
both built on `newCmd`, both isolated from walk-up by default. **This
phase exercises that invariant by committing the file that would have
broken the suite without it.**

## Inherits From

The codebase at the start of this phase has:

- Phase 1's harness extensions: `newCmd`, `Env.WithHome`,
  `WithoutDBArg`, `WithoutFormat`, default `workDir = t.TempDir()`.
- Phase 2's `MCPEnv` and migrated MCP tests.
- Phase 3's migrated inline-expansion and portability tests;
  `runTusk`, `mustRunTusk`, `runWithStdin` deleted.
- Phase 4's migrated `config_test.go` tests; `envWithHome` deleted.
- Suite-wide invariant: no test in `tests/e2e/` constructs
  `exec.Command` directly. Every test goes through `Env` or
  `MCPEnv`, and both go through `newCmd`.

The implementer can rely on:

- `make test-e2e` passing from any CWD.
- `cmd/tusk/main.go` unchanged — the existing config resolution chain
  (`--config` flag > `TUSK_CONFIG` env > walk-up > global) is what
  makes the new repo-root `tusk.toml` resolve correctly.
- `.github/workflows/ci.yml`'s `roadmap-drift` step still has its
  `env: TUSK_DB:` block (line ~161) — to be removed.
- `CONTRIBUTING.md` still has the `export TUSK_DB="$(pwd)/.data/tusk.db"`
  step (around lines 263-266) and the surrounding paragraph — to be
  rewritten.

## Goal

Add the harness regression test, commit the repo-root `tusk.toml`,
and drop every `TUSK_DB=...` plumbing artifact from CI and
`CONTRIBUTING.md`. Verify the drift check still passes end-to-end.

## Tasks

### 1. Add `tests/e2e/harness_isolation_test.go`

Create the file with one test, two-part structure (sanity check +
isolation check). The sanity check proves the seeded `tusk.toml` is
actually operative; without it, the isolation assertion would pass
even if the harness regressed to inheriting the test process's CWD.

```go
package e2e

import (
    "os"
    "path/filepath"
    "testing"
)

// TestHarness_IsolatesFromAncestorTuskToml verifies that the default
// Env.Run path (no InDir override) keeps cmd.Dir out of any ancestor
// chain that contains a tusk.toml. The threat is a harness regression
// that would inherit the test process's CWD — walk-up from there
// would hit /workspaces/tusk/tusk.toml (committed in this PR) and
// every test in the suite would inherit the workspace taxonomy.
//
// This test cannot run with t.Parallel() — it mutates the test
// process's CWD. Go's test runner runs sequential tests before any
// parallel cohort, so the t.Cleanup-restored CWD is in place by the
// time other tests run.
func TestHarness_IsolatesFromAncestorTuskToml(t *testing.T) {
    if binPath == "" {
        t.Skip("binary not built")
    }

    // Setup: seedRoot/tusk.toml + seedRoot/child/.
    seedRoot := t.TempDir()
    seedTOML := filepath.Join(seedRoot, "tusk.toml")
    seedContent := []byte(`[taxonomy]
levels = [["milestone"], ["initiative"], ["story"], ["task", "spike"]]
`)
    if err := os.WriteFile(seedTOML, seedContent, 0o644); err != nil {
        t.Fatalf("writing seed tusk.toml: %v", err)
    }
    childDir := filepath.Join(seedRoot, "child")
    if err := os.MkdirAll(childDir, 0o755); err != nil {
        t.Fatalf("mkdir child: %v", err)
    }

    // Part 1 — sanity. With InDir(childDir), walk-up resolves
    // seedRoot/tusk.toml. A level-less create against the default
    // project must be rejected, proving the seed is operative.
    sanity := newEnv(t, binPath, "flag", "text")
    sanity.InDir(childDir)
    sr := sanity.Run("task", "create", "should-fail")
    if sr.Err == nil {
        t.Fatalf("sanity check failed: walk-up did not reach seed tusk.toml from %s. stdout: %s",
            childDir, sr.Stdout)
    }

    // Part 2 — isolation. Mutate test-process CWD into childDir so
    // that any regression that inherits CWD would walk up into
    // seedRoot. The working harness defaults workDir = t.TempDir(),
    // so cmd.Dir lives in a separate ancestor chain regardless.
    saved, err := os.Getwd()
    if err != nil {
        t.Fatalf("os.Getwd: %v", err)
    }
    if err := os.Chdir(childDir); err != nil {
        t.Fatalf("chdir to %s: %v", childDir, err)
    }
    t.Cleanup(func() {
        if err := os.Chdir(saved); err != nil {
            t.Logf("warning: failed to restore CWD to %s: %v", saved, err)
        }
    })

    isolated := newEnv(t, binPath, "flag", "text")
    ir := isolated.Run("task", "create", "should-succeed")
    if ir.Err != nil {
        t.Fatalf("isolation broken: harness leaked walk-up via test-process CWD.\nstderr: %s\nstdout: %s",
            ir.Stderr, ir.Stdout)
    }
}
```

**Why two parts:** the sanity check is what makes Part 2's assertion
load-bearing. If we asserted only that an isolated create succeeds
without first proving that a child-of-seedRoot create *fails*, a
silent harness regression would still pass the test (because the
`workDir = t.TempDir()` default would take cmd.Dir to a temp dir
unrelated to `seedRoot`'s chain — even from a regressed harness,
walk-up wouldn't reach `seedRoot` from a sibling temp dir).

**Why mutate process CWD in Part 2:** walk-up follows ancestors
only, so for the isolation assertion to actually exercise the chdir
default we need a CWD whose ancestor chain *would* hit the seed if
inherited. `os.Chdir(childDir)` creates that ancestry. The
`t.Cleanup` restores CWD — Go's testing framework guarantees Cleanup
runs before any parallel cohort starts, so no other test sees the
mutation.

### 2. Create `tusk.toml` at the repo root

New file `tusk.toml` at `/workspaces/tusk/tusk.toml`:

```toml
# Workspace config for the tusk repo itself.
# Resolved via walk-up (v0.9), so any `tusk` invocation from inside
# the checkout points at the committed roadmap DB without env setup.
[storage]
path = ".data/tusk.db"
```

That's the entire file. No `[taxonomy]`, no `[urgency]`, nothing else.

### 3. Edit `.github/workflows/ci.yml`

In the `roadmap-drift` job (around lines 139-164), remove the `env`
block from the `Verify ROADMAP.md is up to date` step. Before:

```yaml
      - name: Verify ROADMAP.md is up to date
        if: ${{ vars.TUSK_ROADMAP_CHECK_ENABLED == 'true' }}
        env:
          TUSK_DB: ${{ github.workspace }}/.data/tusk.db
        run: |
          make roadmap
          git diff --exit-code ROADMAP.md
```

After:

```yaml
      - name: Verify ROADMAP.md is up to date
        if: ${{ vars.TUSK_ROADMAP_CHECK_ENABLED == 'true' }}
        run: |
          make roadmap
          git diff --exit-code ROADMAP.md
```

`actions/checkout@v6` lands the working dir at
`${GITHUB_WORKSPACE}`, which contains the new `tusk.toml`; walk-up
resolves the same DB.

### 4. Edit `CONTRIBUTING.md`

Two edits in the "ROADMAP.md is generated" section (around lines
244-290):

**Edit A** (around line 252): change the existing sentence about CI
pointing `TUSK_DB` at the committed file. Replace text similar to "CI
points `TUSK_DB` at the committed file and runs `make roadmap`" with
something like:

> `tusk.toml` at the repo root sets `[storage] path = ".data/tusk.db"`,
> so any `tusk` invocation from inside the checkout — locally or in
> CI — resolves the committed DB via walk-up config discovery (v0.9).
> No env-var setup is required.

**Edit B** (around lines 263-266): replace the numbered step that says
"Point tusk at the committed DB" with a description of the new
zero-setup workflow. The existing list:

```
1. Point tusk at the committed DB (one-shot for the session):
   ```bash
   export TUSK_DB="$(pwd)/.data/tusk.db"
   ```
2. Edit the roadmap via `tusk task` commands. Examples:
   ...
```

Becomes:

```
1. Edit the roadmap via `tusk task` commands. Examples:
   ```bash
   tusk task create "Story: my new story" level=story project=tusk-roadmap parent=<initiative-short-id>
   tusk task done <short-id>
   tusk task move <short-id> --before <target>
   ```
2. Regenerate the markdown:
   ...
```

The remaining numbered steps shift up by one. Verify the renumbering
is consistent with prose references elsewhere in the file.

### 5. Verify drift check end-to-end (locally)

From the repo root, with `TUSK_DB` **not** exported:

```bash
unset TUSK_DB
make roadmap
git diff --exit-code ROADMAP.md
```

The `make roadmap` step must exit zero. The `git diff --exit-code`
step must exit zero (no drift).

If `make roadmap` fails because the binary can't find the DB, the
walk-up resolver isn't picking up `tusk.toml` — debug before
declaring the phase complete.

If `git diff --exit-code` reports a diff in `ROADMAP.md`, the
roadmap regeneration is producing different output than the committed
file. Investigate: it should be identical.

### 6. Verify the e2e suite passes with the committed `tusk.toml`

```bash
make test-e2e
```

This is the moment the regression test from Task 1 pays off. With
`tusk.toml` committed at the repo root, every test must still pass —
the harness migration ensures no test sees the file via walk-up.

If anything fails, the harness migration in Phases 1-4 missed a
direct-exec caller. Find the offending test (likely something that
runs the binary outside of `Env` / `MCPEnv`) and route it through
`newCmd`.

## User-visible behaviors that must still work

- `make roadmap` regenerates `ROADMAP.md` from `.data/tusk.db` with
  no `TUSK_DB` env var set.
- `git diff --exit-code ROADMAP.md` exits zero immediately after
  `make roadmap`, both locally and in the modified `roadmap-drift`
  CI job.
- `tusk task ...` from any subdirectory of the repo resolves
  `.data/tusk.db` via walk-up.
- Every existing e2e test passes with the new `tusk.toml` present.
- `tests/e2e/config_walkup_test.go` tests still pass (they explicitly
  use `InDir`, immune to the change).

## Bridge code

None.

## Follow-up (not gating phase completion)

After the PR merges, append a single line to
`docs/retrospectives/v0.13-roadmap-migration.md` in the
"Follow-up: e2e harness is not hermetic against walk-up config"
section: "Resolved in PR #N (commit `<sha>`)." This is post-merge,
performed by the planning agent (or whoever merges the PR), not part
of the phase's exit criteria.

## Changes Introduced

**New files:**
- `tests/e2e/harness_isolation_test.go` — one test function.
- `tusk.toml` at repo root — 5 lines of TOML + a comment.

**Modified files:**
- `.github/workflows/ci.yml` — remove `env: TUSK_DB:` block from
  `roadmap-drift` step.
- `CONTRIBUTING.md` — reword roadmap workflow section to drop the
  `export TUSK_DB` step.

**No new env vars, no schema migrations, no new dependencies.**
