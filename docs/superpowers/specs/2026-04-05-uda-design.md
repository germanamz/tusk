# User-Defined Attributes (UDA) — Design Spec

**Date:** 2026-04-05
**Roadmap:** v0.5 — Rich Content > Initiative: User-Defined Attributes
**Status:** Approved

---

## Overview

Expose the existing `Task.UDA` (`map[string]any`) field and `tasks.uda` JSON column through CLI and MCP surfaces, with filter support for querying by UDA key-value pairs.

The domain type, database column, repository CRUD, and service-layer wiring already exist and are tested. This spec covers the missing user-facing surface: CLI flags, MCP parameters, text rendering, and filter syntax.

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Flag syntax | `--uda key=value` (repeatable) | Simple, avoids comma-in-value ambiguity |
| Value types | All strings | Avoids type coercion confusion; typed UDAs deferred to v0.9 UDA Schema Validation |
| Modify semantics | Merge (not replace) | Prevents accidental data loss; service merges delta onto existing map |
| Clear a key | `--uda key=` (empty value) | Mirrors `--description ""` and `due:` clear conventions |
| MCP parameter shape | `"uda": {"key": "value"}` object | Natural for agents, maps directly to domain type |
| Display format | Section block with sorted keys | Readable for long/multi-line values |
| Filter syntax | `uda.key:value` and `uda.key:` (absent/empty) | Consistent with existing `key:value` filter convention |
| Wildcards/patterns | Deferred to v0.6 Advanced Filters | YAGNI — simple equality covers current needs |

---

## Story 1: UDA CLI & MCP Surface

### Domain Layer

Add shared validation helpers to `internal/domain/task.go`:

