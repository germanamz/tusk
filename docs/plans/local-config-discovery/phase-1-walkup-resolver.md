# Phase 1 — Walk-Up Resolver & Load Plumbing

## Goal

Land walk-up config discovery in the `config` package and wire the CLI entry
point to feed it the current working directory. After this phase, running
`tusk` from any subdirectory of a project that contains a `tusk.toml` picks up
that file automatically. Global `~/.config/tusk/config.toml` is no longer
auto-created when a local workspace is active. Every other tusk command keeps
working exactly as before.

## Prerequisites

- Base codebase as of commit `063593b` (Explicit Config File Resolver initiative
  complete — `ResolveConfigFile(startDir, explicitFile, globalDir)` exists with
  `startDir` reserved but unused).
- No other phases required.

## Inherits From

None — this is the first phase in the initiative. Implementer should expect the
repository in the state described in `PRODUCT.md` §Configuration and
`config/resolver.go` as currently committed.

## Context Pointers

Read before starting:

- `config/resolver.go` — existing stub, lines 9–40. The `startDir` parameter is
  already in the signature; this phase gives it behavior.
- `config/config.go:196–261` — `Load()` and the `loadOptions` struct. Note the
  existing `WithExplicitFile` / `WithSearchPath` options and the
  `ensureConfigFile` auto-create at lines 228–232.
- `config/write.go:23–48` — `ConfigFilePath` mirrors resolver precedence and
  must stay in lock-step.
- `cmd/tusk/main.go:33–133` — CLI entry. `loadOpts` is built here and passed
  through to `tui.New`. `cfg.Sources.File` already feeds
  `sqlite.ResolveWorkspacePath` at lines 54–59, so relative `storage.path`
  resolution is already implemented — it only needs walk-up to populate
  `Sources.File` with a local path.
- `sqlite/paths.go:14–29` — `ResolveWorkspacePath`. Do not modify; it already
  handles relative, absolute, and `~` correctly.
- `tests/e2e/harness.go:24–112` — e2e `Env` struct. `Env.Run` builds `exec.Cmd`
  but does not set `cmd.Dir`; this phase extends it.
- `PRODUCT.md:354–425` — the Configuration and Workspace Scope sections. Source
  of truth for precedence wording.
- `ROADMAP.md:613–647` — the initiative definition. Do **not** tick the story
  checkboxes in this phase; that happens in Phase 3.

## Tasks

### Task 1 — Implement walk-up in `ResolveConfigFile`

**File:** `config/resolver.go`

Replace the `_ = startDir` placeholder with a walk-up loop that runs **between**
the explicit-file branch and the global-directory branch. Specifically:

1. If `explicitFile != ""`, keep the current behavior (existing file returned,
   missing file is a hard error). The walk-up is skipped entirely.
2. Otherwise, if `startDir != ""`:
   - Begin at `startDir`. Use `os.Stat(filepath.Join(dir, "tusk.toml"))`. If
     the stat succeeds, return that path.
   - Otherwise, advance to `filepath.Dir(dir)`. Stop when the new directory
     equals the current one (filesystem root reached).
   - Do **not** call `filepath.EvalSymlinks`. Use plain `os.Stat`. The walk
     must not follow symlinked ancestors into unrelated trees.
3. If walk-up finds nothing (or `startDir == ""`), fall through to the existing
   global-directory branch unchanged.

Update the doc comment on `ResolveConfigFile` to reflect the new precedence:

```
//  1. explicitFile — if set, must exist; missing file is a hard error.
//  2. walk-up from startDir looking for tusk.toml (when startDir != "" and
//     explicitFile is empty).
//  3. globalDir/config.toml — returned when it exists.
//  4. "" — "defaults only".
```

**Tests** (add to `config/resolver_test.go`, extending the existing
`TestResolveConfigFile` table or adding a new `TestResolveConfigFileWalkUp`):

- `walkup_cwd_hit`: `startDir` contains `tusk.toml` → returned.
- `walkup_ancestor_hit`: `tusk.toml` sits at `startDir`'s grandparent → found
  after two walk steps.
