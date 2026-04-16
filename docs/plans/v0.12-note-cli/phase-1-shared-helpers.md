# Phase 1: Shared Helpers

**Initiative:** Note CLI (v0.12)
**Prerequisites:** None — builds on the base codebase.
**Design spec:** `docs/plans/v0.12-note-cli/design.md`

---

## Goal

Add two additive helpers that later phases depend on. Nothing user-visible ships in this phase; both helpers are pure additions behind package boundaries. The code compiles and all prior tests still pass.

1. `filter.ParseRelativeDuration` — parses `7d`, `2w`, `24h`, `30m` and returns a positive `time.Duration`. Used by Phase 4's `--since` flag.
2. `NoteRepository.FindByIDPrefix` — resolves a UUID prefix (≥ 8 chars) to a single note. Used by Phase 3's `tusk note archive` to accept short prefixes. Ambiguous matches surface explicitly.

## Tasks

### Task 1: Add `filter.ParseRelativeDuration`

**File:** `filter/durations.go` (new)

```go
package filter

import (
    "fmt"
    "strconv"
    "strings"
    "time"
)

// ParseRelativeDuration parses a positive duration with an extended unit set.
// Accepts Go's standard ParseDuration units (ns, us, ms, s, m, h) plus:
//   - "d" — 24 hours
//   - "w" — 7 days
//
// The input must be a single unsigned numeric literal followed by one unit
// suffix; compound forms such as "1w2d" are rejected to keep --since inputs
// unambiguous. Zero and negative durations are rejected.
//
// Examples:
//   ParseRelativeDuration("7d")  → 7 * 24h
//   ParseRelativeDuration("2w")  → 14 * 24h
//   ParseRelativeDuration("24h") → 24h
//   ParseRelativeDuration("30m") → 30m
func ParseRelativeDuration(s string) (time.Duration, error) {
    s = strings.TrimSpace(s)
    if s == "" {
        return 0, fmt.Errorf("duration must not be empty")
    }

    // Split into numeric prefix and unit suffix.
    var i int
    for i < len(s) && (s[i] >= '0' && s[i] <= '9') {
        i++
    }
    if i == 0 {
        return 0, fmt.Errorf("duration %q must begin with a positive integer", s)
    }
    if i == len(s) {
        return 0, fmt.Errorf("duration %q missing unit suffix (d, w, h, m, s)", s)
    }

    numStr := s[:i]
    unit := s[i:]

    n, err := strconv.ParseUint(numStr, 10, 64)
    if err != nil {
        return 0, fmt.Errorf("parsing duration %q: %w", s, err)
    }
    if n == 0 {
        return 0, fmt.Errorf("duration %q must be positive", s)
    }

    switch unit {
    case "d":
        return time.Duration(n) * 24 * time.Hour, nil
    case "w":
        return time.Duration(n) * 7 * 24 * time.Hour, nil
    case "h", "m", "s", "ms", "us", "ns":
        d, err := time.ParseDuration(s)
        if err != nil {
            return 0, fmt.Errorf("parsing duration %q: %w", s, err)
        }
        if d <= 0 {
            return 0, fmt.Errorf("duration %q must be positive", s)
        }
        return d, nil
    default:
        return 0, fmt.Errorf("unknown duration unit %q in %q (expected d, w, h, m, s)", unit, s)
    }
}
```

**Acceptance:** Function compiles, exported, no dependency on any filter internals. `go vet ./filter/...` passes.

---

### Task 2: Test `filter.ParseRelativeDuration`

**File:** `filter/durations_test.go` (new)

Cover every branch:

| Input | Expected |
|-------|----------|
| `"7d"` | `168 * time.Hour` |
| `"2w"` | `14 * 24 * time.Hour` |
| `"24h"` | `24 * time.Hour` |
| `"30m"` | `30 * time.Minute` |
| `"90s"` | `90 * time.Second` |
| `""` | error: empty |
| `"d"` | error: begin with integer |
| `"7"` | error: missing unit |
| `"0d"` | error: must be positive |
| `"-1d"` | error: begin with integer (leading `-` rejected at scan) |
| `"7x"` | error: unknown unit |
| `"1w2d"` | error: unknown unit `w2d` |
| `"1.5d"` | error: begin with integer (dot rejected) |

Use table-driven test with `t.Run(tc.in, ...)`. Assert both exact duration on success and `err != nil` with substring match on failure messages.

**Acceptance:** `go test -v ./filter -run ParseRelativeDuration` passes.

---

### Task 3: Extend `NoteRepository` with `FindByIDPrefix`

**File:** `repository/note.go`

Add a new method to the interface, immediately after `GetByID`:

```go
// FindByIDPrefix returns notes whose UUID (hyphenated lowercase form) begins
// with the given prefix. Prefix must contain at least 8 characters; shorter
// prefixes return (nil, nil) after the caller-side guard runs, so callers
// should enforce length themselves and rely on this method to report actual
// collisions. An exact 36-char UUID is accepted and will match at most one
// row. Results are returned in deterministic order (ascending UUID string).
// The repository does not distinguish active from archived notes here; the
// caller decides how to treat archived matches.
FindByIDPrefix(ctx context.Context, prefix string) ([]*domain.Note, error)
```

