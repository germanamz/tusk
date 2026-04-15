# String Field Input Unification — Design

**Milestone:** v0.11
**Initiative:** String Field Input Unification
**Status:** Draft
**Last updated:** 2026-04-15

## Goal

Move `description` off the bespoke `--description` / `-d` Cobra flag and onto the inline `key=value` syntax every other task property already uses. Apply the same treatment to `title` and the positional body of `tusk task annotate`. Introduce a CLI-layer `@` reference expander so any string-valued field can inline file content (or stdin), anywhere in the value — at the start, in the middle, or multiple times.

This delivers on the v0.11 inline-field principle: entity properties flow through the shared lexer, never through ad-hoc flags, and there is exactly one way to set a field on a task.

## Scope

Ships:

1. A new CLI-layer helper `tui.expandRefs` that scans a string for word-boundary `@` references, reads each file (or stdin for `@-`), validates size and binary status, and returns the substituted string.
2. Removal of the `--description` / `-d` flag from `tusk task create` and `tusk task modify`. Description moves to inline `description=...`.
3. The expander applied to `title=...` on create/modify and to the free-text title on create.
4. The expander applied to the positional body of `tusk task annotate`.
5. A new `[inline]` config section with `max_expansion_size` (int bytes, default 1 MB).
6. Deletion of `internal/tui/description.go` and its tests.
7. Doc sweep (`PRODUCT.md`, `ROADMAP.md`, `docs/configuration.md`) and e2e coverage.

Out of scope — **removed from the original ROADMAP story wording**:

- `ValueModifier` field on `syntax.Token` or `syntax.FieldFilter`. `@` is inline text substitution, not a prefix modifier: the mid-string example `"text @file.txt"` cannot be expressed as a prefix-stripped AST marker, so an AST slot for it has no job to do.
- Value-prefix modifier registry. No registry, no extensibility story for future `?` / `*` markers — those will be designed when a concrete requirement appears, not speculatively.
- Lexer changes. Zero. The v0.9 lexer continues to emit the final string value unchanged; the expander runs on that value purely as a consumer-layer pass.

The ROADMAP story text must be revised as part of this initiative to reflect the scope reduction.

## Semantics

### When `@` triggers expansion

The expander walks the input byte-by-byte and treats `@` as a file reference **only when it is at a word boundary**:

- Start of string, or
- The previous character was ASCII whitespace (space or tab).

`@` in word-internal positions is emitted literally. This means `foo@bar.com`, `"email@example.com"`, and `user@host` never trigger expansion.

### Path delimiter

After a word-boundary `@`, the path is scanned as follows:

- If the next character is `"`, a quoted path is read via the same `scanQuoted` routine the v0.9 lexer uses. `@"./my file.txt"` lets users reference paths containing spaces. Shell invocation: `description='foo @"my file.txt" bar'` (single outer / double inner) or `description="foo @\"my file.txt\" bar"`.
- Otherwise the path runs until the next ASCII whitespace or end-of-string.

### `@@` escape

Two consecutive `@` characters at a word boundary emit a literal `@`. `"@@mention please"` yields the literal string `"@mention please"`. This is the only way to put a literal `@word` at a word boundary inside a string that otherwise participates in expansion.

### Stdin

The path value `-` (bare, unquoted) reads from stdin. `@-` is allowed anywhere the expander runs — at the start of a value, mid-string, or in a positional annotate body. Stdin may only be referenced once per invocation; a second `@-` errors.

### Quoted values are not opaque to `@`

Unlike the v0.9 modifier-tokenization rule (where quoted strings are opaque), the expander runs on the **final decoded string** produced by the lexer. `description="text @./file.txt"` expands `@./file.txt` into file content. Lexer quoting is the escape hatch for shell/lexer syntax (spaces, `=`, parentheses); it is **not** the escape hatch for `@`. The `@@` escape is.

### Binary files

