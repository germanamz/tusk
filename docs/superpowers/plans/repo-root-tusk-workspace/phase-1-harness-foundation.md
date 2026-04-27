# Phase 1 — Harness Foundation

> Initiative: Repo-Root Tusk Workspace
> Spec: `docs/superpowers/specs/2026-04-27-repo-root-tusk-workspace-design.md`

## Prerequisites

None beyond the base codebase. This is the first phase.

## Goal

Add the layered harness primitives (`newCmd`, `Env` opt-outs, default
`workDir = t.TempDir()`) without changing any existing test call site.
Existing tests must continue to pass with their current behavior; new
opt-outs are off by default.

## Tasks

### 1. Add `newCmd` to `tests/e2e/harness.go`

Insert a new unexported function near the top of `harness.go`, after
the imports and before the existing `currentStep` type:

```go
// newCmd returns an exec.Cmd that runs binPath with args. cmd.Dir is set
// to a per-call t.TempDir() so tusk's walk-up config resolver never
// reaches an ancestor's tusk.toml. cmd.Env starts as os.Environ() with
// NO_COLOR=1 appended; callers append further env vars and set Stdin /
// Stdout / Stderr as needed.
func newCmd(t *testing.T, binPath string, args ...string) *exec.Cmd {
    t.Helper()
    cmd := exec.Command(binPath, args...)
    cmd.Dir = t.TempDir()
    cmd.Env = append(os.Environ(), "NO_COLOR=1")
    return cmd
}
```

### 2. Extend `Env` with HOME / DBArg / Format opt-outs

Add three fields to the `Env` struct definition, after `extraEnv` and
before `dbMode`:

```go
homeDir    string // set by WithHome; overrides HOME / USERPROFILE
skipDBArg  bool   // set by WithoutDBArg; suppress --db / TUSK_DB injection
skipFormat bool   // set by WithoutFormat; suppress --format injection
```

Add three fluent setter methods on `*Env`, after the existing
`WithEnv` method:

```go
// WithHome overrides HOME (and USERPROFILE on Windows) for every
// subsequent Run invocation. Used by tests that drive tusk's config
// resolver from a synthetic home directory.
func (e *Env) WithHome(dir string) {
    e.homeDir = dir
}

// WithoutDBArg suppresses both the --db flag and TUSK_DB env var on
// every subsequent Run invocation, regardless of dbMode. Used by
// tests that exercise storage.path resolution from a config file.
func (e *Env) WithoutDBArg() {
    e.skipDBArg = true
}

// WithoutFormat suppresses the --format flag on every subsequent Run
// invocation. Used by tests that assert tusk's default output format.
func (e *Env) WithoutFormat() {
    e.skipFormat = true
}
```

Fields and methods are introduced together — they're the same logical
extension of `Env`'s API. Defaults are zero-value, so existing callers
that don't invoke the new methods see no behavior change.

### 3. Refactor `Env.Run` to use `newCmd` and honor opt-outs

Replace the body of `Env.Run` so that:

- The `--db` / `--format` injection is conditional on `!e.skipDBArg` /
  `!e.skipFormat`.
- The command is constructed via `cmd := newCmd(e.t, e.binPath,
  fullArgs...)` instead of `cmd := exec.Command(...)`.
- `cmd.Dir` is overridden by `e.step.cwd` if set, else `e.workDir` if
  set, else left at `newCmd`'s default `t.TempDir()`.
- The env-construction block is rewritten:
  - Start from `cmd.Env` (already contains `os.Environ()` +
    `NO_COLOR=1` from `newCmd`).
  - If `e.homeDir != ""`, append `HOME=<homeDir>` and
    `USERPROFILE=<homeDir>` (on all platforms — the `USERPROFILE`
    setting is harmless on Linux).
  - If `e.dbMode == "env"` and `!e.skipDBArg`, append `TUSK_DB=<dbPath>`.
  - If `e.configDir != ""`, append `TUSK_CONFIG_DIR=<configDir>`.
  - Append `e.extraEnv...`.

The full updated `Run` method:

```go
func (e *Env) Run(args ...string) Result {
    e.t.Helper()

    expanded := make([]string, len(args))
    for i, arg := range args {
        expanded[i] = e.expandRefs(arg)
    }

    var fullArgs []string
    if e.dbMode == "flag" && !e.skipDBArg {
        fullArgs = append(fullArgs, "--db", e.dbPath)
    }
    if !e.skipFormat {
        fullArgs = append(fullArgs, "--format", e.format)
    }
    fullArgs = append(fullArgs, expanded...)

    cmd := newCmd(e.t, e.binPath, fullArgs...)

    if e.step.cwd != "" {
        cmd.Dir = e.step.cwd
    } else if e.workDir != "" {
        cmd.Dir = e.workDir
    }
    if e.step.stdin != "" {
        cmd.Stdin = strings.NewReader(e.step.stdin)
    }

    if e.homeDir != "" {
        cmd.Env = append(cmd.Env, "HOME="+e.homeDir, "USERPROFILE="+e.homeDir)
    }
    if e.dbMode == "env" && !e.skipDBArg {
        cmd.Env = append(cmd.Env, "TUSK_DB="+e.dbPath)
    }
    if e.configDir != "" {
        cmd.Env = append(cmd.Env, "TUSK_CONFIG_DIR="+e.configDir)
    }
    cmd.Env = append(cmd.Env, e.extraEnv...)

    var stdout, stderr strings.Builder
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr

    err := cmd.Run()
    r := Result{
        Stdout: stdout.String(),
        Stderr: stderr.String(),
        Err:    err,
    }
    e.results = append(e.results, r)
    return r
}
```

Note: the prior `Run` set `cmd.Env = os.Environ() + ...`. The new
version layers onto `cmd.Env` (which `newCmd` already populated with
`os.Environ() + NO_COLOR=1`), so the final env is identical for
existing callers.

### 4. Initialize `workDir = t.TempDir()` in `newEnv`

In the `newEnv` function, in the returned `&Env{...}` literal, add:

```go
workDir: t.TempDir(),
```

after `configDir: t.TempDir(),`. Existing call sites that use
`InDir(...)` continue to override `workDir` via the existing `InDir`
method.

### 5. Add the package-comment paragraph to `tests/e2e/harness.go`

At the very top of the file (above the existing `// tests/e2e/harness.go`
comment), prepend:

```go
// Package e2e drives the tusk binary as a black-box subprocess.
//
// All test commands route through newCmd, which sets cmd.Dir to a
// per-call t.TempDir(). Tusk's walk-up config resolver therefore
// never reaches the repo-root tusk.toml. Tests that need to exercise
// walk-up explicitly call env.InDir(...) to point CWD at a controlled
// directory. No test should construct exec.Command directly — newCmd
// is the only sanctioned construction path so the isolation invariant
// cannot drift.
//
// Caveat: cmd.Env starts from os.Environ(), so a developer shell with
// TUSK_CONFIG or TUSK_DB exported leaks into every test. CI runners
// have clean environments; locally, run `unset TUSK_CONFIG TUSK_DB`
// before `make test-e2e` if you need to verify isolation.
```

Then keep the existing `// tests/e2e/harness.go` comment and `package e2e`
declaration as-is below.

### 6. Verify the suite still passes

Run `make test-e2e` from the repo root. All existing scenarios must
pass unchanged. If anything fails, the refactor of `Env.Run`
introduced a behavioral difference — fix before declaring the phase
complete.

## User-visible behaviors that must still work

- All existing e2e tests pass with no test-file changes in this phase.
- `Env.Run` injects `--db` (when `dbMode == "flag"`) and `--format` as
  it does today. Auto-injection is on by default.
- `Env.Run` injects `TUSK_DB` (when `dbMode == "env"`) as it does today.
- `Env.Run` sets `TUSK_CONFIG_DIR` from `Env.configDir` as it does today.
- `Env.Run` honors `Step.Stdin` via `e.step.stdin` as it does today.
- `Env.InDir(dir)` continues to override `workDir`.
- `Env.WithEnv(k, v)` continues to append to `extraEnv`.
- `runScenarios` and `runWalkUpScenarios` work unchanged.
- The `t.TempDir()` set as `workDir` defaults silently for any test
  that previously had `cmd.Dir == ""` — those tests now run from a
  controlled temp dir instead of the test process's CWD. No existing
  test asserts on the inherited CWD, so behavior is preserved.

## Bridge code

None. This phase is purely additive at the harness level. Existing
helper functions (`mcpEnv`, `runTusk`, `runWithStdin`, `envWithHome`)
and the direct `exec.Command` callers in `mcp_test.go`,
`mcp_urgency_overrides_test.go`, `config_test.go`,
`portability_test.go`, `inline_expansion_test.go` are untouched in
this phase. They migrate in phases 2-4.

## Changes Introduced

**New code (in `tests/e2e/harness.go`):**
- Function `newCmd(t *testing.T, binPath string, args ...string) *exec.Cmd`
- Method `(*Env).WithHome(dir string)`
- Method `(*Env).WithoutDBArg()`
- Method `(*Env).WithoutFormat()`
- Fields on `Env`: `homeDir string`, `skipDBArg bool`, `skipFormat bool`
- Default value of `workDir` field set to `t.TempDir()` in `newEnv`
- Package-comment paragraph

**Modified code:**
- `Env.Run` body rewritten to use `newCmd` and honor opt-outs

**Removed code:**
- None.

**No new files. No new env vars. No schema migrations. No new
dependencies. No bridge code.**
