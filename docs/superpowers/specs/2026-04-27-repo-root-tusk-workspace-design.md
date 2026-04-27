# Design — Repo-Root Tusk Workspace

> Date: 2026-04-27
> Initiative: Repo-Root Tusk Workspace (v0.13)
> Tusk task: `6e04b824`
> Status: design — pending implementation

## Problem

Tusk's roadmap database lives at `.data/tusk.db`. Today, every contributor and
CI step that wants to read it must export `TUSK_DB="$(pwd)/.data/tusk.db"`
first. The repo already supports walk-up config discovery (v0.9): a
`tusk.toml` placed at the repo root with `[storage] path = ".data/tusk.db"`
would let any `tusk` invocation from inside the checkout resolve the
committed DB without env-var ceremony.

The cutover during v0.13 attempted this exact change and reverted. The
e2e test harness inherits CWD from the test process (typically inside
`/workspaces/tusk/tests/e2e`), so walk-up resolves the (newly committed)
repo-root `tusk.toml` as the active config for every scenario. Tests that
exercise `tusk config init --local` and `tusk config set` then *write*
into that committed file, polluting it for the rest of the suite. A
synthetic `[taxonomy]` section made level-check tests fail by demanding
levels on `default`-project tasks.

The retrospective (`docs/retrospectives/v0.13-roadmap-migration.md`,
"Follow-up: e2e harness is not hermetic against walk-up config")
describes the gap and lists viable fixes.

A narrow fix (default `cmd.Dir = t.TempDir()` in `Env.Run`) closes the
walk-up leak for harness-driven tests. But the test suite has accumulated
direct `exec.Command` callers — `mcp_test.go`, `config_test.go`,
`portability_test.go`, `inline_expansion_test.go` — each with its own
ad-hoc env-construction boilerplate, none of which set `cmd.Dir`. Each
direct caller is a latent re-leak waiting to happen. Rather than
hardening the existing direct callers in place (a partial fix), this
design unifies them onto a single layered harness API so that walk-up
isolation, env construction, and other process invariants live in
exactly one place.

## Goal

1. Make the entire e2e suite hermetic against walk-up config: with any
   `tusk.toml` present at the repo root or any ancestor of CWD, every
   test must pass.
2. Unify all direct `exec.Command` callers under a single layered
   harness API so the isolation invariant cannot drift.
3. Commit `tusk.toml` at the repo root pointing at `.data/tusk.db`.
4. Remove all `TUSK_DB=…` plumbing from CI and contributor workflows.

## Non-goals

- Any change to `cmd/tusk/main.go` config resolution. The existing chain
  (`--config` > `TUSK_CONFIG` > walk-up > global) is what makes this
  work and stays untouched.
- Workspace-level `[taxonomy]` in the committed `tusk.toml`. The
  `tusk-roadmap` project carries its own taxonomy in the DB; setting
  one workspace-wide would propagate to any future scratch project in
  this workspace.
- Restructuring tests beyond what the migration requires. If a test
  body has odd structure today, it stays odd; the migration only
  changes how the test invokes the binary.

## Approach

Single PR. Three layered harness primitives plus call-site migration:

### Layer 1 — `newCmd`

New constructor in `tests/e2e/harness.go`:

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

~10 lines. Sole authority over `cmd.Dir`. Used by Layer 2 and any
direct caller; no test in the suite invokes `exec.Command` directly
after this lands.

### Layer 2 — `Env` (extended)

`Env` keeps every existing field. New fluent setters:

- `(*Env) WithHome(dir string)` — sets a `homeDir` field. `Run` builds
  `cmd.Env` so `HOME=dir` and `USERPROFILE=dir` (Windows) reflect the
  override rather than the test process's actual HOME.
- `(*Env) WithoutDBArg()` — sets a `skipDBArg` flag. `Run` then skips
  the `--db` flag injection and the `TUSK_DB` env injection regardless
  of `dbMode`.
