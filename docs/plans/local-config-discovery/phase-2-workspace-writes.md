# Phase 2 — Workspace-Aware Config Writes

## Goal

Add the two user-facing write commands that make walk-up useful: `config set`
learns to target the local `tusk.toml` by default (and the global file via
`--global`), and `config init` learns `--local` to drop a full dump of the
effective config into `./tusk.toml`. Walk-up discovery from Phase 1 is already
wired through `Load` and `ConfigFilePath`, so this phase is purely the TUI /
cobra layer plus tests.

## Prerequisites

- **Phase 1 complete.** `ResolveConfigFile` performs walk-up,
  `config.WithStartDir` exists, `cmd/tusk/main.go` passes `os.Getwd()` through
  `loadOpts`, and `config.ConfigFilePath` honors walk-up. Without Phase 1,
  `config set` cannot know what "local" means.
- No dependency on Phase 3.

## Inherits From

State after Phase 1:

- `config.Load(..., config.WithStartDir(cwd))` resolves local `tusk.toml`
  discovered by walk-up and populates `cfg.Sources.File` with the hit.
- `config.ConfigFilePath(opts ...)` returns the same path `Load` resolved,
  meaning it already returns the local `tusk.toml` when walk-up finds one and
  the global `config.toml` otherwise.
- `cmd/tusk/main.go` builds `loadOpts` once, including `WithStartDir(cwd)`
  when `os.Getwd()` succeeds, and passes `loadOpts` into `tui.New` via the
  existing wiring at `cmd/tusk/main.go:127–131`. The `tui.App` stores those
  opts on `a.loadOpts` (see `internal/tui/app.go`).
- `internal/tui/config.go:268–327` contains the current `runConfigSet`
  implementation. It already uses `config.ConfigFilePath(a.loadOpts...)`,
  which after Phase 1 returns the walk-up hit when present. That means
  `tusk config set ...` **already** writes to the local file by default after
  Phase 1 — this phase adds the `--global` escape hatch and the error message
  for the "no file anywhere" case.
- `internal/tui/config.go:124–151` contains the current `runConfigInit`. It
  only supports global init.
- E2E harness `Env` has a `workDir` field and an `InDir(dir string)` helper
  (added in Phase 1, Task 5).

## Context Pointers

- `internal/tui/config.go:19–71` — `buildConfigCmd` registers all config
  subcommands. New flags hang off the existing `cobra.Command` constructors.
- `internal/tui/config.go:268–327` — `runConfigSet`. Reuse the load-modify-
  write pipeline; only the "which path" decision changes.
- `internal/tui/config.go:124–151` — `runConfigInit`. Reuse `WriteConfig`;
  only the target path differs.
- `config/config.go:156–179` — `Option`, `loadOptions`, `WithSearchPath`,
  `WithExplicitFile`. These are the knobs for steering `ConfigFilePath` away
  from walk-up when `--global` is passed.
- `config/write.go:23–98` — `ConfigFilePath`, `LoadFile`, `WriteConfig`. All
  reusable.
- `tests/e2e/config_test.go` — existing `config set` and `config init`
  scenarios for reference patterns.
- `tests/e2e/config_walkup_test.go` (new in Phase 1) — has the fixture
  patterns for building a temp dir with a local `tusk.toml` and using
  `env.InDir`.
- `PRODUCT.md:388–396` — documented behavior for `config set --global` and
  `config init --local`.
- `ROADMAP.md:628–637` — story definitions. Do not tick; Phase 3 handles that.

## Tasks

### Task 1 — Add `--global` flag to `config set` and implement path selection

**File:** `internal/tui/config.go`

1. In `buildConfigCmd`, convert the `set` `cobra.Command` to a variable so you
   can attach a flag:

   ```go
   setCmd := &cobra.Command{
       Use:   "set <key> <value>",
       Short: "Set a config value and write to file",
       Args:  cobra.ExactArgs(2),
       RunE:  a.runConfigSet,
   }
   var setGlobal bool
   setCmd.Flags().BoolVar(&setGlobal, "global", false,
       "Write to the global config (~/.config/tusk/config.toml) even when a local tusk.toml is active")
   configCmd.AddCommand(setCmd)
   ```

   Store `setGlobal` somewhere `runConfigSet` can read it. Since `runConfigSet`
   is a method, the cleanest option is to read the flag inside the handler:

   ```go
   global, _ := cmd.Flags().GetBool("global")
   ```

   That keeps the App struct unchanged.