- `walkup_root_stop`: neither `startDir` nor any ancestor has a `tusk.toml`;
  globalDir is empty → return `""` without error and without hanging.
- `walkup_walks_over_global`: a `tusk.toml` at an ancestor **and** a
  `config.toml` in `globalDir` → walk-up wins.
- `walkup_skipped_when_explicit`: `explicitFile` is set, `startDir` contains a
  `tusk.toml` → explicit wins, walk-up never runs.
- `walkup_empty_startDir_preserves_old_behavior`: existing test cases with
  `startDir=""` continue to pass.

Use `t.TempDir()` for every filesystem fixture. No changes to other tests.

### Task 2 — Add `WithStartDir` option and gate global auto-create in `Load`

**File:** `config/config.go`

1. Extend `loadOptions` with `startDir string`. Add the public option:

   ```go
   // WithStartDir sets the directory used for walk-up discovery of a local
   // tusk.toml. Only meaningful when no explicit file is configured.
   func WithStartDir(path string) Option {
       return func(o *loadOptions) { o.startDir = path }
   }
   ```

2. In `Load`, after applying options, pass `lo.startDir` into
   `ResolveConfigFile` as the first argument. Remove the hardcoded `""`.

3. **Reorder** the global auto-create step. Today `ensureConfigFile` runs
   unconditionally before the resolver (lines 228–232). Change the order so
   auto-create only happens after the resolver returns `""`:

   ```go
   filePath, err := ResolveConfigFile(lo.startDir, lo.explicitFile, globalDir)
   if err != nil {
       return nil, err
   }

   if filePath == "" && lo.explicitFile == "" && globalDir != "" {
       if err := ensureConfigFile(globalDir); err != nil {
           return nil, err
       }
       filePath = filepath.Join(globalDir, "config.toml")
   }
   ```

   This guarantees that a walk-up hit suppresses global auto-create — running
   tusk inside a project with its own config never spawns a global file. Fresh
   installs outside any tusk project still get a global `config.toml` created
   on first run.

4. Update the `Load` doc comment to reflect the new precedence (walk-up between
   explicit and global, auto-create conditional).

**Tests** (add to `config/config_test.go`):

- `Load_walkup_uses_local_file`: temp dir with a minimal `tusk.toml` that
  overrides `tui.color = false`. Call `Load(WithStartDir(dir), WithSearchPath(emptyGlobal))`.
  Assert `cfg.Sources.File == filepath.Join(dir, "tusk.toml")`, assert
  `cfg.TUI.Color == false`, assert `emptyGlobal` still contains no
  `config.toml` after the call (confirming auto-create was skipped).
- `Load_walkup_miss_autocreates_global`: temp global dir + temp startDir with
  no `tusk.toml`. Call `Load(WithStartDir(startDir), WithSearchPath(globalDir))`.
  Assert the global `config.toml` got created and `Sources.File` points at it.
- `Load_explicit_beats_walkup`: temp startDir with a `tusk.toml` + explicit file
  elsewhere. Call `Load(WithStartDir(startDir), WithExplicitFile(explicit))`.
  Assert `Sources.File == explicit`.

### Task 3 — Teach `ConfigFilePath` about walk-up

**File:** `config/write.go`

`ConfigFilePath` is used by `tusk config set`, `tusk config init`, and
`tusk config edit` to decide which file to write. It must match the Load
resolver so `config set` writes to the same file `Load` reads from.

1. Extend `ConfigFilePath` to honor `WithStartDir`. After the explicit-file
   branch, run the same walk-up loop used in `ResolveConfigFile` (factor a
   private helper `walkUpForLocal(startDir string) string` in `resolver.go`
   and call it from both sites). If walk-up returns a hit, return it.

2. If walk-up misses, fall through to the existing global-path branch
   (`globalDir/config.toml`). Keep the current behavior of returning the
   would-be global path even when it does not yet exist — `config init` relies
   on that to know where to create the file.

3. Do not break any existing caller. The signature stays the same
   (`ConfigFilePath(opts ...Option) (string, error)`).

**Tests** (add to `config/write_test.go`):

