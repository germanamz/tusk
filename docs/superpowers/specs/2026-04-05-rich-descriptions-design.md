# Rich Descriptions — Design Spec

**Initiative:** v0.5 Rich Descriptions (from ROADMAP.md)
**Date:** 2026-04-05

---

## Goal

Enable rich task descriptions via CLI and MCP. Descriptions support full markdown content (rendered as raw text in v0.5; terminal-rendered markdown deferred to v0.6 TUI Polish). File-based input lets users write descriptions in their editor and pass them in.

---

## Current State

- **Domain:** `Task.Description` is a plain `string` field. `TaskUpdate.Description` is `*string` (single pointer — cannot clear).
- **SQLite:** `description TEXT NOT NULL DEFAULT ''`. No changes needed.
- **CLI:** `tusk add` and `tusk modify` have **no way to set or update descriptions**. `tusk info` renders descriptions as a single-line key-value pair.
- **MCP:** `tusk_task_create` and `tusk_task_modify` already accept an optional `description` string parameter. No `@file` syntax — MCP receives content directly.

---

## Design

### 1. CLI Input — `--description` flag

**`tusk add`:**
- Add `--description` / `-d` flag (string type).
- If value starts with `@`, treat the remainder as a file path and read its contents.
- `@-` reads from stdin (enables piping: `cat spec.md | tusk add "My task" -d @-`). If stdin is a TTY (no piped input), return an error: `stdin is a terminal, not a pipe`. TTY detection uses `golang.org/x/term.IsTerminal`. Note: when bubbletea is introduced in v0.6, the TTY detection strategy may need to change since bubbletea manages its own terminal state.
- Omitting the flag or passing empty string means no description.

**`tusk modify`:**
- Same `--description` / `-d` flag.
- Empty string `""` explicitly clears the description.
- `@file` and `@-` work the same as `add`.

**File reading:**
- Resolve `@path` relative to CWD.
- If file doesn't exist, return a clear error: `failed to read description file: <path>: no such file or directory`.
- No size limit enforced.

### 2. Domain — Double-Pointer Description

Change `TaskUpdate.Description` from `*string` to `**string` to match the double-pointer pattern used by `ParentID`, `DueAt`, `WaitUntil`, and `RecurrenceRule`.

Semantics:
- `nil` — don't change the description.
- `*nil` — clear the description (set to `""`).
- `*"value"` — set to value.

This fixes a pre-existing asymmetry where descriptions could be set but never cleared.

### 3. Service Layer

Update `TaskService.Update` description patching logic:
- `nil` → skip (no change).
- `*nil` → set `task.Description = ""`.
- `*"value"` → set `task.Description = value`.

No validation added. Descriptions are free-form content — no length limits, no content restrictions. Consistent with how annotations work.

### 4. MCP Tools

No schema changes. `tusk_task_create` and `tusk_task_modify` already accept description as an optional string parameter. MCP receives content directly (no `@file` syntax).

Update the `tusk_task_modify` handler to support the double-pointer clearing semantics: passing an empty string clears the description.

### 5. SQLite Layer

No changes. `description TEXT NOT NULL DEFAULT ''` already handles empty and non-empty strings.

### 6. `tusk info` Display

```
Description:

line 1 of the description
line 2 of the description
...
```

Always block format with a blank line after `Description:`. No indentation, no truncation. Raw text output (no markdown rendering — deferred to v0.6).

**JSON output:** No changes. Description is already included as a plain string field in both TUI JSON and MCP JSON responses.

---

## Implementation Phases

### Phase 1: Double-Pointer Migration (backend-only)

Domain, service, and MCP changes to support clearing descriptions. No CLI changes. All existing behavior preserved. MCP gains description-clearing ability.

**Files:** `internal/domain/task.go`, `internal/service/task.go`, `internal/mcp/tools.go`, service unit tests.

**Plan doc:** `docs/superpowers/plans/2026-04-05-rich-descriptions-phase1.md`

### Phase 2: CLI Description Input & Display

`--description` flag on `add`/`modify`, `@file`/`@-` reading utility, updated `tusk info` rendering, E2E tests.

**Files:** `internal/tui/commands.go`, `internal/tui/render.go`, E2E tests.

**Plan doc:** `docs/superpowers/plans/2026-04-05-rich-descriptions-phase2.md`

---

## Files to Change

| File | Change |
|------|--------|
| `internal/domain/task.go` | `TaskUpdate.Description`: `*string` → `**string` |
| `internal/service/task.go` | Update description patching for double-pointer semantics |
| `internal/tui/commands.go` | Add `--description` / `-d` flag to `add` and `modify` commands; implement `@file` and `@-` reading |
| `internal/tui/render.go` | Multi-line description block rendering in `tusk info` text output |
| `internal/mcp/tools.go` | Update `handleTaskModify` for double-pointer clearing semantics |
| `tests/e2e/` | E2E scenarios for description input, display, and clearing |

## Testing

**Unit tests:**
- `@file` and `@-` file reading utility — happy path, missing file, stdin.
- `TaskService.Update` with double-pointer description — nil (no change), `*nil` (clear), `*"value"` (set).

**E2E tests:**
- `tusk add "Task" --description "inline desc"` → `tusk info` shows it.
- `tusk add "Task" --description @file.md` → create temp file, verify description matches file contents.
- `tusk modify <id> --description "updated"` → verify changed.
- `tusk modify <id> --description ""` → verify cleared.
- Multi-line description rendering in `tusk info` (text output).
- JSON output includes description correctly.

**MCP:** Add test for clearing description via empty string if not already covered.

---

## Out of Scope

- Terminal-rendered markdown (glamour) — deferred to v0.6 TUI Polish.
- `description:` filter field syntax — blocked on quoted string lexer support.
- Description length validation or content restrictions.