2. Rework the path-selection block at the top of `runConfigSet`
   (currently `internal/tui/config.go:276–285`):

   ```go
   path, err := a.resolveConfigWritePath(global)
   if err != nil {
       return err
   }
   ```

   Add a new helper on `*App`:

   ```go
   // resolveConfigWritePath picks the file `config set` should write to.
   // When global is true, walk-up and any explicit file are bypassed and the
   // global config path is returned (creating the file from defaults if it
   // does not yet exist). When global is false, the path matches whatever
   // Load() would read — typically the walk-up hit or the global file.
   func (a *App) resolveConfigWritePath(global bool) (string, error) {
       if global {
           // Strip walk-up and explicit-file opts so ConfigFilePath returns
           // the global path even when the caller is inside a workspace.
           opts := stripLocalOpts(a.loadOpts)
           path, err := config.ConfigFilePath(opts...)
           if err != nil {
               return "", err
           }
           if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
               if err := ensureGlobalConfigFile(path); err != nil {
                   return "", err
               }
           }
           return path, nil
       }

       path, err := config.ConfigFilePath(a.loadOpts...)
       if err != nil {
           return "", err
       }
       if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
           return "", fmt.Errorf(`no config file found; run "tusk config init" or "tusk config init --local"`)
       }
       return path, nil
   }
   ```

   Helper specifics:

   - `stripLocalOpts(opts []config.Option) []config.Option` — rebuild the
     slice without any option that would influence walk-up or explicit-file.
     Since `config.Option` is an opaque function type, the simplest approach
     is to give it a tag: extend `config` with a public marker or, easier,
     keep `loadOpts` in the TUI layer as a typed struct that records which
     options were requested, and reconstruct a fresh `[]config.Option` here.

     **Chosen approach (simplest, no config-package churn):** store the raw
     `startDir` and `explicitFile` values alongside `loadOpts` on the `App`.
     `cmd/tusk/main.go` already knows both values when it builds `loadOpts`;
     have it pass them in as extra fields on whatever struct `tui.New`
     consumes. See Task 2 for the wiring detail.

   - `ensureGlobalConfigFile(path string) error` — resolve the directory,
     `os.MkdirAll`, load a fresh default `config.Config` via `config.Load()`
     with no options (pure defaults + an ephemeral global path), and write
     via `config.WriteConfig`. Mirror the existing `runConfigInit` body at
     `internal/tui/config.go:124–151` — factor it if the duplication bothers
     you, but a small copy is acceptable.

3. Leave the rest of `runConfigSet` (the `LoadFile` → Viper → Unmarshal →
   Validate → `WriteConfig` pipeline at lines 287–327) untouched.

### Task 2 — Propagate local opts to the TUI layer

**Files:** `cmd/tusk/main.go`, `internal/tui/app.go`, `internal/tui/config.go`

1. In `internal/tui/app.go`, extend the `App` struct (and `New`) with two new
   fields:

   ```go
   configStartDir   string
   configExplicitFile string
   ```

   Update the `tui.New` signature to accept them. This is a mechanical change
   — only `cmd/tusk/main.go` calls `tui.New`.

2. In `cmd/tusk/main.go:127–131`, pass the values already computed at the top
   of `run()`:

   ```go
   app := tui.New(
       taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, playerSvc,
       tui.VersionInfo{...},
       cfg.TUI, cfg.MCP,
       loadOpts,
       startDir, explicitConfig,
   )
   ```