- `(*Env) WithoutFormat()` — sets a `skipFormat` flag. `Run` skips the
  `--format` injection.

`Env.Run` becomes thin: it expands `$N.field` refs, builds the args
list (subject to opt-outs), calls `newCmd`, then layers in `cmd.Env`
extras (`TUSK_CONFIG_DIR`, `TUSK_DB` if not skipped, `WithEnv`
extras, HOME override) and `cmd.Stdin` per the current step. Existing
behavior for callers that don't use the opt-outs is unchanged.

`newEnv` initializes `workDir = t.TempDir()`. The default
`cmd.Dir` chain (`step.cwd > workDir > newCmd's t.TempDir()`) means
walk-up never reaches the repo-root `tusk.toml`. Existing
`InDir(root)` keeps overriding `workDir` exactly as today.

### Layer 2-alt — `MCPEnv`

New file `tests/e2e/harness_mcp.go`. Long-lived `tusk mcp serve`
subprocess with stdin/stdout JSON-RPC pipes:

```go
type MCPEnv struct {
    t      *testing.T
    cmd    *exec.Cmd
    stdin  io.WriteCloser
    stdout *bufio.Reader
    nextID int
    homeDir string
    extraEnv []string
}

func NewMCPEnv(t *testing.T, binPath string) *MCPEnv
func (e *MCPEnv) WithHome(dir string) *MCPEnv  // chainable; must be called before first Send
func (e *MCPEnv) WithEnv(k, v string) *MCPEnv  // chainable; must be called before first Send
func (e *MCPEnv) Send(method string, params any) jsonRPCResponse
```

Construction: `newCmd(t, binPath, "--db", tmpDB, "mcp", "serve")` then
attach pipes, send `initialize`, register `t.Cleanup`. The `WithHome`
/ `WithEnv` setters mutate the cmd's env *before* `cmd.Start` runs;
calling them after the first `Send` is a `t.Fatalf`.

`mcp_test.go::newMCPEnv` and `mcp_urgency_overrides_test.go::newMCPEnvWithHome`
delete; both files target `NewMCPEnv` directly.

### Layer 3 — `runScenarios` / `runWalkUpScenarios` (unchanged)

Matrix runners over `(db_mode, format)`. Already built on `Env`. Tests
that fit the matrix shape use them; tests that don't (single-mode
config tests, MCP tests) call `Env`/`MCPEnv` directly in plain
`func TestX(t *testing.T)` bodies.

### Helpers that delete entirely

- `tests/e2e/portability_test.go::runTusk`, `mustRunTusk` — replaced by
  `Env.Run`. Roundtrip tests use two `Env`s.
- `tests/e2e/inline_expansion_test.go::runWithStdin` — replaced by
  `Step.Stdin` in `runScenarios`.
- `tests/e2e/mcp_test.go::newMCPEnv`, `newMCPEnvWithHome` and
  `tests/e2e/mcp_urgency_overrides_test.go`'s clone — replaced by
  `MCPEnv`.
- `tests/e2e/config_test.go::envWithHome` — replaced by
  `(*Env).WithHome`.

### Repo-root workspace config + plumbing removal

**New file `tusk.toml` at repo root**, ~5 lines:

```toml
# Workspace config for the tusk repo itself.
# Resolved via walk-up (v0.9), so any `tusk` invocation from inside
# the checkout points at the committed roadmap DB without env setup.
[storage]
path = ".data/tusk.db"
```

No `[taxonomy]` section.

**`.github/workflows/ci.yml` `roadmap-drift` step:** remove the
`env: TUSK_DB: ${{ github.workspace }}/.data/tusk.db` block.

**`CONTRIBUTING.md`:** drop the `export TUSK_DB="$(pwd)/.data/tusk.db"`
step (around lines 263-266). Reword the preceding paragraph (around
line 252, "CI points `TUSK_DB` at the committed file") so it explains
that walk-up discovery (v0.9) resolves the committed `tusk.toml` and
therefore the committed DB, with no env-var setup. The end-to-end flow
then reads:

```bash
# from inside the checkout — no env setup needed
tusk task create "Story: my new story" level=story project=tusk-roadmap parent=<initiative>
make roadmap
git add .data/tusk.db ROADMAP.md
```

**`docs/retrospectives/v0.13-roadmap-migration.md`:** append "Resolved
in PR #N (commit `<sha>`)" to the "Follow-up" section once the PR is
merged. Section itself stays.

## Tests

### Harness regression test — new file `tests/e2e/harness_isolation_test.go`

One scenario, `TestHarness_IsolatesFromAncestorTuskToml`. Two-part
structure (sanity + isolation) so a regression actually fails the
test rather than passing trivially.

**Setup.** Create a temp `seedRoot`. Write a `tusk.toml` at
`seedRoot` containing `[taxonomy] levels = [["milestone"],
["initiative"], ["story"], ["task", "spike"]]` — the exact shape
that broke the v0.13 cutover. Create `seedRoot/child/` as a
descendant directory.

**Part 1 — sanity check.** Construct an `Env` and call
`InDir(seedRoot/child)`. Run `tusk task create "should-fail"` (no
`project=`, no `level=`). With CWD inside `seedRoot`'s chain,
walk-up resolves the seed; the level-less create against the
`default` project must be **rejected**. If the create succeeds, the
seed is not operative — the test cannot meaningfully verify
isolation, so `t.Fatal` with a clear message before proceeding.

**Part 2 — isolation check.** `os.Chdir(seedRoot/child)` to mutate
the **test process's** CWD. Register a `t.Cleanup` that restores
`os.Getwd()`'s prior value. Construct a fresh `Env` and run
`tusk task create "should-succeed"` with **no `InDir` call**. If
the harness ever regressed to inheriting the test process's CWD,
walk-up from the inherited CWD would hit `seedRoot/tusk.toml` and
the level-less create would fail. With the working harness
(`workDir = t.TempDir()`), `cmd.Dir` is somewhere unrelated to
`seedRoot`'s ancestor chain and the create succeeds.

**Concurrency note.** This test mutates process CWD, so it must
**not** call `t.Parallel()`. Go's test runner runs sequential tests
serially before any parallel test cohort starts; the `t.Cleanup`
restores CWD before any subsequent test sees it.

**Rationale.** Every other test now routes through `Env`/`MCPEnv`,
both built on `newCmd`. This regression covers them transitively.
`config_walkup_test.go` already exercises walk-up resolution end to
end. The two-part design is what makes the test load-bearing —
without the sanity step, the isolation assertion would pass even if
the harness regressed.

### Package-comment documentation

Add a paragraph to the top of `tests/e2e/harness.go`:

> All test commands route through `newCmd`, which sets `cmd.Dir` to a
> per-call `t.TempDir()`. Tusk's walk-up config resolver therefore
> never reaches the repo-root `tusk.toml`. Tests that need to
> exercise walk-up explicitly call `env.InDir(...)` to point CWD at a
> controlled directory. No test should construct `exec.Command`
> directly — `newCmd` is the only sanctioned construction path so the
> isolation invariant cannot drift.

### Full-suite sentinel run — manual, once before opening the PR

`make test-e2e` from inside the repo with the new repo-root `tusk.toml`
present. Confirms no test fails. Once `tusk.toml` is committed, every
future e2e run is implicitly the sentinel run.

### Drift-check end-to-end verification

- **Locally:** with no `TUSK_DB` exported, `make roadmap && git diff
  --exit-code ROADMAP.md` exits zero.
- **CI:** the modified `roadmap-drift` job (post `TUSK_DB` removal)
  runs the same commands and stays green when
  `TUSK_ROADMAP_CHECK_ENABLED` is true.

## Failure modes & edge cases