After reading a file, the expander scans the first 8 KB of content for NUL bytes (the strategy git uses for binary detection). If any NUL byte is present, the reference is rejected. Binary attachments are planned for a future release; for now, descriptions, titles, and annotation bodies must be text.

### Size cap

Each individual `@` expansion is capped at `inline.max_expansion_size` bytes (default 1 MB). An over-cap reference is rejected with a clear message that reports the actual size and the limit. The cap is per-reference, not per-invocation — three 900 KB expansions in one description are allowed.

### Error taxonomy

| Condition | Error message |
|---|---|
| Missing file | `@<path>: no such file` |
| Over size cap | `@<path>: file is X MB, exceeds Y MB limit for inline expansion` |
| Binary content | `@<path>: appears to be a binary file; tusk descriptions and annotations must be text. binary file attachments are planned for a future release.` |
| Stdin is a TTY | `@-: stdin is a terminal, not a pipe` |
| Stdin referenced twice | `@-: stdin referenced more than once in one invocation` |
| Bare `@` | `bare @ is not a valid reference` |
| Empty quoted path | `empty path after @` |
| Unclosed quoted path | `unclosed quoted path after @` |

All errors include enough context for the user to locate the bad reference in their input.

## Lexer and AST — unchanged

`syntax.Token.ValueModifier` is not added. `syntax.FieldFilter.ValueModifier` is not added. `syntax.ParseValue` is not added. `syntax/token.go`, `syntax/modifier.go`, and `syntax/parse_fields.go` are not modified. The v0.9 `ModifierSet` continues to register `+` and `-` for token-prefix markers only.

The v0.9 quoted-string opacity rule still applies to modifier tokenization (`title="pending(initial)"` is still a literal string, not a group modifier), because `@` expansion is not modifier tokenization. The two concepts are orthogonal.

## Expander — `internal/tui/expand.go`

Replaces `internal/tui/description.go`.

### Signature

```go
func expandRefs(raw string, stdin *os.File, maxSize int64) (string, error)
```

The helper is pure: no command-specific knowledge, no inspection of inline string syntax beyond the `@`-scanning rule, no global state. Callers pass their own stdin handle so tests can inject `os.Pipe` readers.

### Algorithm

Single forward scan over `raw`:

1. Walk byte-by-byte. Track `atBoundary bool` — true at start, true whenever the previous emitted byte was ASCII whitespace.
2. When `raw[i] == '@'` and `atBoundary`:
   - `raw[i+1] == '@'` → emit literal `@`, advance past both bytes, `atBoundary` becomes false.
   - `raw[i+1] == '"'` → scan a quoted path via `scanQuoted`, resolve the file, substitute content.
   - Otherwise → scan a bare path until next ASCII whitespace or end-of-string, resolve the file, substitute content.
   - Path `-` (bare, unquoted) → read stdin.
3. Any other byte → append to output buffer, update `atBoundary` based on whether the byte is whitespace.

After substitution, the scan resumes at the byte immediately after the consumed reference. Substituted content is **not** re-scanned for `@` references — expansion is one level deep.

### File read

1. `os.Stat(path)`. Missing → error.
2. `info.Size() > maxSize` → error with sizes in message. Avoids reading huge files into memory just to reject them.
3. `os.ReadFile(path)`.
4. NUL-byte scan on `content[:min(8192, len(content))]`. Any NUL → binary error.
5. Append content to output buffer.

### Stdin read

1. `stdin == nil || term.IsTerminal(int(stdin.Fd()))` → TTY error (preserves current `readDescription` behavior).
2. `io.ReadAll(io.LimitReader(stdin, maxSize+1))`.
3. `len(data) > maxSize` → over-cap error.
4. NUL-byte scan on first 8 KB.
5. Append content to output buffer.
6. Mark stdin as consumed on the expander state so a second `@-` in the same invocation errors.

### Relative paths

File paths resolve relative to the caller's working directory (`os.Getwd`), not the config file's directory. This matches how a user types a path on the shell. Absolute paths and `~`-prefixed paths are expanded via `filepath.Abs` / `os.UserHomeDir`.

