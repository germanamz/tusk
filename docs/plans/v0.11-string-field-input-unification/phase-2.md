# Phase 2 — Consumer rewire + description.go removal

**Initiative:** String Field Input Unification (v0.11)
**Design spec:** `docs/plans/v0.11-string-field-input-unification/design.md`
**Prerequisites:** Phase 1 complete (expander + config in place).

## Inherits From

Phase 1 leaves the repository in this state:

- `internal/tui/expand.go` exists with `expandRefs(raw, stdin, maxSize)` and `(*App).expandRefs(raw, stdin)`. Covered by `internal/tui/expand_test.go`. **Currently unused** by any command.
- `config.Config` has an `Inline InlineConfig` field with `MaxExpansionSize int64`, default `1048576`. The `App` struct already carries the loaded `Config` (search `internal/tui/app.go` for `cfg Config` or equivalent — the existing wiring is untouched).
- `internal/tui/description.go` still exists with `readDescription(value, stdin)`. Still called from `runCreate` at `internal/tui/commands.go:179-190` and from `runModify` at `:451-468`.
- `tusk task create` and `tusk task modify` still expose `--description` / `-d` as a Cobra flag.
- All v0.11 phase-1 tests pass; no behavior has changed for end users yet.

Phase 2 is the breaking-change gate that switches the CLI surface to inline fields and deletes the old flag path.

## Goal

Replace the `--description` Cobra flag with inline `description=...` on create and modify. Add inline `title=...` support on both commands, routed through the expander. Route the positional body of `tusk task annotate` through the expander. Delete `internal/tui/description.go` and its test file. Add e2e coverage for the new surface.

At the end of this phase, every string-valued task property on every task command flows through the shared inline lexer and the `@`-reference expander. The `-d` flag is gone.

## User-visible behaviors that must continue to work after this phase

- `tusk task create "title"` still creates a task with that title.
- `tusk task create "title" description="body"` now works (new behavior replacing `-d "body"`).
- `tusk task create "title" description=@./spec.md` loads the description from a file (replaces `--description @./spec.md`).
- `tusk task create "title" description="see @./notes.md for details"` expands `@./notes.md` mid-string.
- `tusk task create title=@./title.txt description=@./body.md` loads both from files.
- `tusk task create "title with @./file.md reference"` expands the mid-string reference in the free-text title.
- `tusk task modify <id> description="new body"` replaces the description.
- `tusk task modify <id> description=""` clears the description (preserves the existing clear semantics through the `**string` double-pointer path).
- `tusk task modify <id> description=@./new.md` replaces the description from a file.
- `tusk task modify <id> title="new title"` replaces the title.
- `tusk task modify <id> title=@./title.txt` replaces the title from a file.
- `tusk task annotate <id> "body"` still annotates.
- `tusk task annotate <id> @./note.md` loads the annotation body from a file.
- `tusk task annotate <id> @-` loads the annotation body from piped stdin.
- `tusk task create "title" -d "body"` now returns Cobra's standard `unknown shorthand flag: 'd'` error. Same for `--description`.
- All optimistic-locking, tag handling, UDA handling, and other field semantics on create/modify are unchanged.

## Tasks

### Task 1 — Rewire `runCreate` in `internal/tui/commands.go`

**File:** `internal/tui/commands.go` (function `runCreate` starting at line ~160)

Remove the Cobra flag registration. Find the `init()` block or wherever `createCmd.Flags()` is configured (around line 35 based on prior exploration) and delete:

```go
createCmd.Flags().StringP("description", "d", "", `task description (use @file to read from file, @- for stdin)`)
```

Inside `runCreate`:

1. Delete the block at lines ~179-190 that calls `cmd.Flags().Changed("description")` and `readDescription`. It is replaced by field-based reads below.
2. After `fs, parseErrs := filter.Parse(input)` and the title check, resolve stdin once:

   ```go
   var stdinFile *os.File
   if f, ok := cmd.InOrStdin().(*os.File); ok {
       stdinFile = f
   }
   ```

   Reuse the same pattern that `readDescription` used. Do this resolution once at the top of the field-handling block so the expander can be called for title, description, and any future string field in the same invocation without re-resolving.