1. **`/tmp` ancestor collision.** `t.TempDir()` lands under
   `os.TempDir()` (typically `/tmp` on Linux). If an outer ancestor
   of that directory holds a `tusk.toml`, walk-up finds it. The
   regression test implicitly covers this. Documented in the package
   comment as an assumption rather than guarded against in code.

2. **`os.Environ()` inheritance via `cmd.Env = os.Environ()`.** If
   the developer's shell has `TUSK_CONFIG=/some/path` or
   `TUSK_DB=/some/path` exported, those leak into every test. This
   is a pre-existing latent issue, not regressed by this PR. Noted in
   the package comment as a known caveat.

3. **`Env.WithHome` portability.** Tusk's config resolver uses
   `os.UserHomeDir`, which on Windows reads `USERPROFILE` not `HOME`.
   `WithHome(dir)` sets both env vars to the override; defensive guard
   for a future Windows CI slot.

4. **`Env.WithoutDBArg` and `dbMode`.** With `WithoutDBArg`, both
   `--db` and `TUSK_DB` are skipped regardless of `dbMode`. Tests that
   opt out are by definition testing config-driven DB resolution, so
   they run as plain Go tests (not via `runScenarios`) and the matrix
   becomes vacuous. Documented in the field's doc-comment.

5. **`MCPEnv` and walk-up.** The MCP subprocess is a tusk binary that
   walks up from its own CWD. `newCmd` sets `cmd.Dir = t.TempDir()`
   for it, so the same isolation applies.

6. **`tusk config init`-style tests.** `init` (no `--local`) writes to
   the global config dir, redirected via `TUSK_CONFIG_DIR`. With
   `cmd.Dir = t.TempDir()`, walk-up still finds nothing in the temp
   ancestor chain, so the global path is taken — same behavior as
   today.

7. **`tusk config init --local`-style tests.** They create a
   `tusk.toml` in the test's CWD. With `cmd.Dir` a per-call temp dir,
   they write there — improving hermeticity. Diff-review check:
   confirm no test asserts `tusk.toml` is created at a path that
   depended on the inherited CWD.

8. **Test parallelism.** `runScenarios` already uses `t.Parallel()`.
   Migrated tests using plain `func TestX(t)` must keep their
   original parallel/sequential behavior. Each test's `Env` gets its
   own `t.TempDir()` for `configDir`, so there's no shared global
   state.

9. **`portability_test.go` two-DB roundtrip.** Each test exports from
   one DB and imports into a second. With multi-`Env`, each `Env`
   gets its own `workDir` (separate temp ancestor chains, no
   interaction). The export file path crosses `Env`s — that's a
   command-line argument, unrelated to walk-up.

10. **`make roadmap` with no `TUSK_DB` set.** Walks up from the repo
    root, hits the new `tusk.toml`, resolves `storage.path =
    ".data/tusk.db"` relative to the config file's directory (= repo
    root). Same DB as before.

11. **CI `roadmap-drift` job after `TUSK_DB` removal.**
    `actions/checkout@v6` lands the working dir at
    `${GITHUB_WORKSPACE}`, which holds the committed `tusk.toml`.
    Walk-up finds the same file, behavior preserved.

12. **Tests that legitimately expect walk-up to land on the committed
    `tusk.toml`.** None exist (walk-up tests all use `InDir` to
    controlled roots). Diff-review check.

## Commit sequencing inside the PR

Each commit must keep `make test-e2e` green:

1. **Harness foundation** — add `newCmd`, refactor `Env.Run` to use
   it, set `workDir = t.TempDir()` default, add
   `WithHome`/`WithoutDBArg`/`WithoutFormat`. No call-site changes.
2. **`MCPEnv` introduction** — add `harness_mcp.go`. Old `mcpEnv`
   coexists.
3. **Migrate MCP tests** — `mcp_test.go` and
   `mcp_urgency_overrides_test.go` swap to `MCPEnv`. Old `mcpEnv` and
   constructors deleted.