## Config

### New section

```toml
[inline]
# Maximum byte size of a single @file expansion on inline string fields.
# Applied per reference, not per invocation.
max_expansion_size = 1048576
```

### Go type

```go
type InlineConfig struct {
    MaxExpansionSize int64 `mapstructure:"max_expansion_size"`
}
```

Added to `Config` as `Inline InlineConfig`. Default `1048576` (1 MB).

### Validation

`config.Load` rejects `max_expansion_size <= 0` with a clear error. No upper bound is enforced — a user who raises the cap to 100 MB knows what they are doing.

### Plumbing

The `App` struct in `internal/tui/app.go` already holds the loaded `Config`. `expandRefs` is invoked as `a.expandRefs(raw, stdin)` via a thin method wrapper that reads `a.cfg.Inline.MaxExpansionSize` from the stored config.

## Command wiring

### `tusk task create` (`runCreate`)

1. Remove `createCmd.Flags().StringP("description", "d", ...)`.
2. After `filter.Parse(input)`:
   - If `title=...` appears as a field, use its value. Else use `fs.Title()` (free text).
   - Both paths run through `a.expandRefs(...)` before assignment.
   - If `description=...` appears as a field, run its value through `a.expandRefs(...)` and assign.
3. Free-text title (`tusk task create "Write spec for @./spec.md"`) expands — the expander runs on the joined free-text string too.

### `tusk task modify` (`runModify`)

1. Remove the Cobra `--description` flag.
2. Pattern-match `description=...` in the field list:
   - Present with empty value → clear (`**string` outer-non-nil / inner-nil).
   - Present with non-empty value → run through `expandRefs`, set via outer-non-nil inner-non-nil pointer.
   - Absent → no change (outer-nil).
3. Same treatment for `title=...`.
4. Free-text title (`fs.Title()`) also runs through the expander, preserving the v0.9 behavior that free text on `modify` sets the title.

### `tusk task annotate` (`runAnnotate`)

1. `body := strings.Join(args[1:], " ")` stays.
2. `body, err := a.expandRefs(body, stdinFile)` runs before `a.taskSvc.Annotate(ctx, shortID, body)`.
3. Literal `@` at a word boundary in a positional body → user writes `@@` (escape) or quotes at shell level.

### Conflict resolution

If `title=...` field is set AND free text is also present on `create`, the field wins. This is the v0.9 behavior; unchanged here.

Bare `key=value` that is not a registered top-level field still errors with "unknown field". The v0.11 inline-field principle is unchanged here — this initiative only touches how `description` / `title` / annotation bodies are resolved, not which fields are accepted.

## MCP parity

MCP tool schemas for `tusk_task_create`, `tusk_task_modify`, and `tusk_task_annotate` accept `title`, `description`, and `body` as structured JSON strings. Agents pass content directly; no `@` expansion is wired into the MCP layer. Attempting to pass `@./foo.txt` via MCP would store the literal string `@./foo.txt`, which is the correct behavior — agents don't have a filesystem context the server can meaningfully resolve against.

Verification pass: grep `internal/mcp/` for any accidental `expandRefs` import or `@` prefix handling. The grep should return nothing.

## Testing

### Unit tests (`internal/tui/expand_test.go`)

- Whole-value: `@./foo.txt` → file content.
- Mid-string: `"prefix @./foo.txt suffix"` → `"prefix <content> suffix"`.
- Multiple refs: `"a @./one.txt b @./two.txt"` — both resolved.
- Quoted path: `@"./my file.txt"` — space-containing path resolved.
- `@@` escape at word boundary → literal `@`.
- Word-internal `@`: `foo@bar.com` → unchanged.
- Empty `@`: `"foo @ bar"` → error.
- Empty quoted path: `@""` → error.
- Unclosed quoted path: `@"./baz` → error.
- Missing file → error message contains the path.
- Binary file (NUL byte in first 8 KB) → error message mentions attachments.
- Size cap exceeded → error message reports actual size and limit.
- `@-` stdin happy path (via `os.Pipe`).
- `@-` with TTY stdin → error.
- `@-` referenced twice → error.
- Substituted content is not re-scanned: a file whose body contains `@./other.txt` does not trigger a second expansion.
- Relative path resolution against CWD.

