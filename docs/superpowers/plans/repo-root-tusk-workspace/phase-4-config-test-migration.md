# Phase 4 — Migrate config_test.go

> Initiative: Repo-Root Tusk Workspace
> Spec: `docs/superpowers/specs/2026-04-27-repo-root-tusk-workspace-design.md`

## Prerequisites

- Phase 1 (Harness Foundation) complete.
- Phase 2 (MCPEnv migration) complete — `TestMCP_DisabledTools` was
  already migrated in Phase 2; this phase does not touch it.

Phase 3 is not strictly required (no overlap), but the planned commit
sequence has Phase 3 before Phase 4.

## Inherits From

The codebase at the start of this phase has:

- Phase 1's harness extensions in place.
- Phase 2's `MCPEnv`; `TestMCP_DisabledTools` in `config_test.go`
  already uses `NewMCPEnv(...).WithHome(homeDir)`. The legacy
  `newMCPEnvWithHome` helper is deleted.
- `config_test.go::envWithHome` (existing helper, not yet deleted —
  consumed by tests this phase migrates).

If Phase 3 has also landed (the planned commit sequence), then
`inline_expansion_test.go::runWithStdin` and
`portability_test.go::runTusk`/`mustRunTusk` are already deleted.
**This phase does not depend on Phase 3** — it touches only
`config_test.go`. Other files' migration state is irrelevant to the
work performed here.

## Goal

Migrate every remaining direct-exec test in `config_test.go` to use
`Env` with appropriate opt-outs (`WithHome`, `WithoutDBArg`,
`WithoutFormat`, `WithEnv`). Delete `envWithHome`.

## Tasks

### 1. Group A — `TestCLI_WithConfigFile`

In `tests/e2e/config_test.go::TestCLI_WithConfigFile` (line 14):

- Build `homeDir` and the fake `~/.config/tusk/config.toml` as
  today.
- Replace the direct `exec.Command + envWithHome` block with:

```go
env := newEnv(t, binPath, "flag", "text")  // dbMode/format pair: "flag"/"text" — verify matches existing assertions
env.WithHome(homeDir)
env.WithoutDBArg()  // test exercises storage.path resolution from the config file
r := env.Run("task", "create", "Config test task")
if r.Err != nil {
    t.Fatalf("tusk add failed: %v\nstderr: %s", r.Err, r.Stderr)
}
```

- Keep the post-run assertion that the DB file was created at
  `dbPath`.

### 2. Group B — config init / path / show / get / set / validate family

Migrate these 7 tests, each to use `Env` with `WithHome` and
`WithoutDBArg` (config commands don't need a `--db` flag):

- `TestCLI_ConfigInit` (line 104)
- `TestCLI_ConfigPath` (line 138)
- `TestCLI_ConfigShow` (line 156)
- `TestCLI_ConfigGet` (line 189)
- `TestCLI_ConfigSet` (line 257)
- `TestCLI_ConfigSet_BlockedFields` (line 373)
- `TestCLI_ConfigValidate` (line 417)

Pattern for each:

```go
homeDir := t.TempDir()
// (set up any pre-existing config files inside homeDir as before)

env := newEnv(t, binPath, "flag", "text")  // verify format matches existing assertions
env.WithHome(homeDir)
env.WithoutDBArg()
// for tests that assert non-default text output, also:
env.WithoutFormat()

r := env.Run("config", "init")  // or whichever subcommand
// existing assertions
```

For multi-step tests (e.g., `TestCLI_ConfigInit` runs `init` then
checks the file, then runs `init` again and asserts an error), use
multiple `env.Run(...)` calls in sequence — `Env` retains state
across calls.

For `TestCLI_ConfigGet` and `TestCLI_ConfigSet` which do `init` then
`get`/`set`, use a single `Env` for the sequence.

### 3. Group C1 — explicit `--config` / `TUSK_CONFIG` / precedence

Migrate these 5 tests:

- `TestCLI_ExplicitConfigFlag` (line 480)
- `TestCLI_TuskConfigEnv` (line 512)
- `TestCLI_FlagBeatsEnv` (line 543)
- `TestCLI_MissingExplicitConfigIsHardError` (line 581)
- `TestCLI_TuskEnvOverlaysExplicitConfig` (line 726)

Pattern:

```go
configFile := filepath.Join(t.TempDir(), "explicit.toml")
// (write configFile contents as before)

env := newEnv(t, binPath, "flag", "text")
env.WithoutDBArg()  // DB resolution via config file, not flag
// for TUSK_CONFIG-based tests:
env.WithEnv("TUSK_CONFIG", configFile)
// for --config flag-based tests, pass --config explicitly in args:
r := env.Run("--config", configFile, "task", "create", "Task")
```

`TestCLI_FlagBeatsEnv` sets both `TUSK_CONFIG` (via `WithEnv`) and
passes `--config` in args — the test asserts the flag wins. This
naturally falls out of `Env`'s envvar + arg layering.

### 4. Group C2 — config-path / show-header explicit-file variants

Migrate these 5 tests:

- `TestCLI_ConfigValidate_ExplicitFile` (line 602)
- `TestCLI_ConfigPath_ExistingGlobal` (line 631)
- `TestCLI_ConfigPath_ExplicitFile` (line 656)
- `TestCLI_ConfigShowHeader_ExplicitFile` (line 680)
- `TestCLI_ConfigShowHeader_GlobalFile` (line 705)

Same pattern as Group C1. The "existing global" tests use
`WithHome(homeDir)` to point at a homeDir that has a pre-seeded
`~/.config/tusk/config.toml`. The "explicit file" tests use
`--config <path>` in args.

### 5. Delete `envWithHome`

After Tasks 1-4, `envWithHome` has no remaining callers. Delete it
from `tests/e2e/config_test.go`. Remove any imports that become
unused.

Verify with `grep -n envWithHome tests/e2e/` — should match zero
lines after deletion.

### 6. Run `make test-e2e`

All ~21 tests in `config_test.go` must pass. Pay special attention to:

- `TestCLI_FlagBeatsEnv` — the most precedence-sensitive test. If
  the harness's env-injection ordering broke flag precedence,
  this test will fail.
- `TestCLI_TuskEnvOverlaysExplicitConfig` — exercises a subtle case
  where `TUSK_*` env vars overlay a flag-loaded config. The harness
  must inject env vars in the correct order.
- `TestCLI_MissingExplicitConfigIsHardError` — asserts a non-zero
  exit and a specific stderr message. `Env.Run` returns `Result.Err`
  and `Result.Stderr`; the assertion must use those.

## User-visible behaviors that must still work

- All ~20 non-MCP tests in `config_test.go` continue to pass with
  identical semantics.
- Config resolution precedence (`--config` > `TUSK_CONFIG` > walk-up
  > global) is exercised by the same tests it is today.
- `config init` / `config init --local` still create files at the
  expected paths (now relative to `Env.workDir`'s `t.TempDir()` for
  `--local`, or to `homeDir` for global).
- Tests that intentionally exercise `[storage] path = ...` resolution
  (`TestCLI_WithConfigFile`) still produce a DB at the path declared
  in the config file.

## Bridge code

None introduced. `envWithHome` (an existing helper, not bridge code)
is deleted in this phase — its consumers have all been migrated.

## Changes Introduced

**Deleted code:**
- `tests/e2e/config_test.go::envWithHome`.

**Modified code:**
- `tests/e2e/config_test.go` — ~20 test functions migrated.

**Kept:**
- `TestMCP_DisabledTools` (already migrated in Phase 2; not touched
  here).

**No new files, env vars, schema migrations, or dependencies.**