4. **Migrate `inline_expansion_test.go`** — `TestCLI_InlineExpansion_Stdin`
   to `runScenarios`; delete `runWithStdin`.
5. **Migrate `portability_test.go`** — convert per-test to `Env`;
   delete `runTusk`/`mustRunTusk`.
6. **Migrate `config_test.go`** — convert each test to `Env` with
   appropriate opt-outs; delete `envWithHome`.
7. **Add `harness_isolation_test.go`** regression test.
8. **Commit repo-root `tusk.toml`, drop `TUSK_DB` plumbing** — new
   `tusk.toml`, edit `.github/workflows/ci.yml`, edit
   `CONTRIBUTING.md`. Regression test's value pays off here.
9. **Append "resolved in PR #N" to retrospective** — last,
   post-merge.

Each commit is independently revertable.

## File-by-file change list

| File | Change |
|---|---|
| `tests/e2e/harness.go` | Add `newCmd`. `Env.Run` refactored to use it. `newEnv` sets `workDir = t.TempDir()`. New methods `WithHome`, `WithoutDBArg`, `WithoutFormat`. Package-comment paragraph. |
| `tests/e2e/harness_mcp.go` | New file. `MCPEnv` type, constructor, `Send`, `WithHome`, `WithEnv`. |
| `tests/e2e/harness_isolation_test.go` | New file. One regression test. |
| `tests/e2e/mcp_test.go` | Delete `newMCPEnv` / `newMCPEnvWithHome`. Migrate ~50+ call sites to `NewMCPEnv` / `.WithHome`. |
| `tests/e2e/mcp_urgency_overrides_test.go` | Delete local `newMCPEnv` clone. Migrate call sites. |
| `tests/e2e/inline_expansion_test.go` | Delete `runWithStdin`. Migrate `TestCLI_InlineExpansion_Stdin` to `runScenarios` with `Step.Stdin`. |
| `tests/e2e/portability_test.go` | Delete `runTusk` / `mustRunTusk`. Each test builds `Env`(s) directly. `freshDBPath` / `decodeWorkspace` / `stripVolatile` kept. |
| `tests/e2e/config_test.go` | Delete `envWithHome`. Migrate ~46 exec sites to `Env` with `WithHome` / `WithoutDBArg` / `WithoutFormat` / `WithEnv` as needed. |
| `tusk.toml` | New file at repo root. `[storage] path = ".data/tusk.db"`. |
| `.github/workflows/ci.yml` | Remove `env: TUSK_DB:` block from `roadmap-drift` step. |
| `CONTRIBUTING.md` | Drop `export TUSK_DB=…` from roadmap workflow. Reword surrounding paragraph. |
| `docs/retrospectives/v0.13-roadmap-migration.md` | Append "resolved in PR #N" to follow-up section (post-merge). |

## Tusk task mapping

This design covers the full "Repo-Root Tusk Workspace" initiative
(`6e04b824`). All eleven descendant tasks land in this single PR. The
harness-migration scope expands beyond what the original ROADMAP tasks
described; the additional work is captured in the commit sequence
above and is necessary to durably close the walk-up isolation gap.

**Story 1 — Hermetic e2e harness against walk-up config (`15e25ede`):**
- `fd9c0d2b` — harness change → Layer 1 + Layer 2 work (commits 1-6)
- `0ae1febd` — regression test → §Tests + commit 7
- `82701dd1` — full-suite sentinel run → §Tests
- `a89df54d` — package-comment documentation → §Tests

**Story 2 — Commit repo-root tusk.toml and drop TUSK_DB plumbing (`a5b52d76`):**
- `16411749` — repo-root `tusk.toml` → commit 8
- `813fd114` — drop `TUSK_DB` from CI `roadmap-drift` → commit 8
- `cc9e5a80` — drop `export TUSK_DB` from CONTRIBUTING.md → commit 8
- `0b92922f` — link merged PR from retrospective → commit 9 (post-merge)
- `029bfef9` — verify drift check end-to-end → §Tests
