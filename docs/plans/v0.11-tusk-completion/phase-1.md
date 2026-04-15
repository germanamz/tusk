# Phase 1 — `tusk completion` Subcommand (single phase)

## Goal

Ship a human-facing `tusk completion` command with four shell leaves — `bash`, `zsh`, `fish`, `powershell` — that emit completion scripts for the current Cobra command tree to stdout. Running the command (and Cobra's hidden `__complete` RPC) must not open the workspace database or run config resolution. Ship the e2e smoke and root-command regression tests that the roadmap calls for, plus the install-flow docs in `PRODUCT.md` and `docs/configuration.md`.

This initiative is intentionally a single phase. The story set is small (three stories in `ROADMAP.md` §`Initiative: tusk completion Subcommand`, lines 790-807), the code surface is additive, and every task is compile- and test-clean on its own. No later phase exists; there is nothing to sequence against.

## Prerequisites

- Base codebase at `main`, v0.11 phase 4 complete (`cafb3b9 feat(v0.11): phase 4 — task relations migration and MCP rename`).
- No other v0.11 initiative in flight on this phase's files. The v0.11 **String Field Input Unification** and **UDA Flag Elimination** initiatives both edit `internal/tui/commands.go`; this phase only edits `internal/tui/app.go`, `internal/tui/completion.go` (new), `cmd/tusk/main.go`, `cmd/tusk/main_test.go`, `tests/e2e/completion_test.go` (new), `PRODUCT.md`, and `docs/configuration.md`, so it can interleave with them in any order.

## Relevant Design Context

- Roadmap section: `ROADMAP.md` lines 790-807 — the three stories (wire, docs, tests) that this phase delivers end-to-end.
- Product description of the expected user surface: `PRODUCT.md` lines 237-260 — the existing "Shell completion" block that already lists the four commands and the install snippets. This phase makes that section accurate rather than aspirational.
- CLI entry point and command tree wiring: `internal/tui/app.go`, function `tui.New(...)`. `a.root` is constructed from a `&cobra.Command{...}` literal and every existing subcommand is attached via `a.root.AddCommand(...)` calls in the same function, ending with an `mcpCmd` block and a final `return a`. The completion command must be added inside this method, after the `mcpCmd` block. Line numbers are deliberately omitted — treat the anchors as semantic. Other v0.11 initiatives touching this file may shift numbers before this phase is implemented.
- Cobra's default completion auto-registration: `github.com/spf13/cobra@v1.10.2/completions.go:740-796` (`InitDefaultCompletionCmd`). Cobra auto-adds a `completion` command at `Execute` time unless `CompletionOptions.DisableDefaultCmd` is set. This phase owns the slot explicitly and sets `DisableDefaultCmd = true` so the auto-registration never collides with or shadows the explicit one.
- Constructor nil-tolerance: `internal/tui/app.go:83` documents that `taskSvc, tagSvc, and projectSvc may be nil for testing command registration`. `collectNonTerminalStatuses(nil)` at line 173 returns the fallback `["pending","active"]`, so the constructor does not panic under nil services. This is what makes the completion-only bypass in `cmd/tusk/main.go` safe.
- Harness contract for e2e steps: `tests/e2e/harness.go` lines 194-211 — `Step.Args`, `Step.Assert(t, r)`, and `Step.AssertText(t, output)` are the primitives this phase uses. `runScenarios` (lines 219-262) runs every scenario across `{flag, env} × {text, json}`, and always injects `--format text|json` on every invocation (line 92). Completion output is format-agnostic, so the 4-way matrix runs without special casing.
- Global flag strippers: `cmd/tusk/main.go` — two existing helpers, `stripDBFlag(args []string) []string` and `stripConfigFlag(args []string) []string`, already drop the two global flags that `app.Run` must not see. Reuse both in the completion bypass. Do not duplicate their logic.
- Roadmap bypass constraint: `ROADMAP.md:798` — *"No persistent flag parsing side effects — the completion command runs without touching the workspace database or config resolution"*. This is why Task 2 exists; otherwise `run()` in `cmd/tusk/main.go` opens SQLite and runs migrations before Cobra ever sees `completion`.
- `ValidArgsFunction` audit (confirmed before drafting this plan): zero matches in `internal/tui/` today. Cobra's `__complete` RPC walks the command tree but will not invoke a service-touching handler on any existing command during bypass. Keep this true going forward by leaving services as `nil` in the bypass path.

## Tasks

1. **Add the `completion` Cobra command.**
   - Create `internal/tui/completion.go`.
   - Define `func (a *App) buildCompletionCmd() *cobra.Command`. The parent command uses:
     - `Use: "completion [bash|zsh|fish|powershell]"`
     - `Short: "Generate shell completion scripts"`
     - `Long:` a static block — one short paragraph describing the purpose and noting that scripts are generated on demand, followed by the four generate-and-install snippets that currently live in `PRODUCT.md:248-259`. Keep the snippets identical to `PRODUCT.md` so regeneration guidance stays in one visible place across `--help` and the product doc.
     - `Args: cobra.NoArgs`
     - `ValidArgsFunction: cobra.NoFileCompletions`
     - `RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }` — bare `tusk completion` prints usage with exit 0.
   - Attach four leaf subcommands on the parent. Each leaf:
     - `Args: cobra.NoArgs`
     - `DisableFlagsInUseLine: true`
     - `ValidArgsFunction: cobra.NoFileCompletions`
     - Emits its script to `cmd.OutOrStdout()` via Cobra's built-in generators:
       | `Use`        | Short                                          | Generator call                                         |
       |--------------|------------------------------------------------|--------------------------------------------------------|
       | `bash`       | `Generate the autocompletion script for bash` | `cmd.Root().GenBashCompletionV2(out, true)`            |
       | `zsh`        | `Generate the autocompletion script for zsh`  | `cmd.Root().GenZshCompletion(out)`                     |
       | `fish`       | `Generate the autocompletion script for fish` | `cmd.Root().GenFishCompletion(out, true)`              |
       | `powershell` | `Generate the autocompletion script for powershell` | `cmd.Root().GenPowerShellCompletionWithDesc(out)` |
     - Each `RunE` resolves `out := cmd.OutOrStdout()` at call time (not at command construction) so that tests which redirect the root's output stream see the script.
   - In `internal/tui/app.go`, inside `New(...)` immediately after `a.root` is constructed (the `a.root = &cobra.Command{...}` literal) and before the first `a.root.AddCommand(...)` call, add `a.root.CompletionOptions.DisableDefaultCmd = true`. This suppresses Cobra's auto-registration so the explicit command owns the slot. Anchor the insertion semantically, not by line number — other v0.11 initiatives may have shifted the line count by the time this phase lands.
   - In `internal/tui/app.go`, after the `mcpCmd` registration (`a.root.AddCommand(mcpCmd)`) and before the final `return a`, add `a.root.AddCommand(a.buildCompletionCmd())`. Placing it after `mcpCmd` keeps the help output alphabetized-by-registration-order for the non-entity verbs (`completion`, `config`, `mcp`, `version`). Use the same semantic anchoring as above.
   - The new file and the `app.go` edits together are strictly additive — no existing command, flag, or handler is renamed, moved, or reflagged. Running `go build ./...` and `make test` must pass after this task in isolation.

2. **Bypass config + database initialization for completion-only invocations.**
   - Edit `cmd/tusk/main.go`.
   - Add a new helper at the bottom of the file:
     ```go
     // isCompletionInvocation reports whether args (the slice after the binary
     // name) dispatches to either the human-facing `completion` command or
     // Cobra's hidden `__complete` shell-completion RPC. It walks the args,
     // skipping the five known global flags (and their values) so that
     // `tusk --config foo completion bash` and `tusk --db=/tmp/x __complete task ""`
     // both return true.
     func isCompletionInvocation(args []string) bool { ... }
     ```
     - Recognized global flags to skip: `--config`, `--db`, `--format`, `--player`, `--no-color`. The first three take a value (either in the next slot or after `=`); `--no-color` is boolean; `--player` takes a value. Match both `--flag value` and `--flag=value` shapes for the value-carrying ones.
     - Return `true` if the first non-flag token is `completion` or `__complete`. Return `true` for bare `tusk completion` (no further args) so the help path is bypassed too. Return `false` for every other first token, including empty args.
   - At the very top of `run()` — immediately after the `func run() error {` line and before the first statement in the existing body (the `explicitConfig, err := resolveConfigPath()` call) — add:
     ```go
     if isCompletionInvocation(os.Args[1:]) {
         app := tui.New(
             nil, nil, nil, nil, nil, nil, nil, nil, nil,
             tui.VersionInfo{Version: version, Commit: commit, Date: date},
             config.TUIConfig{}, config.MCPConfig{}, nil,
         )
         return app.Run(stripConfigFlag(stripDBFlag(os.Args[1:])))
     }
     ```
     - This reuses the existing `stripConfigFlag` and `stripDBFlag` helpers already defined later in `cmd/tusk/main.go`, so the completion branch consumes identical arg-sanitization logic to the normal branch. No new imports are needed — `config` and `tui` are already imported at the top of the file.
     - Passing every service as `nil` is safe because `tui.New`'s doc comment (on the constructor in `internal/tui/app.go`) documents nil-tolerance for the service params, and the constructor's only nil-sensitive call — `collectNonTerminalStatuses(nil)` — takes an early-return fallback to `["pending","active"]`.
   - Extend `cmd/tusk/main_test.go` with a table-driven test `TestIsCompletionInvocation` that exercises the helper across these shapes, one row per shape:
     | Input (args slice)                                | Expected |
     |---------------------------------------------------|----------|
     | `[]string{}`                                      | `false`  |
     | `[]string{"completion"}`                          | `true`   |
     | `[]string{"completion", "bash"}`                  | `true`   |
     | `[]string{"__complete", "task", ""}`              | `true`   |
     | `[]string{"--config", "/tmp/x", "completion", "zsh"}` | `true`  |
     | `[]string{"--config=/tmp/x", "completion", "fish"}` | `true`  |
     | `[]string{"--db", "/tmp/y", "completion", "bash"}` | `true`  |
     | `[]string{"--db=/tmp/y", "completion", "bash"}`   | `true`  |
     | `[]string{"--format", "json", "completion", "bash"}` | `true` |
     | `[]string{"--no-color", "completion", "bash"}`    | `true`   |
     | `[]string{"--player", "me", "completion", "bash"}` | `true`  |
     | `[]string{"task", "list"}`                        | `false`  |
     | `[]string{"--config", "/tmp/x", "task", "list"}`  | `false`  |
     | `[]string{"version"}`                             | `false`  |
     | `[]string{"mcp", "serve"}`                        | `false`  |
   - Do not alter any other code path in `main.go`. The normal branch continues to open config, resolve the DB, run migrations, and wire services exactly as today.

3. **Add e2e scenarios for the four shells and the root-command regression.**
   - Create `tests/e2e/completion_test.go`.
   - Declare `func TestCompletion(t *testing.T) { runScenarios(t, binPath, scenarios) }` using the same pattern as the existing test files (see `tests/e2e/task_lifecycle_test.go` for the canonical shape — `binPath` is a package-level var populated by `TestMain`).
   - Scenario 1 — `completion_smoke`:
     - Four `Step` entries, one per shell. Each `Args` is `[]string{"completion", "<shell>"}`.
     - Each `Step.Assert` asserts `r.Err == nil`, `r.Stdout != ""`, and `len(r.Stdout) >= 100` (loose lower bound, not script parsing — just catches the "emitted nothing" regression).
   - Scenario 2 — `completion_lists_root_commands`:
     - Single `Step` with `Args: []string{"completion", "bash"}`.
     - `Step.Assert` iterates a fixed slice of expected top-level command names and fails the test if any name is absent from `r.Stdout`. Membership check uses `strings.Contains(r.Stdout, name)` — add `"strings"` to the test file's import block (the existing e2e files already import it, so this matches convention). Expected set (inline, alphabetized, with a comment block above the literal so any future root refactor forces a sync edit here):
       ```go
       import "strings"
       // ...
       // NOTE: keep this list in sync with the AddCommand calls in
       // internal/tui/app.go (func New) between the task parent and the
       // completion command. A missing entry means a root-level command
       // silently lost its completion coverage.
       rootCmds := []string{
           "completion", "config", "mcp", "player",
           "project", "tag", "task", "version", "workflow",
       }
       for _, name := range rootCmds {
           if !strings.Contains(r.Stdout, name) {
               t.Fatalf("completion bash output missing root command %q", name)
           }
       }
       ```
     - Do **not** include `undo` in `rootCmds` for this phase. The roadmap item `ROADMAP.md:807` lists `undo` as an expected top-level verb, but `undo` is not registered on the root in the current codebase — it belongs to the v0.11 **workspace-wide verbs** initiative, which is independent of this phase. Add a trailing comment on the `rootCmds` literal: `// TODO(v0.11): add "undo" here once the workspace-wide verbs initiative registers it on the root.` The test must track reality, not the aspirational roadmap.
     - Do not assert on bash-specific script syntax beyond the command name matches. The goal is regression coverage for "command still reachable via completion", not "Cobra's bash template renders correctly".
   - Both scenarios run under the standard 4-way matrix (`{flag, env} × {text, json}`) that `runScenarios` applies. Completion output is identical across all four combinations; the redundant runs are cheap and match the harness convention.
   - Use only the existing `harness.go` primitives. Do not extend `Step` or `Env`.

4. **Document the install flow.**
   - `PRODUCT.md` lines 237-260 — the "Shell completion" section already exists with the four commands and install snippets. Cross-check each snippet against the output of `./bin/tusk completion <shell> | head -3` after Task 1 lands:
     - bash: emitted script begins with `# bash completion V2 for tusk`
     - zsh: emitted script begins with `#compdef tusk`
     - fish: emitted script begins with `# fish completion for tusk`
     - powershell: emitted script begins with `# powershell completion for tusk`
   - If any install-path or invocation snippet in `PRODUCT.md:248-259` has drifted from what Cobra actually generates, correct it in-place. Do not rewrite the surrounding prose. If nothing drifts, this task closes with zero doc-file edits.
   - `docs/configuration.md` — append a new top-level section titled `## Shell Completion` at the end of the file. Section contents:
     - One paragraph explaining that tusk generates completion scripts on demand and ships no pre-baked artifacts in the repo or release tarballs; every upgrade requires regenerating.
     - A per-shell install-path table:
       | Shell      | Install path (user scope)                                                    |
       |------------|------------------------------------------------------------------------------|
       | bash       | `~/.local/share/bash-completion/completions/tusk`                            |
       | zsh        | `"${fpath[1]}/_tusk"` — first writable directory in `$fpath`                 |
       | fish       | `~/.config/fish/completions/tusk.fish`                                       |
       | powershell | Append output to your `$PROFILE` (e.g. `tusk completion powershell \| Out-String \| Invoke-Expression`) |
     - The same four generate-and-install one-liners that live in `PRODUCT.md:250-259`, reused verbatim so the two docs do not drift.
     - A sentence pointing readers at `tusk completion --help` for the command reference.
   - Do not create `docs/shell-completion.md` as a separate file. The section fits comfortably inside `docs/configuration.md`; the roadmap explicitly allows either placement at `ROADMAP.md:802`, and inline keeps the docs surface smaller.

## User-Visible Behavior After This Phase

The implementer agent should treat the following as acceptance criteria — every bullet must still pass after the phase is applied:

- `tusk completion bash|zsh|fish|powershell` each print a non-empty shell script to stdout and exit 0.
- `tusk completion` with no subcommand prints the parent command's help and exits 0.
- `tusk completion` and its four leaves are listed under `tusk --help`.
- `tusk completion bash`, invoked against a host with **no** readable config file and **no** writable database path, still succeeds — no file is opened, no migration is run, no error is printed. (Manual verification: run the command with `TUSK_CONFIG=/nonexistent/path.toml TUSK_DB=/nonexistent/dir/tusk.db ./bin/tusk completion bash` and confirm success.)
- `tusk __complete task ""` (Cobra's hidden shell-completion RPC) also runs without opening the database or resolving config — same bypass covers it.
- Every root-level subcommand that exists today — `task`, `project`, `workflow`, `tag`, `player`, `config`, `mcp`, `version`, and the new `completion` — remains reachable and unchanged. No verb is renamed, moved, or reflagged.
- The existing e2e suites (`tests/e2e/*`) still pass unmodified.
- `go build ./...`, `make test`, and `make vet` are clean.

## Changes Introduced

**New files:**
- `internal/tui/completion.go` — houses `buildCompletionCmd()` and the four leaf constructors.
- `tests/e2e/completion_test.go` — scenarios `completion_smoke` and `completion_lists_root_commands`.
- `docs/plans/v0.11-tusk-completion/phase-1.md` — this document (removed at post-implementation review per planning-agent convention).

**Modified files:**
- `internal/tui/app.go` — two additions inside `tui.New(...)`: `a.root.CompletionOptions.DisableDefaultCmd = true` after line 116, and `a.root.AddCommand(a.buildCompletionCmd())` after line 164.
- `cmd/tusk/main.go` — new top-of-`run()` bypass branch plus a new `isCompletionInvocation` helper at the bottom of the file.
- `cmd/tusk/main_test.go` — new `TestIsCompletionInvocation` table-driven test.
- `PRODUCT.md` — lines 237-260 cross-checked against real generator output; corrections in place only if drift is observed.
- `docs/configuration.md` — new trailing `## Shell Completion` section.

**Interfaces modified:** none. All changes are additive.

**New environment variables:** none.

**Schema migrations:** none.

**Added dependencies:** none. Cobra v1.10.2 (already in `go.mod`) supplies all four completion generators.

**Bridge code introduced:** none. This phase has no successor, so there is no later-phase removal to schedule. The explicit `CompletionOptions.DisableDefaultCmd = true` line is permanent configuration, not bridge code; it stays for the life of the binary to keep Cobra's auto-registration from silently shadowing the explicit command in future Cobra upgrades.