3. In `internal/tui/config.go`, implement `stripLocalOpts`:

   ```go
   func (a *App) stripLocalOpts() []config.Option {
       // Rebuild loadOpts without WithStartDir / WithExplicitFile so
       // ConfigFilePath falls through to the global branch.
       // Keep any search-path override (e.g. TUSK_CONFIG_DIR honored by
       // config.resolveGlobalDir) by passing it through explicitly if needed.
       return nil // defaults: no startDir, no explicit file, global branch wins
   }
   ```

   Returning `nil` is sufficient in practice: `config.ConfigFilePath(nil...)`
   falls through to `resolveGlobalDir` which still honors `TUSK_CONFIG_DIR`,
   the env var the e2e harness uses to isolate the global directory. The
   stored `a.configStartDir` / `a.configExplicitFile` fields exist to let
   future code reason about the caller's context — they are not consumed in
   this phase but Phase 3 docs reference them. If that bothers you, collapse
   this task into Task 1 and keep the fields out.

   **Simpler path:** drop Task 2 entirely and let `stripLocalOpts()` always
   return `nil`. That matches the current behavior of `config.ConfigFilePath`
   when called with no options and still honors `TUSK_CONFIG_DIR`. The
   implementer may pick either approach; document the choice in the commit.

### Task 3 — Add `--local` flag to `config init`

**File:** `internal/tui/config.go`

1. Convert the `init` `cobra.Command` to a variable in `buildConfigCmd` the
   same way as Task 1, with a new bool flag:

   ```go
   var initLocal bool
   initCmd.Flags().BoolVar(&initLocal, "local", false,
       "Write ./tusk.toml instead of the global config file")
   ```

2. In `runConfigInit`, branch on the flag:

   ```go
   local, _ := cmd.Flags().GetBool("local")
   if local {
       return a.runConfigInitLocal(cmd)
   }
   // existing global path (unchanged)
   ```

3. Implement `runConfigInitLocal`:

   - Compute the target: `cwd, err := os.Getwd()`; `target := filepath.Join(cwd, "tusk.toml")`.
   - If the file already exists, return `fmt.Errorf("file exists: %s", target)`.
     Use `os.Stat` + `os.IsNotExist` to distinguish exists from permission
     errors — only a clean "already exists" should produce the friendly
     message; any other stat error should propagate.
   - Load the current effective config via `config.Load(a.loadOpts...)`. This
     includes defaults merged with whatever walk-up or env already resolved.
     Do **not** build a fresh Config from scratch — the story says "a full
     dump of the current effective config" (`ROADMAP.md:634`).
   - Write with `config.WriteConfig(cfg, target)`.
   - Print `Created ./tusk.toml` (or the absolute path — pick one and be
     consistent with the existing `runConfigInit` output, which prints the
     absolute path).

### Task 4 — E2E: `config set` local and `--global`

**File:** `tests/e2e/config_walkup_test.go` (extend the file created in
Phase 1)

Add scenarios:

- `config_set_local_writes_to_walkup_file` — `$tmp/tusk.toml` pre-populated
  with valid defaults (reuse a fixture builder; simplest is `tusk config init
  --local` in a prior step, but since we're testing set in isolation, write a
  minimal valid TOML file directly). `env.InDir($tmp)`, `TUSK_CONFIG_DIR`
  pointed at an **empty** global dir. Run `tusk config set tui.color false`.
  Assert stdout/stderr are clean, read `$tmp/tusk.toml` directly from disk,
  assert it contains `color = false`. Assert the global temp dir is still
  empty (no `config.toml` spawned).

- `config_set_global_flag_writes_to_global_even_with_local_present` — same
  `$tmp/tusk.toml` setup, same empty `TUSK_CONFIG_DIR`. Run
  `tusk config set --global tui.color false`. Assert the global
  `$TUSK_CONFIG_DIR/config.toml` now exists and contains `color = false`.
  Assert `$tmp/tusk.toml` is **unchanged** (same mtime or same content —
  content check is simpler).

- `config_set_no_file_errors_helpfully` — no `tusk.toml` anywhere (workDir is
  an isolated temp dir), `TUSK_CONFIG_DIR` pointed at another empty temp dir.
  Crucially: prevent the auto-create path by running `config set` as the very
  first command in this scenario. After Phase 1, global auto-create happens
  inside `config.Load` on first run. Since `config set` calls `config.Load`
  indirectly via `ConfigFilePath`/`LoadFile` pipeline, this may or may not
  trigger auto-create depending on the implementation of `resolveConfigWritePath`.

  Expected behavior: `config set` without `--global`, running outside any
  workspace, should either (a) write to the auto-created global file (if
  Load-style auto-create fires) or (b) emit the
  `no config file found; run "tusk config init" ...` error. Pick one
  behavior explicitly in Task 1 and encode the assertion here to match. The
  simpler product behavior is (b): `config set` without `--global` should
  error when the only viable target is a not-yet-created global file, because
  that's what the roadmap story wording suggests
  (`ROADMAP.md:631`: "With no active file and no --global, emit a clear error").

  If the implementation falls into (a) by accident (because `config.Load`
  runs somewhere in the pipeline and auto-creates), adjust
  `resolveConfigWritePath` to stat the path explicitly before writing and
  return the error — this is what the helper body in Task 1 already does.

  Assert `r.Err != nil`, `assertStderrContains(t, r, "no config file found")`,
  and that the global temp dir is still empty after the call.