- `ConfigFilePath_walkup_hit_returns_local`: startDir contains a `tusk.toml`,
  global dir also has a `config.toml`, walk-up wins.
- `ConfigFilePath_walkup_miss_falls_through_to_global`: startDir has no
  `tusk.toml`, global `config.toml` exists → global path returned.
- `ConfigFilePath_walkup_miss_no_global_file_still_returns_path`: global dir
  exists but no `config.toml` — returned path is `globalDir/config.toml` so
  `config init` can create it.

### Task 4 — Pass `os.Getwd()` from the CLI entry point

**File:** `cmd/tusk/main.go`

1. After `resolveConfigPath` (line 34) and before calling `config.Load`,
   compute the start directory:

   ```go
   startDir, _ := os.Getwd() // best-effort; empty string disables walk-up
   if startDir != "" {
       loadOpts = append(loadOpts, config.WithStartDir(startDir))
   }
   ```

2. Ignore the `os.Getwd()` error deliberately — a deleted CWD should downgrade
   to "no walk-up" rather than aborting startup. Do not add a new error return.

3. No other changes to `main.go`. `sqlite.ResolveWorkspacePath` is already
   called with `filepath.Dir(cfg.Sources.File)` at lines 54–59; a walk-up hit
   will automatically flow through and resolve `storage.path` relative to the
   local `tusk.toml` directory.

**No unit tests** at this layer — behavior is covered by the e2e scenarios in
Task 6.

### Task 5 — Extend the e2e harness with a working directory

**File:** `tests/e2e/harness.go`

1. Add a `workDir string` field to `Env`.
2. In `Env.Run`, after constructing `exec.Cmd`, set `cmd.Dir = e.workDir` when
   `e.workDir != ""`. This causes the binary to start inside the given
   directory so walk-up can find a `tusk.toml` there.
3. Add a helper method on `Env`:

   ```go
   // InDir sets the working directory used for subsequent Run invocations.
   // Used by walk-up scenarios that need the binary to start inside a
   // specific temp directory.
   func (e *Env) InDir(dir string) { e.workDir = dir }
   ```

4. Do not break existing scenarios. `workDir` defaults to `""`, which preserves
   the current behavior (no `cmd.Dir` set).

### Task 6 — E2E walk-up scenarios

**File:** new `tests/e2e/config_walkup_test.go`

The existing `tests/e2e/config_test.go` is already 745 lines; put walk-up
scenarios in a new file to keep the diff reviewable.

Reuse `runScenarios` so each scenario runs under both db modes and both output
formats. Each scenario should:

1. Build its own temp directory tree.
2. Call `env.InDir(...)` before the first `env.Run`.
3. Keep `env.configDir` (the env var `TUSK_CONFIG_DIR`) pointing at a separate
   temp directory — one that starts empty — so the global branch is isolated
   from the host's real `~/.config/tusk`.

Required scenarios:

- `walkup_cwd_hit` — `$tmp/tusk.toml` with `tui.color = false`. `env.InDir($tmp)`,
  run `config show` and `config path`. Assert the `# active: ...` header
  contains `$tmp/tusk.toml`, assert `config path` stdout equals that path,
  assert the rendered config contains `color = false`.

- `walkup_ancestor_hit` — `$tmp/tusk.toml`, create `$tmp/a/b/c`, `env.InDir($tmp/a/b/c)`,
  run `config path`. Assert stdout equals `$tmp/tusk.toml`.

- `walkup_no_global_autocreate_when_local_present` — local `tusk.toml` in
  startDir, `TUSK_CONFIG_DIR` pointed at an **empty** temp dir. Run any simple
  command (`tusk list`). After the command completes, assert that the global
  temp dir still contains **no** `config.toml`. This is the story "Conditional
  global auto-create" from `ROADMAP.md:638`.