- `ValidateUDAKey(key string) error` — validates against `^[a-zA-Z_][a-zA-Z0-9_-]*$`. Rejects empty keys, keys starting with numbers, and keys containing special characters (`.`, `$`, `[` — important for Story 2's `json_extract` safety).
- `ValidateUDA(uda map[string]any) error` — validates all keys via `ValidateUDAKey` and all values are strings. Returns descriptive errors naming the offending key/value.

Both CLI and MCP call the same validation functions.

### Service Layer

Modify `TaskService.Update` in `internal/service/task.go` to support merge semantics:

When `upd.UDA != nil`:
1. Start from the current `task.UDA` map (already fetched for optimistic locking)
2. For each key in `*upd.UDA`:
   - Empty string value → delete the key from the map
   - Non-empty string value → set/overwrite the key
3. Set `task.UDA` to the merged result
4. The repository writes the full map as before (whole-map replace at the storage level)

Validate UDA via `ValidateUDA` before merging — reject invalid keys or non-string values with an appropriate error.

### CLI: Flag Definition & Parsing

**Files:** `internal/tui/commands.go`

Add `--uda` as a `StringArrayP` flag on both `add` and `modify` commands.

**`parseUDAFlags(values []string) (map[string]any, error)` helper:**
- Split each value on the first `=`
- No `=` found → error: `invalid UDA format "<value>", expected key=value`
- Empty key (before `=`) → error: `empty UDA key`
- Validate key via `domain.ValidateUDAKey` → propagate error
- Duplicate keys → last one wins
- Return `map[string]any` with string values

**`tusk add`:** If `--uda` flags present, parse via `parseUDAFlags`, set `task.UDA` before calling `taskSvc.Create`.

**`tusk modify`:** If `--uda` flags present, parse via `parseUDAFlags`, set `upd.UDA = &udaMap`. Service-layer merge handles the rest. Empty values (`--uda env=`) become delete signals during merge.

### CLI: Display in `tusk info`

**File:** `internal/tui/render.go`

Add a UDA section to `renderTaskInfo` after existing fields, rendered only when `len(task.UDA) > 0`:

```
UDA:
  env:    prod
  context:
    This is a longer value
    that spans multiple lines
  team:   backend
```

Rules:
- Keys sorted alphabetically for stable output
- Single-line values: inline after key with aligned padding
- Multi-line values (or very long values): key on its own line, value indented on subsequent lines

**JSON output:** Already works — `taskJSON.UDA` is populated via `toTaskJSON`. No change needed.

### MCP: Parameter Registration & Handling

**Files:** `internal/mcp/server.go`, `internal/mcp/tools.go`

**`tusk_task_create`:** Add optional `uda` object parameter. Handler extracts `map[string]any` from request arguments, validates via `domain.ValidateUDA`, converts to `map[string]any` with string values, sets `task.UDA`.

**`tusk_task_modify`:** Same `uda` object parameter. Merge semantics — partial object overwrites/adds keys, empty string values remove keys. Sets `upd.UDA = &udaMap`.

**MCP validation (strict, before touching service):**
- `uda` value must be an object → error: `"uda" must be an object`
- Each key validated via `domain.ValidateUDAKey` → error: `invalid UDA key "<key>"`
- Each value must be a string → error: `UDA value for "<key>" must be a string`
- Empty object `{}` is valid (no-op)

**`taskResponse`:** Add `UDA map[string]any \`json:"uda,omitempty"\`` to the struct. Populate in `toTaskResponse` from `task.UDA`. This fixes the existing gap where UDA data was silently dropped from all MCP responses.

### Files Touched

- `internal/domain/task.go` — `ValidateUDAKey`, `ValidateUDA`
- `internal/service/task.go` — merge logic in `Update`
- `internal/tui/commands.go` — `--uda` flag on add/modify, `parseUDAFlags`
- `internal/tui/render.go` — UDA section in `renderTaskInfo`
- `internal/mcp/tools.go` — `taskResponse.UDA`, handler changes for create/modify
- `internal/mcp/server.go` — `uda` parameter registration on create/modify tools
- `tests/e2e/` — new scenarios

### Tests

**Unit:**
- `ValidateUDAKey` — valid keys, invalid keys (special chars, empty, starts with number, contains `.`)
- `ValidateUDA` — valid map, non-string values rejected, invalid keys rejected
- Service merge — add keys, overwrite keys, delete keys (empty value), merge with existing UDA, merge with nil existing UDA

**E2E:**
- Create task with `--uda env=prod --uda team=backend`, verify `tusk info` displays both
- Modify task to add a UDA key, verify merge (existing keys preserved)
- Modify task to clear a UDA key (`--uda env=`), verify removal
- Modify task to overwrite a UDA key, verify new value
- JSON output includes UDA in `tusk info` and `tusk list`

---

## Story 2: UDA Filter Support

### Filter Syntax

`uda.key:value` — match tasks where UDA key equals value.
`uda.key:` — match tasks where UDA key is absent or empty string.

Multiple `uda.*` filters are AND'd — all must match.

### Lexer

**File:** `internal/filter/token.go`

No changes needed. `uda.key:value` already tokenizes as `TokenField` since it contains a colon with a non-empty left side. The `.` in `uda.key` is not special to the lexer.

### Parser

**File:** `internal/filter/parser.go`

Add a prefix check before the "unknown field" error path. After `strings.Cut(tok.Value, ":")` produces `key` and `value`:

- If `key` starts with `uda.`, extract the UDA key name (strip `uda.` prefix)
- Validate the UDA key via `domain.ValidateUDAKey`
- Empty UDA key name (bare `uda.:value`) → parse error
- Value can be empty (for absent/empty matching) or any string
- Append as `FieldFilter{Key: key, Value: value}` where key retains the `uda.` prefix

### Resolver

**File:** `internal/filter/resolve.go`

Add a `strings.HasPrefix(field.Key, "uda.")` case in the field switch:

- Extract key name via `strings.TrimPrefix(field.Key, "uda.")`
- Lazy-init `tf.UDA` map if nil
- Set `tf.UDA[udaKey] = field.Value`

### Domain

**File:** `internal/domain/filter.go`

Add to `TaskFilter`:

```go
UDA map[string]string // filter by UDA key=value pairs (AND semantics)
```

### SQLite

**File:** `internal/sqlite/task.go`

In `buildFilter`, for each entry in `filter.UDA`:

- Non-empty value: `json_extract(uda, ?) = ?` with args `"$."+k` and `v`
- Empty value (absent/empty match): `(json_extract(uda, ?) IS NULL OR json_extract(uda, ?) = '')` with args `"$."+k` twice

SQLite's `json_extract` is available by default (compiled in since 3.9.0). Since all UDA values are strings, string-to-string comparison works correctly.

**Performance:** `json_extract` on a TEXT column without an index is a full table scan per condition. Acceptable for expected data sizes. Indexing deferred — premature optimization.

### Files Touched

- `internal/domain/filter.go` — `TaskFilter.UDA`
- `internal/filter/parser.go` — `uda.*` prefix handling
- `internal/filter/resolve.go` — resolver case
- `internal/sqlite/task.go` — `buildFilter` UDA conditions
- `tests/e2e/` — filter scenarios

### Tests

**Unit:**
- Parser accepts `uda.key:value` and `uda.key:` (empty value)
- Parser rejects `uda.:value` (empty key) and `uda.inv@lid:value` (bad key chars)
- Resolver maps `uda.key:value` to `TaskFilter.UDA`
- `buildFilter` generates correct `json_extract` SQL for both value match and absent/empty match

**E2E:**
- `tusk list uda.env:prod` returns matching tasks only
- `tusk list uda.env:prod uda.team:backend` (AND) filters correctly
- `tusk list uda.env:` returns tasks where env is absent or empty
- Filter with non-existent UDA key returns no results