3. Resolve the title from the field list first, then fall back to free text:

   ```go
   var rawTitle string
   if f, ok := fs.GetField("title"); ok {
       rawTitle = f.Value
   } else {
       rawTitle = fs.Title()
   }
   if rawTitle == "" {
       return fmt.Errorf("title is required")
   }
   expandedTitle, err := a.expandRefs(rawTitle, stdinFile)
   if err != nil {
       return err
   }
   task.Title = expandedTitle
   ```

   The free-text path (`fs.Title()`) running through the expander is what enables `tusk task create "Write spec for @./spec.md"` to work.
4. Resolve the description from the field list:

   ```go
   if f, ok := fs.GetField("description"); ok {
       expandedDesc, err := a.expandRefs(f.Value, stdinFile)
       if err != nil {
           return err
       }
       task.Description = expandedDesc
   }
   ```

   An empty `description=` field on create sets `task.Description = ""`, which is the same as omitting it (the task is created with no description). No special case needed.

**Stdin-once caveat:** if both `title=@-` and `description=@-` appear in one invocation, the second call to `a.expandRefs` would see a stdin that has already been drained. The phase-1 expander's `stdinConsumed` tracking is local to a single `expandRefs` call, so without extra plumbing this case would silently succeed with an empty second value. Phase 2 introduces a small state struct threaded through multiple expander calls in one invocation.

**Refactor the phase-1 expander to accept an optional state struct:**

1. In `internal/tui/expand.go`, add:

   ```go
   type expandState struct {
       stdinConsumed bool
   }

   func expandRefsWithState(raw string, stdin *os.File, maxSize int64, state *expandState) (string, error) {
       // body copied from the phase-1 expandRefs, using state.stdinConsumed
       // in place of the old local bool
   }
   ```

2. Rewrite the phase-1 package-private `expandRefs` to delegate:

   ```go
   func expandRefs(raw string, stdin *os.File, maxSize int64) (string, error) {
       return expandRefsWithState(raw, stdin, maxSize, &expandState{})
   }
   ```

   Phase-1 unit tests are untouched and keep passing — the stdin-once-per-call invariant is preserved because every phase-1 caller allocates a fresh state.

3. Add a new `App` method:

   ```go
   func (a *App) expandRefsWithState(raw string, stdin *os.File, state *expandState) (string, error) {
       return expandRefsWithState(raw, stdin, a.cfg.Inline.MaxExpansionSize, state)
   }
   ```

   The existing `(*App).expandRefs` method from phase 1 stays unchanged — it allocates a fresh state internally and is still the right entry point for single-call consumers like `runAnnotate`.

4. `runCreate` and `runModify` allocate one `expandState` at the top of their field-handling block and pass it into every `expandRefsWithState` call within the invocation. This is what closes the cross-field stdin-once gap.

5. Add a unit test in `expand_test.go` covering the cross-call invariant: one `expandState`, two `expandRefsWithState` calls with the same state, each referencing `@-`, the second returns the `stdin referenced more than once in one invocation` error even though neither raw string contained two `@-` tokens.

### Task 2 — Rewire `runModify` in `internal/tui/commands.go`

**File:** `internal/tui/commands.go` (function `runModify` starting at line ~429)

Remove the Cobra flag registration. Find the init block around line 44 and delete:

```go
modifyCmd.Flags().StringP("description", "d", "", `new description (use @file to read from file, @- for stdin, "" to clear)`)
```

Inside `runModify`:

1. Delete the block at lines ~451-468 that calls `cmd.Flags().Changed("description")` and `readDescription`. Replace with field-based logic below.
2. At the top of the field handling section (after the `current, err := a.taskSvc.GetByShortID` fetch), resolve stdin and allocate one `expandState`:

   ```go
   var stdinFile *os.File
   if f, ok := cmd.InOrStdin().(*os.File); ok {
       stdinFile = f
   }
   state := &expandState{}
   ```