Update the interface doc comment block above the interface to mention the new method alongside the existing ones.

**Acceptance:** `go build ./repository/...` passes. Every type that implements `NoteRepository` (currently `sqlite.NoteRepo` only) reports a compile error until Task 4 lands — that is expected within this phase.

---

### Task 4: Implement `FindByIDPrefix` in SQLite

**File:** `sqlite/note.go`

Append a new method on the `NoteRepo` type. Follow the existing method style in the same file (context, parameterized queries, row scan, timestamp decoding).

```go
// FindByIDPrefix returns notes whose id (lowercase, hyphenated UUID string)
// begins with prefix. Returns up to 2 rows — callers only need to
// distinguish "zero matches", "one match", and "more than one match".
func (r *NoteRepo) FindByIDPrefix(ctx context.Context, prefix string) ([]*domain.Note, error) {
    rows, err := r.db.QueryContext(ctx, `
        SELECT id, project_id, player_id, task_id, body, metadata,
               archived_at, created_at
        FROM notes
        WHERE id LIKE ? || '%'
        ORDER BY id ASC
        LIMIT 2
    `, prefix)
    if err != nil {
        return nil, fmt.Errorf("querying notes by id prefix: %w", err)
    }
    defer rows.Close()

    var out []*domain.Note
    for rows.Next() {
        note, err := scanNote(rows)
        if err != nil {
            return nil, err
        }
        out = append(out, note)
    }
    if err := rows.Err(); err != nil {
        return nil, fmt.Errorf("iterating notes by id prefix: %w", err)
    }
    return out, nil
}
```

If a `scanNote` helper does not yet exist in `sqlite/note.go`, extract the row-scanning block used inside `GetByID` into a private helper `scanNote(row rowScanner) (*domain.Note, error)` where `rowScanner` is an interface satisfied by both `*sql.Row` and `*sql.Rows` (`Scan(dest ...any) error`). Reuse it from `GetByID`, `List`, and the new method. Do not change the on-disk schema or any existing SQL.

**Acceptance:** `go build ./sqlite/...` passes. `go test ./sqlite/... -run Note` passes (existing tests still green).

---

### Task 5: Test SQLite `FindByIDPrefix`

**File:** `sqlite/note_test.go`

Add cases that cover:

1. **Unique 8-char prefix match** — insert a single note with a known UUID (e.g., via `uuid.New()`), call `FindByIDPrefix(ctx, id.String()[:8])`, assert one result with matching ID.
2. **Full UUID match** — same note, call `FindByIDPrefix(ctx, id.String())`, assert one result.
3. **No match** — call with a prefix that cannot match any row (e.g., `"00000000"` on a table with a known set of non-zero UUIDs), assert empty slice, no error.
4. **Ambiguous prefix** — insert two notes whose UUIDs are crafted to share the first 8 characters. Construct the two notes by seeding their `ID` fields directly before `Create` — the repo's `Create` accepts pre-populated `note.ID`. Call `FindByIDPrefix` with the shared 8-char prefix, assert length 2 and that both IDs appear.
5. **Lowercase-only matching** — insert a note, call `FindByIDPrefix` with the prefix uppercased (e.g., `strings.ToUpper(id.String()[:8])`), assert empty result. This documents the behavior: prefix matching is case-sensitive; callers must lowercase before calling. (The CLI handler in Phase 3 will lowercase before invoking.)

Construct ambiguous UUIDs by calling `uuid.Parse` on a hand-written string like `"abcdef12-0000-4000-8000-000000000001"` and `"abcdef12-0000-4000-8000-000000000002"`.

**Acceptance:** `go test -v ./sqlite -run Prefix` passes. `go test -race ./sqlite/...` passes.

---

## Prerequisites

None. This phase may run in parallel with Phase 2.

## User-visible behavior preserved

No user-visible changes. The CLI surface, MCP surface, and library API are unchanged. All pre-existing e2e scenarios continue to pass.

## Bridge code introduced

None. Both additions are real implementations used in later phases — they are not stubs.

## Changes Introduced

**New files:**
- `filter/durations.go`
- `filter/durations_test.go`
- (if needed) updated `sqlite/note.go` with private `scanNote` helper

**Modified files:**
- `repository/note.go` — added `FindByIDPrefix` to `NoteRepository` interface
- `sqlite/note.go` — implemented `FindByIDPrefix`; may extract `scanNote` helper
- `sqlite/note_test.go` — added prefix-match tests

**New interfaces / methods:**
- `filter.ParseRelativeDuration(string) (time.Duration, error)`
- `repository.NoteRepository.FindByIDPrefix(ctx, prefix string) ([]*domain.Note, error)`

**Dependencies / migrations / env vars:** none.

**Bridge code removal targets:** none.
