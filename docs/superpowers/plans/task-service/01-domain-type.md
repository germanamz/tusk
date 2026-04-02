# Phase 1: Add `TaskUpdate` Domain Type

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add the `TaskUpdate` struct to the domain layer so the service layer can accept partial updates.

**Prereqs:** None — this phase has no dependencies.

**Files:**
- Modify: `internal/domain/task.go` (add struct after line 25, the closing brace of `Task`)

---

## Background

The `TaskUpdate` struct enables partial updates to tasks. Instead of the caller sending a full `Task` object (risking accidental overwrites), they send only the fields they want to change.

**Double-pointer pattern:** For fields that are nullable in the database (`ParentID`, `ProjectID`, `DueAt`, `WaitUntil`, `RecurrenceRule`), we use `**T` (pointer-to-pointer). This distinguishes three states:

| Outer pointer | Inner pointer | Meaning |
|---|---|---|
| `nil` | — | Don't change this field |
| non-nil | `nil` | Set the field to NULL |
| non-nil | non-nil | Set the field to this value |

For non-nullable fields (`Title`, `Description`, `Status`, `Priority`), a single pointer suffices: `nil` = don't change, non-nil = set to this value.

---

## Task 1: Add the `TaskUpdate` struct

**Files:**
- Modify: `internal/domain/task.go:25` (after the `Task` struct)

- [ ] **Step 1: Read the current file**

Open `internal/domain/task.go` and confirm the `Task` struct ends at line 25. You'll add the new struct right after it.

The file currently looks like this:

```go
package domain

import (
	"time"

	"github.com/google/uuid"
)

type Task struct {
	ID             uuid.UUID
	ShortID        string
	ParentID       *uuid.UUID
	ProjectID      *uuid.UUID
	Title          string
	Description    string
	Status         string
	Priority       int
	Version        int
	DueAt          *time.Time
	WaitUntil      *time.Time
	RecurrenceRule *string
	UDA            map[string]any
	CreatedAt      time.Time
	ModifiedAt     time.Time
}
```

- [ ] **Step 2: Add the `TaskUpdate` struct**

After the closing brace of `Task`, add:

```go
// TaskUpdate represents a partial update to a task.
// Nil pointer fields mean "don't change this field".
// For nullable fields (ParentID, ProjectID, DueAt, WaitUntil, RecurrenceRule),
// a double pointer is used: outer nil = don't change, outer non-nil + inner nil = set to NULL,
// outer non-nil + inner non-nil = set to value.
type TaskUpdate struct {
	ShortID        string          // required — identifies the task to update
	Version        int             // required — optimistic locking check
	Title          *string
	Description    *string
	Status         *string
	Priority       *int
	ParentID       **uuid.UUID
	ProjectID      **uuid.UUID
	DueAt          **time.Time
	WaitUntil      **time.Time
	RecurrenceRule **string
	UDA            *map[string]any
}
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/domain/...`

Expected: no output (clean compile). If you see an error, check for typos or missing imports (the existing imports for `time` and `uuid` already cover everything `TaskUpdate` needs).

- [ ] **Step 4: Run existing domain tests to check for regressions**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/domain/... -v`

Expected: all existing tests PASS (or no tests if the domain package has none).

- [ ] **Step 5: Commit**

```bash
git add internal/domain/task.go
git commit -m "feat(domain): add TaskUpdate struct for partial updates"
```