- `walkup_explicit_config_flag_overrides` — create a local `$tmp/tusk.toml`
  with `tui.color = false` and a different `$other/custom.toml` with
  `tui.color = true`. `env.InDir($tmp)`, run `tusk --config $other/custom.toml config show`.
  Assert the active header points at `$other/custom.toml`.

  Note: `--config` is a pre-cobra flag parsed by `main.resolveConfigPath`. It
  must appear before the subcommand in args. The harness currently only
  injects `--db` and `--format` as prefixes; passing `--config` as an arg to
  `env.Run` should work, since `cmd/tusk/main.go:206` scans raw `os.Args`.
  Verify this works before checking in the scenario; if the harness ordering
  interferes, add `--config` ahead of `--format` in `Env.Run` the same way
  `--db` is handled.

- `walkup_tusk_config_env_overrides` — same setup but override via
  `TUSK_CONFIG` env var. Extend `Env` with an optional `TUSK_CONFIG` field, or
  append the env var directly inside the scenario using an escape hatch (e.g.
  a `withEnv(k, v)` helper). Keep the harness extension minimal and scoped to
  this scenario file if possible.

- `walkup_storage_path_relative_resolves_to_config_dir` — `$tmp/tusk.toml`
  containing:

  ```toml
  [storage]
  path = "./tusk.db"
  ```

  `env.InDir($tmp/sub)` (with `sub` pre-created). Run `tusk add "foo"`,
  capture its short id. Then `env.InDir($tmp)`, run `tusk list` and assert
  the task "foo" is present. This verifies that both working directories see
  the same database file (the one next to `tusk.toml`).

  Important: this scenario must run in `dbMode == "flag"` *only not applicable*
  — passing `--db` would bypass `storage.path`. Either skip this scenario for
  the `flag` dbMode via an explicit check at the top of the scenario, or run
  it outside `runScenarios` as a standalone `t.Run(...)`. Preferred: standalone
  `t.Run` that builds an `Env` directly without the matrix.

All assertions use the existing `assertContains`, `assertStderrContains`, etc.
helpers.

## User-Visible Behaviors To Preserve

These must still work after this phase ships:

- `tusk` with no `tusk.toml` anywhere creates `~/.config/tusk/config.toml` on
  first run (or the `TUSK_CONFIG_DIR` equivalent).
- `tusk --config path/to/file.toml` with a missing file still errors hard.
- `tusk config show`, `config path`, `config get`, `config validate`,
  `config edit`, `config set`, `config init` all still run with the same
  behavior they had before in the "no local `tusk.toml`" case.
- `TUSK_*` environment variables (e.g. `TUSK_STORAGE_PATH`) still override
  values loaded from the active file.
- `--db` flag and `TUSK_DB` env still win over `storage.path` from any config
  file.
- The embedded default `kanban` workflow and `_default` project still apply
  when no user file overrides them.
- All existing scenarios in `tests/e2e/config_test.go` continue to pass.

## Changes Introduced

**New files**

- `tests/e2e/config_walkup_test.go`

**Modified files**

- `config/resolver.go` — `ResolveConfigFile` implements walk-up; new private
  helper `walkUpForLocal(startDir string) string`.
- `config/config.go` — `loadOptions.startDir`, new `WithStartDir` option,
  `Load` reordered to gate auto-create on walk-up miss. Doc comment updated.
- `config/write.go` — `ConfigFilePath` honors walk-up via the shared helper.
- `config/resolver_test.go` — walk-up table cases.
- `config/config_test.go` — `Load_walkup_*` tests.
- `config/write_test.go` — `ConfigFilePath_walkup_*` tests.
- `cmd/tusk/main.go` — passes `os.Getwd()` via `config.WithStartDir`.
- `tests/e2e/harness.go` — `Env.workDir` + `Env.InDir` helper; `Env.Run` sets
  `cmd.Dir` when set.

**New public API**

- `config.WithStartDir(path string) Option`
- `(*e2e.Env).InDir(dir string)` (test-only package)

**No schema migrations. No new dependencies. No new environment variables.**

**Bridge code:** none. This phase is self-contained and ships as-is.

## Commit

One commit:
`feat(config): walk-up tusk.toml discovery from CWD`

Commit body should note the conditional global auto-create change and that
`ConfigFilePath` tracks walk-up for future `config set` alignment.