3. Handle `title=...` field and the existing free-text title path together:

   ```go
   if f, ok := fs.GetField("title"); ok {
       expanded, err := a.expandRefsWithState(f.Value, stdinFile, state)
       if err != nil {
           return err
       }
       upd.Title = &expanded
   } else if title := fs.Title(); title != "" {
       expanded, err := a.expandRefsWithState(title, stdinFile, state)
       if err != nil {
           return err
       }
       upd.Title = &expanded
   }
   ```

   Free-text on modify already sets the title in the existing v0.9 code. Wrapping it through the expander preserves that behavior and adds file-reference support.
4. Handle `description=...` field with double-pointer semantics preserved:

   ```go
   if f, ok := fs.GetField("description"); ok {
       if f.Value == "" {
           var nilStr *string
           upd.Description = &nilStr
       } else {
           expanded, err := a.expandRefsWithState(f.Value, stdinFile, state)
           if err != nil {
               return err
           }
           dp := &expanded
           upd.Description = &dp
       }
   }
   ```

   Empty value still clears — matches the current `--description ""` behavior that the e2e tests already lock in. A non-empty value goes through the expander.

### Task 3 — Rewire `runAnnotate` in `internal/tui/commands.go`

**File:** `internal/tui/commands.go` (function `runAnnotate` starting at line 635)

Current body:

```go
body := strings.Join(args[1:], " ")
_, err := a.taskSvc.Annotate(ctx, shortID, body)
```

Replace with:

```go
body := strings.Join(args[1:], " ")

var stdinFile *os.File
if f, ok := cmd.InOrStdin().(*os.File); ok {
    stdinFile = f
}

expanded, err := a.expandRefs(body, stdinFile)
if err != nil {
    return err
}

_, err = a.taskSvc.Annotate(ctx, shortID, expanded)
```

No field parsing on annotate — the positional body is a plain string that runs through the expander directly. A body like `"@./note.md"` or `"see @./context.md for background"` or `"@-"` all work uniformly.

The annotate command has a single expander call, so the plain `(*App).expandRefs` is fine; no `expandState` needed.

### Task 4 — Delete `internal/tui/description.go` and `internal/tui/description_test.go`

**Files:**

- `internal/tui/description.go` — delete entirely.
- `internal/tui/description_test.go` — delete entirely.

After deletion, run `make build` and `make lint`. Every call site for `readDescription` was removed in tasks 1 and 2; the build should succeed. If `grep -rn readDescription internal/` returns anything, the implementer missed a call site — find and remove it before declaring the task done.

### Task 5 — Add e2e coverage

**File:** `tests/e2e/` — locate the existing scenario file that covers `tusk task create` / `tusk task modify` description handling (likely `tests/e2e/description_test.go` or a scenarios file under that directory; the implementer should grep for `-d` and `--description` and `readDescription` in `tests/e2e/` to find the right file).

Rewrite existing scenarios that used `-d` or `--description` to the new inline syntax. Add new scenarios for every case listed in the design spec's "E2E tests" section. The complete list:

1. `tusk task create "title" description=@./spec.md` — whole-value file load. Verify `tusk task get $0.short_id` shows the file content as description.
2. `tusk task create "title" description="see @./notes.md for details"` — mid-string expansion inside a quoted value. Verify description equals `"see <notes content> for details"`.
3. `tusk task create "title" description="email@example.com"` — word-internal `@` preserved. Verify description is exactly `"email@example.com"`.
4. `tusk task create "title" description="@@literal"` — escape produces literal `@literal`.
5. `tusk task modify $0.short_id description=@./new.md` — replace.
6. `tusk task modify $0.short_id description=""` — clear. Verify `tusk task get` shows an empty description.
7. `tusk task modify $0.short_id title=@./title.txt` — title from file.
8. `tusk task create title=@./title.txt description=@./body.md` — both from files, no free-text title.
9. `tusk task annotate $0.short_id @./note.md` — positional file load. Verify the annotation body in `tusk task get`.
10. `tusk task annotate $0.short_id @-` with piped stdin (use the harness's stdin-injection feature if present; if not, use a shell pipe in the scenario runner).
11. **Stale flag contract:** `tusk task create "t" -d "body"` — expect non-zero exit and Cobra's `unknown shorthand flag: 'd'` error on stderr. Same for `--description "body"` → `unknown flag: --description`. This locks in the breaking-change gate.
12. **Binary file rejected:** create a fixture file with a NUL byte in its first 8 KB, reference via `description=@./binary.bin`, expect non-zero exit and an error mentioning `binary file`.
13. **File over size cap:** create a temp config with `inline.max_expansion_size = 100`, create a 200-byte file, reference via `description=@./big.txt`, expect non-zero exit and an error reporting both sizes. This scenario requires the harness to support custom config files — check if the existing harness has a `Config` or `ConfigPath` field on `Scenario`; if not, this scenario can be skipped with a note and moved to a follow-up issue, since unit test 15 in phase 1 already covers the algorithmic behavior.
14. **Quoted path with space:** create a file named `"my file.txt"`, reference via `description=@\"./my file.txt\"` (or use the single-outer / double-inner quoting trick the harness supports). Verify the content loads.

Use the existing harness step-reference syntax (`$0.short_id`) to chain create → modify → get / annotate steps. Every scenario runs in all DB modes and output formats the harness already iterates over.

### Task 6 — Update unit tests broken by the rewire

**File:** `internal/tui/commands_test.go`

Run `make test ./internal/tui/...` after tasks 1-4 and identify failing tests. The likely set:

- Tests that invoke `runCreate` / `runModify` via a Cobra command with `-d` or `--description` flag — rewrite to use inline `description=...` in args.
- Tests that asserted on `readDescription` directly — delete, since `expand_test.go` (phase 1) covers the helper.
- Tests that check free-text title extraction via `fs.Title()` on `create` — verify they still pass once `runCreate` routes title through the expander with no `@` in the input (no-op path).

Do not loosen any assertion that checks description or title content — update the invocation, preserve the expectation.

Also add one new unit test to `expand_test.go` for the `expandRefsWithState` stdin-once invariant introduced in task 1:

- Create an `expandState`, call `expandRefsWithState("@-", pipe, state)` once (piped content `"X"`), then call `expandRefsWithState("@-", pipe, state)` again — expect the second call to return `stdin referenced more than once in one invocation` even though neither individual raw string contained two `@-` tokens.

## Changes Introduced

**New files:** None.

**Modified files:**

- `internal/tui/commands.go` — `runCreate`, `runModify`, `runAnnotate` rewired; `--description` / `-d` flag registration removed from both commands.
- `internal/tui/expand.go` — adds `expandState` struct and `(*App).expandRefsWithState` method alongside the phase-1 `expandRefs`. Phase-1 callers (if any existed — they do not) are unaffected.
- `internal/tui/expand_test.go` — one new test for the cross-call stdin-once invariant.
- `internal/tui/commands_test.go` — invocations updated from flag-based to inline-based, `readDescription` tests removed.
- `tests/e2e/` — rewritten and expanded scenarios per task 5.

**Deleted files:**

- `internal/tui/description.go`
- `internal/tui/description_test.go`

**New interfaces:**

- `type expandState struct { stdinConsumed bool }` — package-private in `internal/tui`.
- `func (a *App) expandRefsWithState(raw string, stdin *os.File, state *expandState) (string, error)`.

**Removed interfaces:**

- `func readDescription(value string, stdin *os.File) (string, error)` — deleted with `description.go`.

**Removed CLI flags:**

- `tusk task create --description / -d`
- `tusk task modify --description / -d`

**New CLI surface:**

- `description=...` inline field on create/modify.
- `title=...` inline field on create/modify.
- `@`-reference expansion on `title`, `description`, and the positional annotate body.

**Schema migrations:** None.

**Environment variables:** None new.

**Dependencies:** None new.

**Bridge code:** None. The v0.11 milestone is pre-release — no backward-compat aliases for removed flags. The unknown-flag error is the intended contract.