### Task 5 — E2E: `config init --local`

**File:** `tests/e2e/config_walkup_test.go`

Add scenarios:

- `config_init_local_creates_file` — empty `$tmp`, `env.InDir($tmp)`. Run
  `tusk config init --local`. Assert exit 0, assert `$tmp/tusk.toml` exists
  on disk, assert it parses as valid TOML and is a valid config (either
  parse inline with `go-toml` or run `tusk config validate` as a follow-up
  step). Run `tusk config show` from the same workDir; assert the
  `# active: ...` header contains `$tmp/tusk.toml`.

- `config_init_local_refuses_overwrite` — pre-create `$tmp/tusk.toml` with
  arbitrary valid content. `env.InDir($tmp)`. Run `tusk config init --local`.
  Assert `r.Err != nil`, `assertStderrContains(t, r, "file exists")`. Read
  `$tmp/tusk.toml` and confirm it was **not** overwritten.

Reuse the existing `runScenarios` matrix if possible, or run these as
standalone `t.Run` blocks if the dbMode/format axes don't add signal. For
init/set tests the output format matters less (no structured payload to
validate in JSON mode); pick whichever is simpler. Be consistent with the
style chosen in Phase 1's `config_walkup_test.go`.

## User-Visible Behaviors To Preserve

- `tusk config init` with no flag still creates the global config file (or
  prints "already exists"), same as today.
- `tusk config set <key> <value>` with **no** local `tusk.toml` and **no**
  `--global` still errors with a clear message — same as today, same message
  text shape (`no config file found; run "tusk config init" ...`), extended
  to mention `--local`.
- `tusk config set <key> <value>` with a local `tusk.toml` writes to that
  file. (This already works after Phase 1 via `ConfigFilePath` walk-up; this
  phase keeps it working and adds coverage.)
- `tusk config set --global <key> <value>` writes to the global file even
  when a local `tusk.toml` is active.
- `tusk config init --local` creates `./tusk.toml` containing a full effective
  config dump. Fails if the file already exists.
- All validation still runs before write — an invalid value is rejected with
  the same error text as before.
- `tusk config edit`, `config show`, `config path`, `config get`,
  `config validate` are untouched. They continue to follow walk-up precedence
  from Phase 1.
- All existing `tests/e2e/config_test.go` scenarios still pass unchanged.

## Changes Introduced

**Modified files**

- `internal/tui/config.go` — flag registration for `set --global` and
  `init --local`, new `resolveConfigWritePath` helper, new
  `runConfigInitLocal` helper, optional `ensureGlobalConfigFile` helper.
- `internal/tui/app.go` — optional: new fields on `App` for `configStartDir`
  and `configExplicitFile` if Task 2's opt-in version is chosen. If the
  implementer takes the simpler "return `nil` from stripLocalOpts" path,
  `app.go` is untouched.
- `cmd/tusk/main.go` — optional: matches the `tui.New` signature change from
  Task 2 if taken. Otherwise untouched.
- `tests/e2e/config_walkup_test.go` — new scenarios from Tasks 4 and 5
  appended to the file created in Phase 1.

**New public API**

- New cobra flags `--global` on `config set`, `--local` on `config init`.
- No new exported Go types or functions outside the `tui` package.

**No schema migrations. No new dependencies. No new environment variables.**

**Bridge code:** none.

## Commit

Two commits keep the diff reviewable:

1. `feat(tui): add --global flag to config set`
2. `feat(tui): add --local flag to config init`

A single combined commit is also acceptable; prefer two if the diff is large.