### E2E tests (`tests/e2e/`)

- `tusk task create "title" description=@./spec.md` — whole-value load.
- `tusk task create "title" description="see @./notes.md for details"` — mid-string expansion inside a quoted value.
- `tusk task create "title" description="email@example.com"` — word-internal `@` preserved.
- `tusk task create "title" description="@@literal"` — escape produces literal `@literal`.
- `tusk task modify <id> description=@./new.md` — replace.
- `tusk task modify <id> description=""` — clear.
- `tusk task modify <id> title=@./title.txt` — title from file.
- `tusk task annotate <id> @./note.md` — positional file load.
- `tusk task annotate <id> @-` with piped stdin.
- Stale flag invocation `tusk task create "t" -d "body"` → Cobra's "unknown shorthand flag" error (locks in the removal contract alongside the v0.11 grouping initiative's unknown-command hint table).
- Binary file rejected with attachments-hint error.
- File over size cap rejected with size reported.
- Quoted-path expansion for a file whose name contains a space.

## Documentation changes

- `PRODUCT.md` — rewrite the "Inline Syntax" section's file-reference paragraph. Current v0.11 wording describes `@` as a "value-prefix modifier"; rewrite to describe it as a word-boundary inline reference with quoted-path support, `@@` escape, NUL-byte binary guard, and the configurable per-reference size limit. Remove any implication that `@` lives in the lexer's modifier registry.
- `ROADMAP.md` — edit the initiative's Story 1 ("Value-position modifiers in the lexer") to reflect the scope reduction: drop the AST and lexer changes; retain only the expander specification. Story 2 (`expandValueRef` → `expandRefs`) keeps its structure but gains the binary/size-limit requirements and the mid-string expansion rule.
- `docs/configuration.md` — add a new `[inline]` subsection documenting `max_expansion_size`.
- No `docs/releases/v0.11.md` or `docs/status/v0.11-status.md` updates — those land at milestone completion, not per-initiative.

## Risks and open questions

- **Re-scanning substituted content.** The design deliberately does **not** re-scan file contents for nested `@` references. A file that starts with `@./other.txt` is inserted literally. This prevents recursion bombs and keeps the mental model one-level. If a user genuinely wants nested includes, they can layer files themselves.
- **CWD-relative paths and workspace config directories.** The expander resolves paths against the caller's CWD, not the active config file's directory. For users running `tusk` deep inside a workspace where `tusk.toml` is several directories up, `@./spec.md` reads from their CWD, not from the workspace root. This matches shell intuition and avoids the surprise of "the same command reads a different file depending on the workspace root". Documented in `PRODUCT.md`.
- **Size cap default.** 1 MB is roomy for any sane description. A user with unusual needs can raise it via config. If the default turns out to bite real users, it is trivially adjustable without a schema change.
- **Binary detection heuristic.** NUL-byte scan on the first 8 KB is git's approach and is the right balance of simplicity and accuracy for this use case. It misses non-UTF-8 text encodings (Latin-1, Shift-JIS), which are rare in task descriptions and will still be accepted as text — the right failure mode.

## Implementation sequence

Rough order for the phase plan (phase plan to be drafted separately):

1. Add `[inline]` config section with `max_expansion_size`, wire into `App`.
2. Write `internal/tui/expand.go` and its unit tests. Delete `description.go`.
3. Rewire `runCreate`, `runModify`, `runAnnotate` to use `expandRefs`. Remove `--description` flag.
4. MCP grep verification.
5. E2E coverage.
6. Doc sweep (`PRODUCT.md`, `ROADMAP.md`, `docs/configuration.md`).

Each step is independently reviewable; step 3 is the breaking-change gate.
