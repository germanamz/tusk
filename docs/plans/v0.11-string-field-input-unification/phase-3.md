# Phase 3 — Documentation sweep + MCP verification

**Initiative:** String Field Input Unification (v0.11)
**Design spec:** `docs/plans/v0.11-string-field-input-unification/design.md`
**Prerequisites:** Phase 2 complete (CLI surface fully rewired, description.go deleted, e2e scenarios green).

## Inherits From

Phase 2 leaves the repository in this state:

- `--description` / `-d` flag is gone from `tusk task create` and `tusk task modify`.
- `description=...` and `title=...` inline fields work on create and modify, with `@`-reference expansion (whole-value, mid-string, quoted paths, `@@` escape).
- `tusk task annotate <id> <body>` routes the positional body through the expander.
- `internal/tui/description.go` and its test file are deleted.
- `internal/tui/expand.go` exposes `expandRefs`, `expandRefsWithState`, and `expandState`.
- `config.Inline.MaxExpansionSize` is wired with a 1 MB default.
- E2E coverage includes the full happy-path and error-path matrix from the design spec.

Phase 3 is documentation-and-verification only. No behavior change, no code rewire. It is the last gate before the initiative can be ticked in ROADMAP.md.

## Goal

Bring every user-facing doc in lockstep with the implementation, revise the ROADMAP story text to match the scope reduction that landed (the `ValueModifier` AST path was dropped in favor of a pure text expander), and verify the MCP layer did not accidentally grow a `@` handler during the implementation.

## User-visible behaviors that must continue to work after this phase

Exactly the set from phase 2 — this phase does not modify any code paths that affect runtime behavior.

- `make build`, `make test`, `make lint` all still pass.
- The docs describe the actual shipped behavior, with no stale references to `--description` / `-d`, the ROADMAP story wording about "value-prefix modifiers in the lexer", or the `expandValueRef` helper name from the original story text.

## Tasks

### Task 1 — MCP verification pass

**Search targets:** `internal/mcp/`

Run these greps and confirm each returns no hits (or only hits that predate this initiative and are unrelated):

```bash
grep -rn "expandRefs\|expandRefsWithState\|expandState" internal/mcp/
grep -rn "readDescription" internal/mcp/
grep -rn "@\"" internal/mcp/
grep -rn "@-" internal/mcp/
```

If any hit is found, the MCP layer has accidentally grown `@`-reference handling, which is wrong — MCP tools receive `title`, `description`, and `body` as structured JSON fields from agents, and agents pass content directly. The fix is to remove the handling so the MCP surface stays a pure pass-through.

Also open `internal/mcp/` tool definitions for `tusk_task_create`, `tusk_task_modify`, and `tusk_task_annotate` (search for `tusk_task_create` as a string literal to find the registration site) and confirm:

- The tool schemas list `title`, `description`, and `body` as plain `string` parameters.
- The handlers call `TaskService.Create` / `Update` / `Annotate` directly with the raw JSON-supplied strings, no pre-processing.

Document the verification result as a short comment in the phase-3 completion report — one line per grep, confirming "no hits" or listing any hits found and explaining why they are unrelated. The planning agent reads this during the continuity review.

### Task 2 — Rewrite the ROADMAP initiative story text

**File:** `ROADMAP.md` — the "Initiative: String Field Input Unification" section under v0.11 (starts at line ~810 based on the current file).

Rewrite **Story 1** ("Value-position modifiers in the lexer") to reflect the scope reduction. The new story title and body should read along these lines (adjust to match the surrounding bullet-list style):

> - [x] **Story: Word-boundary `@` reference expansion**
>   - [x] Add a CLI-layer expander `internal/tui.expandRefs(raw, stdin, maxSize)` that scans a string for word-boundary `@` references and substitutes file content (or stdin for `@-`) inline
>   - [x] Word boundary means start-of-string or preceded by ASCII whitespace — `foo@bar.com` and `user@host` are never expanded
>   - [x] Bare path scans until next whitespace; quoted path `@"./name with space.txt"` scans a quoted span for paths containing spaces
>   - [x] `@@` at a word boundary escapes to a literal `@`
>   - [x] `@-` reads stdin; stdin may only be referenced once per invocation (enforced across multiple `expandRefsWithState` calls in one command via a shared state struct)
>   - [x] Substituted content is **not** re-scanned for nested references — expansion is one level deep
>   - [x] No AST or lexer changes — the expander runs on the final decoded string value from the v0.9 lexer, after quotes have already collapsed. Quoted lexer values are **not** opaque to `@` expansion; lexer quoting escapes shell/lexer syntax, `@@` escapes the expander.

Rewrite **Story 2** ("I/O consumer helper `expandValueRef`") to:

> - [x] **Story: Expander file-read and stdin semantics**
>   - [x] File paths resolve via `os.ReadFile` against the caller's CWD; `~/` prefix expands via `os.UserHomeDir`; absolute paths pass through
>   - [x] Missing file → `@<path>: no such file` error
>   - [x] Binary detection via NUL-byte scan on the first 8 KB of content (git's approach); binary files rejected with an error pointing at future attachment support
>   - [x] Per-reference size cap configured via `inline.max_expansion_size` (default 1 MB); over-cap files rejected with actual size and limit in the error message
>   - [x] Stdin TTY guard preserved from the old `readDescription` helper
>   - [x] Replaces `internal/tui/description.go` entirely — the old helper and its tests are deleted

Story 3 ("Drop `--description` flag") and Story 4 ("Positional bodies gain `@` expansion") keep their current structure; just tick their boxes and clean up any sub-bullets that referenced the AST modifier approach (e.g., any bullet that says "pattern-match on `ValueModifier`" — rewrite to "read the field value from `FilterSet.GetField` and pass it through the expander").

Story 5 ("MCP field parity check") gains its completion tick and the verification result from task 1 above.

Tick the top-level `- [ ]` initiative checkbox to `- [x]` once every story is ticked.

Also add a one-line note at the very top of the initiative (before the first story) that reads:

> **Scope note:** The original story set included `syntax.ValueModifier` AST changes and a value-prefix modifier registry. These were dropped in design — `@` is inline text substitution, not a prefix marker, so the mid-string case (`"text @file.txt"`) cannot be represented as a stripped AST marker. The shipped implementation is a pure consumer-layer text pass with no lexer or AST changes. See `docs/plans/v0.11-string-field-input-unification/design.md` for the full reasoning.

### Task 3 — Rewrite the PRODUCT.md inline-syntax file-reference paragraph

**File:** `PRODUCT.md`

Locate the "Inline Syntax" section under the "Filtering" heading. The current v0.11 wording describes `@` as a "value-prefix modifier" registered in the lexer's value-prefix modifier registry. Rewrite the `@` paragraph to match the shipped design:

- `@` is an inline reference expander, not a lexer modifier.
- It triggers only at word boundaries (start-of-string or after whitespace).
- Bare path scans until next whitespace; quoted path `@"./name with spaces.txt"` supports paths containing spaces.
- `@@` escapes to a literal `@`.
- `@-` reads stdin once per invocation.
- Mid-string expansion: `description="see @./notes.md for details"` works.
- Quoted values are **not** opaque to `@` — lexer quoting handles shell syntax, `@@` handles literal `@` in content.
- Per-reference size cap configurable via `inline.max_expansion_size`.
- Binary files rejected with a forward-pointing note about attachments.

Remove the bullet in the Inline Syntax section that describes value-prefix modifier registry extensibility (`?`, `*`, etc.) — that concept is no longer part of the shipped design. If a future initiative needs a value-prefix registry, it will be added then with concrete requirements.

Update the CLI usage examples in the earlier "CLI" section. The current examples show:

```bash
tusk task modify a3f8b2c1 description=@./spec.md       # load from file
cat spec.md | tusk task modify a3f8b2c1 description=@-  # load from stdin
tusk task annotate a3f8b2c1 @./investigation.md        # annotate from file
```

Add two more examples right after that block to lock in the new capabilities:

```bash
tusk task modify a3f8b2c1 description="see @./notes.md for background"  # mid-string
tusk task modify a3f8b2c1 description="@@literal-at-sign in body"       # escape
```

### Task 4 — Add `[inline]` section to docs/configuration.md

**File:** `docs/configuration.md`

Find the section that documents the TOML config schema and add a new subsection describing `[inline]`. Match the style of the existing `[storage]`, `[urgency]`, `[mcp]` sections. Content:

- Section name: `[inline]`
- One-paragraph intro explaining that the section configures behavior of the inline syntax expander used across task-scoped CLI commands.
- Document `max_expansion_size` (int, bytes, default `1048576`, applied per `@` reference not per invocation).
- Document the `TUSK_INLINE_MAX_EXPANSION_SIZE` env var override that Viper provides automatically via the `TUSK_` prefix.
- One worked example showing a user raising the cap to 5 MB.
- A note that oversized references are rejected with the actual size and limit in the error message, and that binary files are rejected separately via a NUL-byte scan on the first 8 KB.

Cross-reference PRODUCT.md's Inline Syntax section as the authoritative description of when `@` triggers.

### Task 5 — Smoke-verify the updated docs

Run `make build` and then exercise each doc example by hand from a scratch workspace:

```bash
tusk task create "design notes" description=@./docs/plans/v0.11-string-field-input-unification/design.md
tusk task get $SHORT_ID  # verify description shows the loaded content
tusk task modify $SHORT_ID description="see @./README.md for overview"  # mid-string
tusk task modify $SHORT_ID description="@@literal-at-sign"  # escape
tusk task annotate $SHORT_ID @-  # verify piped stdin path
```

Any example that does not work is either a broken doc or a regression in phase 2 — in the latter case, return the phase to phase 2 for rework rather than patching the doc.

## Changes Introduced

**New files:** None.

**Modified files:**

- `ROADMAP.md` — v0.11 initiative story text rewritten, checkboxes ticked, scope note added.
- `PRODUCT.md` — Inline Syntax `@` paragraph rewritten; two new CLI usage examples added.
- `docs/configuration.md` — new `[inline]` subsection documenting `max_expansion_size`.

**Deleted files:** None.

**New interfaces:** None.

**Removed interfaces:** None.

**Schema migrations:** None.

**Environment variables:** None new. `TUSK_INLINE_MAX_EXPANSION_SIZE` is newly documented but has existed implicitly since phase 1 (Viper auto-binds `TUSK_`-prefixed env vars to config keys).

**Dependencies:** None new.

**Bridge code:** None.

**Release/status docs:** Do **not** create `docs/releases/v0.11.md` or `docs/status/v0.11-status.md` in this phase. Per project convention those land only at milestone completion, not per-initiative, even if a phase plan suggests otherwise.
