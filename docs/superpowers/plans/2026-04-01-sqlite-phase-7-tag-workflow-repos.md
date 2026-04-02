# SQLite Phase 7: TagRepo & WorkflowRepo — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the final two SQLite repository structs — `TagRepo` (6 methods, including join table operations for assigning/removing tags on tasks) and `WorkflowRepo` (4 methods, including JSON column handling for the `Statuses` field). Also add an integration test that proves Phase 4's tag-based filtering (`TaskRepo.List` with `TaskFilter.Tags` and `TaskFilter.ExcludeTags`) works end-to-end with real tag data. After this phase, the entire SQLite persistence layer is complete.

**Architecture:** Both repos live in `internal/sqlite/` and implement their respective interfaces from `internal/repository/`. They receive a `*sql.DB` (obtained from `Store.DB()`) and follow the same patterns established in Phases 1-6. Tests use helpers from previous phases: `testStore` and `mustTimeNow` from `store_test.go`, `newTestTask` and `mustCreateTask` from `task_test.go`, and `NewProjectRepo` from `project.go`.

**Tech Stack:** Go 1.26, `github.com/google/uuid`, `database/sql`, `encoding/json`, CGo SQLite driver

---

## Context: What Has Been Built So Far

**Tusk** is a terminal-based task manager written in Go. It stores tasks, projects, tags, workflows, and relations in a local SQLite database. Each piece of the persistence layer has been built incrementally across phases.

**Phase 0 (Domain + Repository Interfaces):** Created all domain structs (`Task`, `Project`, `Tag`, `Workflow`, `WorkflowTransition`, etc.) in `internal/domain/` and all repository interfaces (`TaskRepository`, `TagRepository`, `WorkflowRepository`, etc.) in `internal/repository/`. These are pure types with no logic.

**Phase 1 (SQLite Foundation):** Created the `Store` struct in `internal/sqlite/store.go` with:
- `New()` — opens the SQLite database and runs migrations (which create all tables, including `tags`, `tag_assignments`, `workflows`, and `workflow_transitions`)
- `DB()` — returns the underlying `*sql.DB` so repos can use it
- `Close()` — closes the database connection
- `const timeFormat` — the standard time layout `"2006-01-02T15:04:05.000Z"` used for all time columns
- Helper functions: `nullableString(*string) any` — returns `nil` (SQL NULL) if the pointer is nil, or the dereferenced string if set
- Test helpers in `store_test.go`: `testStore(t)` (creates an in-memory DB with all migrations applied, including seed data) and `mustTimeNow()` (returns the current time truncated to millisecond precision)

**Phase 2 (ProjectRepo):** Implemented `ProjectRepo` in `project.go` with `NewProjectRepo(db *sql.DB)`. We need this in our workflow tests because workflows belong to projects (the `workflows` table has a `project_id` foreign key referencing `projects`).

**Phase 3 (TaskRepo CRUD):** Implemented `TaskRepo` in `task.go` with `NewTaskRepo(db *sql.DB)`. Created shared test helpers in `task_test.go`: `newTestTask()` (creates a minimal valid `domain.Task`) and `mustCreateTask(t, repo, task)` (inserts a task, fails the test if it errors). We need these because tag assignment tests require existing tasks in the database.

**Phase 4 (TaskRepo.List with filters):** Implemented the `List` method on `TaskRepo` with support for filtering by tags (`TaskFilter.Tags`) and excluding by tags (`TaskFilter.ExcludeTags`). Our integration test in this phase will verify that these filters work correctly with real tag data in the database.

**Phases 5 and 6:** Implemented tree queries (`GetChildren`, `GetDescendants`) and the `RelationRepo`. Not directly relevant to this phase, but all their tests will continue to run as part of the full suite.

**This Phase (Phase 7)** builds the last two repositories and completes the SQLite layer.

---

## Key Concepts You Need to Understand

### 1. Join Tables (the `tag_assignments` table)

**The problem:** A task can have many tags, and a tag can be on many tasks. This is called a **many-to-many relationship**. You cannot model this with a single foreign key column — if you put a `tag_id` on the tasks table, each task could only have one tag.

**The solution — a join table:** We create a third table, `tag_assignments`, that has two columns:

```sql
CREATE TABLE tag_assignments (
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    tag_id  TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, tag_id)
);
```

Each row in `tag_assignments` represents one link between a task and a tag. For example:

| task_id | tag_id |
|---------|--------|
| task-aaa | tag-111 |
| task-aaa | tag-222 |
| task-bbb | tag-111 |

This tells us:
- Task `aaa` has tags `111` and `222`
- Task `bbb` has tag `111`
- Tag `111` is on tasks `aaa` and `bbb`

**The `PRIMARY KEY (task_id, tag_id)` is a composite primary key.** It means the combination of `task_id` and `tag_id` must be unique — you cannot assign the same tag to the same task twice. If you try, SQLite will return a `UNIQUE constraint failed` error.

**`ON DELETE CASCADE`** means: if a task is deleted from the `tasks` table, all its rows in `tag_assignments` are automatically deleted too. Same for tags. This prevents orphan rows.

In our Go code:
- `AssignToTask(taskID, tagID)` inserts a row into `tag_assignments`
- `RemoveFromTask(taskID, tagID)` deletes a row from `tag_assignments`
- `GetTaskTags(taskID)` queries `tag_assignments` joined with `tags` to get the full tag objects

### 2. JOIN Queries (how `GetTaskTags` works)

To get all tags for a task, we need data from **two tables**: the tag's details (name, color) live in `tags`, but the link to the task lives in `tag_assignments`. A `JOIN` combines rows from two tables based on a matching condition:

```sql
SELECT t.id, t.name, t.color
FROM tags t
JOIN tag_assignments ta ON t.id = ta.tag_id
WHERE ta.task_id = ?
```

Here is what happens step by step:

1. `FROM tags t` — start with the tags table (aliased as `t` for brevity)
2. `JOIN tag_assignments ta ON t.id = ta.tag_id` — for each tag, find rows in `tag_assignments` where the tag's ID matches. Only tags that have at least one matching row survive the join.
3. `WHERE ta.task_id = ?` — of those joined rows, keep only the ones for our specific task.

The result is all tags that are assigned to the given task.

### 3. JSON Columns in SQLite (the `statuses` column)

SQLite does not have a native JSON column type. Instead, we store JSON as plain `TEXT`. The `workflows` table has:

```sql
statuses TEXT NOT NULL DEFAULT '["pending","active","completed","deleted"]'
```

In Go, the domain type is `[]string` (a slice of strings). We need to convert between Go and SQLite:

**Writing (Go to SQLite):** Before inserting, convert the Go slice to a JSON string:

```go
statusesJSON, err := json.Marshal(wf.Statuses)
// wf.Statuses = []string{"backlog", "in_progress", "done"}
// statusesJSON = []byte(`["backlog","in_progress","done"]`)
```

Then pass `string(statusesJSON)` to the SQL query.

**Reading (SQLite to Go):** After scanning, convert the JSON string back to a Go slice:

```go
var statusesJSON string
// scan statusesJSON from the database...
err := json.Unmarshal([]byte(statusesJSON), &wf.Statuses)
// statusesJSON = `["pending","active","completed","deleted"]`
// wf.Statuses = []string{"pending", "active", "completed", "deleted"}
```

`json.Marshal` converts a Go value to JSON bytes. `json.Unmarshal` converts JSON bytes back to a Go value. Together they let us store structured data in a plain text column.

### 4. Nullable `*string` for `Tag.Color`

The `Tag` struct has `Color *string` — a pointer to a string. This represents a nullable field:
- `nil` means "no color set" (stored as SQL `NULL`)
- `&"#ff0000"` means "red" (stored as the string `"#ff0000"`)

**Writing:** We use the `nullableString` helper from `store.go`:

```go
nullableString(tag.Color)
// If tag.Color is nil  -> returns nil (SQL NULL)
// If tag.Color is &"#ff0000" -> returns "#ff0000"
```

**Reading:** We scan into `sql.NullString`, then check `.Valid`:

```go
var color sql.NullString
row.Scan(..., &color)
if color.Valid {
    tag.Color = &color.String  // column had a value
}
// If color.Valid is false, tag.Color stays nil (its zero value)
```

This is the same pattern used for `RecurrenceRule` in `TaskRepo`.

### 5. Seed Data (Default Workflow in Migrations)

When `testStore(t)` creates an in-memory database and runs migrations, the migrations insert **seed data** — default rows that the application needs to function. Specifically:

- A **default project** with ID `00000000-0000-0000-0000-000000000000` (all zeros) and name `"default"`
- A **default workflow** linked to that project, named `"default"`, with statuses `["pending", "active", "completed", "deleted"]`
- **6 workflow transitions** for the default workflow (e.g., pending -> active, active -> completed, etc.)

This means our workflow tests can query for the default workflow **without creating it first**. The `TestWorkflowGetByProjectAndName` test does exactly this — it looks up the default project's default workflow and verifies it has 4 statuses.

For tests that need a *custom* workflow (like `TestWorkflowCreate`), we first create a new project (using `NewProjectRepo`), then create a workflow linked to it.

### 6. Composite Primary Keys

The `tag_assignments` table has `PRIMARY KEY (task_id, tag_id)` — two columns together form the primary key. This is different from the other tables, which have a single `id TEXT PRIMARY KEY`.

A composite primary key means:
- The combination of (task_id, tag_id) must be unique
- There is no separate `id` column
- To delete a specific row, you need both values: `DELETE FROM tag_assignments WHERE task_id = ? AND tag_id = ?`

Similarly, `workflow_transitions` has `UNIQUE(workflow_id, from_status, to_status)` — you cannot create two transitions with the same from/to pair in the same workflow.

---

## How the Pieces Wire Together

```
store.go             tag.go                 tag_test.go
────────             ──────                 ───────────
Store.DB()  ────>  NewTagRepo(db)          testStore(t) from store_test.go
                      │                         │
nullableString()      │ uses helper             │ creates in-memory DB
                      │                         │
                      v                         v
                AssignToTask()           NewTagRepo(s.DB())
                RemoveFromTask()         NewTaskRepo(s.DB())  <── from task.go
                GetTaskTags()            newTestTask()         <── from task_test.go
                                         mustCreateTask()      <── from task_test.go

store.go             workflow.go            workflow_test.go
────────             ───────────            ────────────────
Store.DB()  ────>  NewWorkflowRepo(db)     testStore(t) from store_test.go
                      │                         │
json.Marshal()        │ uses encoding/json      │ creates in-memory DB
json.Unmarshal()      │                         │ (with seed data!)
                      v                         v
              GetByProjectAndName()      NewWorkflowRepo(s.DB())
              Create()                   NewProjectRepo(s.DB())  <── from project.go
              GetTransitions()           mustTimeNow()           <── from store_test.go
              AddTransition()
```

**Important cross-phase dependencies:**

1. **Tag tests use task helpers:** `TestTagAssignToTask`, `TestTagRemoveFromTask`, `TestTagGetTaskTagsEmpty`, and `TestTagFilterIntegration` all need tasks in the database. They use `NewTaskRepo`, `newTestTask()`, and `mustCreateTask()` from Phase 3.

2. **Workflow tests use project helpers:** `TestWorkflowCreate` and `TestWorkflowAddTransition` need to create projects first (because `workflows.project_id` has a foreign key). They use `NewProjectRepo` from Phase 2 and `mustTimeNow()` from Phase 1.

3. **The integration test bridges Phases 4 and 7:** `TestTagFilterIntegration` creates tags, creates tasks, assigns tags to tasks, then calls `taskRepo.List()` with tag filters. This validates that Phase 4's SQL filter logic works correctly with real tag assignment data.

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `internal/sqlite/tag_test.go` | Tag tests + integration test |
| Modify | `internal/sqlite/tag.go` | Full `TagRepo` implementation (replaces stub) |
| Create | `internal/sqlite/workflow_test.go` | Workflow tests |
| Create | `internal/sqlite/workflow.go` | Full `WorkflowRepo` implementation |

---

### Task 1: Write Tag Tests

**Files:**
- Create: `internal/sqlite/tag_test.go`

This task creates the tag test file. The tests will not compile yet because `TagRepo` only has a stub `package sqlite` line. This is intentional — we write tests first, then make them pass in Task 2.

The test file includes:

- **A compile-time interface check:** `var _ repository.TagRepository = (*TagRepo)(nil)` — this tells the Go compiler to verify that `*TagRepo` implements every method in `repository.TagRepository`. If any method is missing or has the wrong signature, the build fails.
- **Basic CRUD tests:** Create a tag with a color, create one without a color (null), look up a nonexistent tag.
- **Join table tests:** Assign a tag to a task, remove a tag from a task, query tags for a task with no tags.
- **An integration test** (`TestTagFilterIntegration`) that creates 3 tags, 3 tasks, assigns tags in a specific pattern, then uses `taskRepo.List()` with `TaskFilter.Tags` and `TaskFilter.ExcludeTags` to verify filtering works correctly.

- [ ] **Step 1: Write `tag_test.go`**

Create the file `internal/sqlite/tag_test.go` with the following exact contents:

```go
package sqlite

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

var _ repository.TagRepository = (*TagRepo)(nil)

func TestTagCreate(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())
	ctx := context.Background()
	color := "#ff0000"
	tag := &domain.Tag{ID: uuid.New(), Name: "bug", Color: &color}
	if err := repo.Create(ctx, tag); err != nil { t.Fatalf("Create: %v", err) }
	got, err := repo.GetByName(ctx, "bug")
	if err != nil { t.Fatal(err) }
	if got.Name != "bug" { t.Fatalf("expected bug, got %s", got.Name) }
	if got.Color == nil || *got.Color != "#ff0000" { t.Fatalf("expected #ff0000, got %v", got.Color) }
}

func TestTagCreateNullColor(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())
	tag := &domain.Tag{ID: uuid.New(), Name: "frontend"}
	if err := repo.Create(context.Background(), tag); err != nil { t.Fatal(err) }
	got, err := repo.GetByName(context.Background(), "frontend")
	if err != nil { t.Fatal(err) }
	if got.Color != nil { t.Fatalf("expected nil color, got %v", got.Color) }
}

func TestTagGetByNameNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())
	_, err := repo.GetByName(context.Background(), "nonexistent")
	if err != domain.ErrNotFound { t.Fatalf("expected ErrNotFound, got %v", err) }
}

func TestTagList(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())
	ctx := context.Background()
	for _, name := range []string{"bug", "feature", "docs"} {
		if err := repo.Create(ctx, &domain.Tag{ID: uuid.New(), Name: name}); err != nil { t.Fatal(err) }
	}
	tags, err := repo.List(ctx)
	if err != nil { t.Fatal(err) }
	if len(tags) != 3 { t.Fatalf("expected 3, got %d", len(tags)) }
}

func TestTagAssignToTask(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	tagRepo := NewTagRepo(s.DB())
	ctx := context.Background()
	task := newTestTask(); mustCreateTask(t, taskRepo, task)
	tag := &domain.Tag{ID: uuid.New(), Name: "urgent"}
	if err := tagRepo.Create(ctx, tag); err != nil { t.Fatal(err) }
	if err := tagRepo.AssignToTask(ctx, task.ID, tag.ID); err != nil { t.Fatalf("AssignToTask: %v", err) }
	tags, err := tagRepo.GetTaskTags(ctx, task.ID)
	if err != nil { t.Fatal(err) }
	if len(tags) != 1 { t.Fatalf("expected 1, got %d", len(tags)) }
	if tags[0].Name != "urgent" { t.Fatalf("expected urgent, got %s", tags[0].Name) }
}

func TestTagRemoveFromTask(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	tagRepo := NewTagRepo(s.DB())
	ctx := context.Background()
	task := newTestTask(); mustCreateTask(t, taskRepo, task)
	tag := &domain.Tag{ID: uuid.New(), Name: "temp"}
	if err := tagRepo.Create(ctx, tag); err != nil { t.Fatal(err) }
	if err := tagRepo.AssignToTask(ctx, task.ID, tag.ID); err != nil { t.Fatal(err) }
	if err := tagRepo.RemoveFromTask(ctx, task.ID, tag.ID); err != nil { t.Fatalf("RemoveFromTask: %v", err) }
	tags, err := tagRepo.GetTaskTags(ctx, task.ID)
	if err != nil { t.Fatal(err) }
	if len(tags) != 0 { t.Fatalf("expected 0 after remove, got %d", len(tags)) }
}

func TestTagGetTaskTagsEmpty(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	tagRepo := NewTagRepo(s.DB())
	ctx := context.Background()
	task := newTestTask(); mustCreateTask(t, taskRepo, task)
	tags, err := tagRepo.GetTaskTags(ctx, task.ID)
	if err != nil { t.Fatal(err) }
	if len(tags) != 0 { t.Fatalf("expected 0, got %d", len(tags)) }
}

func TestTagFilterIntegration(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	tagRepo := NewTagRepo(s.DB())
	ctx := context.Background()
	bugTag := &domain.Tag{ID: uuid.New(), Name: "bug"}
	apiTag := &domain.Tag{ID: uuid.New(), Name: "api"}
	docsTag := &domain.Tag{ID: uuid.New(), Name: "docs"}
	for _, tag := range []*domain.Tag{bugTag, apiTag, docsTag} {
		if err := tagRepo.Create(ctx, tag); err != nil { t.Fatal(err) }
	}
	t1 := newTestTask(); t2 := newTestTask(); t3 := newTestTask()
	for _, task := range []*domain.Task{t1, t2, t3} { mustCreateTask(t, taskRepo, task) }
	for _, pair := range [][2]uuid.UUID{
		{t1.ID, bugTag.ID}, {t1.ID, apiTag.ID},
		{t2.ID, bugTag.ID}, {t2.ID, docsTag.ID},
		{t3.ID, apiTag.ID},
	} {
		if err := tagRepo.AssignToTask(ctx, pair[0], pair[1]); err != nil { t.Fatal(err) }
	}
	tasks, err := taskRepo.List(ctx, domain.TaskFilter{Tags: []string{"bug", "api"}})
	if err != nil { t.Fatal(err) }
	if len(tasks) != 1 || tasks[0].ID != t1.ID { t.Fatalf("expected only t1, got %d tasks", len(tasks)) }
	tasks, err = taskRepo.List(ctx, domain.TaskFilter{ExcludeTags: []string{"docs"}})
	if err != nil { t.Fatal(err) }
	if len(tasks) != 2 { t.Fatalf("expected 2 excluding docs, got %d", len(tasks)) }
}
```

**What each test verifies:**

| Test | What it proves |
|------|----------------|
| `TestTagCreate` | Insert a tag with a color; verify name and color survive the round-trip |
| `TestTagCreateNullColor` | Insert a tag without a color (nil); verify color comes back as nil, not empty string |
| `TestTagGetByNameNotFound` | Querying a nonexistent tag name returns `domain.ErrNotFound` |
| `TestTagList` | Insert 3 tags; verify List returns all 3 |
| `TestTagAssignToTask` | Create a task and a tag, assign the tag; verify GetTaskTags returns it |
| `TestTagRemoveFromTask` | Assign a tag to a task, then remove it; verify GetTaskTags returns 0 tags |
| `TestTagGetTaskTagsEmpty` | Query tags for a task that has none; verify empty slice, no error |
| `TestTagFilterIntegration` | End-to-end: 3 tags, 3 tasks, 5 assignments; verify `TaskRepo.List` with `Tags` filter returns only the task with ALL specified tags, and `ExcludeTags` filter excludes tasks with any excluded tag |

**Understanding `TestTagFilterIntegration` in detail:**

This test creates the following tag assignment pattern:

| Task | Tags |
|------|------|
| t1 | bug, api |
| t2 | bug, docs |
| t3 | api |

Then it runs two filter queries:

1. `TaskFilter{Tags: []string{"bug", "api"}}` — find tasks that have **both** "bug" AND "api". Only `t1` has both. Result: 1 task.
2. `TaskFilter{ExcludeTags: []string{"docs"}}` — find tasks that do NOT have "docs". `t2` has "docs", so it is excluded. Result: 2 tasks (`t1` and `t3`).

This test does not test `TagRepo` alone — it tests the **integration** between `TagRepo` (which creates the tag data) and `TaskRepo.List` (which queries using that data). This is why it is called an integration test.

- [ ] **Step 2: Verify the tests do NOT compile yet**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/sqlite/`

Expected: Compilation errors because `TagRepo` does not yet have the required methods. This is correct — we wrote tests first.

- [ ] **Step 3: Commit the test file**

```bash
git add internal/sqlite/tag_test.go
git commit -m "test(sqlite): add TagRepo tests and tag filter integration test"
```

---

### Task 2: Implement TagRepo

**Files:**
- Modify: `internal/sqlite/tag.go` (currently contains only `package sqlite`)

Now we write the implementation that makes all the tag tests from Task 1 pass. This file replaces the stub `tag.go`.

- [ ] **Step 1: Write `tag.go`**

Replace the contents of `internal/sqlite/tag.go` with the following exact code:

```go
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

type TagRepo struct {
	db *sql.DB
}

func NewTagRepo(db *sql.DB) *TagRepo {
	return &TagRepo{db: db}
}

func (r *TagRepo) Create(ctx context.Context, tag *domain.Tag) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO tags (id, name, color) VALUES (?, ?, ?)`,
		tag.ID.String(), tag.Name, nullableString(tag.Color),
	)
	return err
}

func (r *TagRepo) GetByName(ctx context.Context, name string) (*domain.Tag, error) {
	var (tag domain.Tag; id string; color sql.NullString)
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, color FROM tags WHERE name = ?`, name,
	).Scan(&id, &tag.Name, &color)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil { return nil, err }
	tag.ID, err = uuid.Parse(id)
	if err != nil { return nil, err }
	if color.Valid { tag.Color = &color.String }
	return &tag, nil
}

func (r *TagRepo) List(ctx context.Context) ([]*domain.Tag, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, color FROM tags`)
	if err != nil { return nil, err }
	defer rows.Close()
	var result []*domain.Tag
	for rows.Next() {
		var (tag domain.Tag; id string; color sql.NullString)
		if err := rows.Scan(&id, &tag.Name, &color); err != nil { return nil, err }
		tag.ID, err = uuid.Parse(id)
		if err != nil { return nil, err }
		if color.Valid { tag.Color = &color.String }
		result = append(result, &tag)
	}
	return result, rows.Err()
}

func (r *TagRepo) AssignToTask(ctx context.Context, taskID, tagID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO tag_assignments (task_id, tag_id) VALUES (?, ?)`,
		taskID.String(), tagID.String())
	return err
}

func (r *TagRepo) RemoveFromTask(ctx context.Context, taskID, tagID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM tag_assignments WHERE task_id = ? AND tag_id = ?`,
		taskID.String(), tagID.String())
	return err
}

func (r *TagRepo) GetTaskTags(ctx context.Context, taskID uuid.UUID) ([]*domain.Tag, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT t.id, t.name, t.color FROM tags t
		 JOIN tag_assignments ta ON t.id = ta.tag_id
		 WHERE ta.task_id = ?`, taskID.String())
	if err != nil { return nil, err }
	defer rows.Close()
	var result []*domain.Tag
	for rows.Next() {
		var (tag domain.Tag; id string; color sql.NullString)
		if err := rows.Scan(&id, &tag.Name, &color); err != nil { return nil, err }
		tag.ID, err = uuid.Parse(id)
		if err != nil { return nil, err }
		if color.Valid { tag.Color = &color.String }
		result = append(result, &tag)
	}
	return result, rows.Err()
}
```

**Line-by-line walkthrough of the important parts:**

**`TagRepo` struct (lines 12-14):**
Holds a single field: `db *sql.DB`. Same pattern as every other repo in the project.

**`NewTagRepo(db *sql.DB)` constructor (lines 16-18):**
Takes the database handle from `Store.DB()` and returns a ready-to-use repo.

**`Create` method (lines 20-26):**
Inserts a new tag. The three columns are:
- `id` — the UUID converted to a string (`tag.ID.String()`)
- `name` — plain string, stored as-is
- `color` — nullable, passed through `nullableString(tag.Color)` from `store.go`. If `tag.Color` is `nil`, this passes SQL `NULL`. If it is `&"#ff0000"`, this passes the string `"#ff0000"`.

**`GetByName` method (lines 28-42):**
Looks up a tag by name. Key details:
1. We scan `color` into `sql.NullString` because the column can be NULL.
2. If `QueryRowContext` finds no matching row, `Scan` returns `sql.ErrNoRows`. We translate that to `domain.ErrNotFound` using `errors.Is()`.
3. We parse the `id` string into `uuid.UUID` using `uuid.Parse()`.
4. If `color.Valid` is true (column was NOT null), we set `tag.Color = &color.String`. Otherwise, `tag.Color` stays `nil` (its zero value for a pointer).

**`List` method (lines 44-58):**
Returns all tags. This uses `QueryContext` (returns multiple rows) instead of `QueryRowContext` (returns one row). The pattern:
1. `r.db.QueryContext(ctx, ...)` returns `*sql.Rows` and an error.
2. `defer rows.Close()` ensures the result set is closed even if we return early due to an error. Forgetting this can leak database connections.
3. `for rows.Next()` iterates over each row. `rows.Next()` returns `false` when there are no more rows or an error occurs.
4. Inside the loop, we scan each row and append to the result slice.
5. After the loop, `rows.Err()` returns any error that occurred during iteration. Always check this — it catches errors like broken connections that `rows.Next()` silently hides by returning `false`.

**`AssignToTask` method (lines 60-65):**
Inserts a row into the `tag_assignments` join table. This is the simplest possible INSERT — just two UUID strings. If the tag is already assigned to this task, the composite primary key constraint will cause a `UNIQUE constraint failed` error.

**`RemoveFromTask` method (lines 67-72):**
Deletes a row from the `tag_assignments` join table. The WHERE clause uses both columns of the composite primary key. If the assignment does not exist, the DELETE affects 0 rows but does not error — this is standard SQL behavior.

**`GetTaskTags` method (lines 74-90):**
This is the JOIN query explained in the Key Concepts section above. It joins `tags` with `tag_assignments` to find all tags assigned to a specific task. The scanning logic is identical to `List` — same three columns (`id`, `name`, `color`), same `sql.NullString` for color.

Note that `List` and `GetTaskTags` have nearly identical scanning code. In a larger project, you might extract a `scanTag` helper (like `scanTask` in `task.go`). For only 3 columns, the duplication is acceptable and keeps the code straightforward.

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/sqlite/`

Expected: No errors.

- [ ] **Step 3: Run the tag tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run TestTag`

Expected output (all 8 tests pass):

```
=== RUN   TestTagCreate
--- PASS: TestTagCreate
=== RUN   TestTagCreateNullColor
--- PASS: TestTagCreateNullColor
=== RUN   TestTagGetByNameNotFound
--- PASS: TestTagGetByNameNotFound
=== RUN   TestTagList
--- PASS: TestTagList
=== RUN   TestTagAssignToTask
--- PASS: TestTagAssignToTask
=== RUN   TestTagRemoveFromTask
--- PASS: TestTagRemoveFromTask
=== RUN   TestTagGetTaskTagsEmpty
--- PASS: TestTagGetTaskTagsEmpty
=== RUN   TestTagFilterIntegration
--- PASS: TestTagFilterIntegration
PASS
```

If any test fails, read the error message carefully. Common issues:
- **"no such table: tags"** — the migrations did not create the `tags` table. Check your migration files.
- **"no such table: tag_assignments"** — same issue, but for the join table.
- **"expected ErrNotFound, got <nil>"** — `GetByName` is not converting `sql.ErrNoRows` to `domain.ErrNotFound`.
- **"expected #ff0000, got <nil>"** — the `color.Valid` check is missing or incorrect.
- **"expected only t1, got N tasks"** in the integration test — the `TaskRepo.List` tag filter SQL from Phase 4 may have a bug.

- [ ] **Step 4: Commit**

```bash
git add internal/sqlite/tag.go internal/sqlite/tag_test.go
git commit -m "feat(sqlite): implement TagRepo with join table operations"
```

---

### Task 3: Write Workflow Tests

**Files:**
- Create: `internal/sqlite/workflow_test.go`

This task creates the workflow test file. Like Task 1, we write tests first.

The workflow tests are interesting because some tests read **seed data** (the default workflow created by migrations) while others create fresh data.

- [ ] **Step 1: Write `workflow_test.go`**

Create the file `internal/sqlite/workflow_test.go` with the following exact contents:

```go
package sqlite

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

var _ repository.WorkflowRepository = (*WorkflowRepo)(nil)

func TestWorkflowGetByProjectAndName(t *testing.T) {
	s := testStore(t)
	repo := NewWorkflowRepo(s.DB())
	ctx := context.Background()
	defaultProjectID := uuid.MustParse("00000000-0000-0000-0000-000000000000")
	wf, err := repo.GetByProjectAndName(ctx, defaultProjectID, "default")
	if err != nil { t.Fatalf("GetByProjectAndName: %v", err) }
	if wf.Name != "default" { t.Fatalf("expected default, got %s", wf.Name) }
	if len(wf.Statuses) != 4 { t.Fatalf("expected 4 statuses, got %d", len(wf.Statuses)) }
	expected := []string{"pending", "active", "completed", "deleted"}
	for i, s := range expected {
		if wf.Statuses[i] != s { t.Fatalf("status[%d]: expected %s, got %s", i, s, wf.Statuses[i]) }
	}
}

func TestWorkflowGetByProjectAndNameNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewWorkflowRepo(s.DB())
	_, err := repo.GetByProjectAndName(context.Background(), uuid.New(), "nonexistent")
	if err != domain.ErrNotFound { t.Fatalf("expected ErrNotFound, got %v", err) }
}

func TestWorkflowGetTransitions(t *testing.T) {
	s := testStore(t)
	repo := NewWorkflowRepo(s.DB())
	ctx := context.Background()
	defaultProjectID := uuid.MustParse("00000000-0000-0000-0000-000000000000")
	wf, err := repo.GetByProjectAndName(ctx, defaultProjectID, "default")
	if err != nil { t.Fatal(err) }
	transitions, err := repo.GetTransitions(ctx, wf.ID)
	if err != nil { t.Fatal(err) }
	if len(transitions) != 6 { t.Fatalf("expected 6, got %d", len(transitions)) }
}

func TestWorkflowCreate(t *testing.T) {
	s := testStore(t)
	projRepo := NewProjectRepo(s.DB())
	repo := NewWorkflowRepo(s.DB())
	ctx := context.Background()
	proj := &domain.Project{ID: uuid.New(), Name: "kanban-project", DefaultWorkflow: "kanban", CreatedAt: mustTimeNow()}
	if err := projRepo.Create(ctx, proj); err != nil { t.Fatal(err) }
	wf := &domain.Workflow{ID: uuid.New(), ProjectID: proj.ID, Name: "kanban", Statuses: []string{"backlog", "in_progress", "review", "done"}}
	if err := repo.Create(ctx, wf); err != nil { t.Fatalf("Create: %v", err) }
	got, err := repo.GetByProjectAndName(ctx, proj.ID, "kanban")
	if err != nil { t.Fatal(err) }
	if len(got.Statuses) != 4 { t.Fatalf("expected 4, got %d", len(got.Statuses)) }
	if got.Statuses[0] != "backlog" { t.Fatalf("expected backlog, got %s", got.Statuses[0]) }
}

func TestWorkflowAddTransition(t *testing.T) {
	s := testStore(t)
	projRepo := NewProjectRepo(s.DB())
	repo := NewWorkflowRepo(s.DB())
	ctx := context.Background()
	proj := &domain.Project{ID: uuid.New(), Name: "test-proj", DefaultWorkflow: "simple", CreatedAt: mustTimeNow()}
	if err := projRepo.Create(ctx, proj); err != nil { t.Fatal(err) }
	wf := &domain.Workflow{ID: uuid.New(), ProjectID: proj.ID, Name: "simple", Statuses: []string{"open", "closed"}}
	if err := repo.Create(ctx, wf); err != nil { t.Fatal(err) }
	tr := &domain.WorkflowTransition{ID: uuid.New(), WorkflowID: wf.ID, FromStatus: "open", ToStatus: "closed"}
	if err := repo.AddTransition(ctx, tr); err != nil { t.Fatalf("AddTransition: %v", err) }
	transitions, err := repo.GetTransitions(ctx, wf.ID)
	if err != nil { t.Fatal(err) }
	if len(transitions) != 1 { t.Fatalf("expected 1, got %d", len(transitions)) }
	if transitions[0].FromStatus != "open" || transitions[0].ToStatus != "closed" {
		t.Fatalf("unexpected: %s → %s", transitions[0].FromStatus, transitions[0].ToStatus)
	}
}
```

**What each test verifies:**

| Test | What it proves |
|------|----------------|
| `TestWorkflowGetByProjectAndName` | Can read the seed (default) workflow; JSON statuses are correctly unmarshaled into a 4-element string slice |
| `TestWorkflowGetByProjectAndNameNotFound` | Querying a nonexistent project/name pair returns `domain.ErrNotFound` |
| `TestWorkflowGetTransitions` | The seed workflow has exactly 6 transitions (seeded by migrations) |
| `TestWorkflowCreate` | Create a new project, then a new workflow with custom statuses; verify round-trip through JSON serialization |
| `TestWorkflowAddTransition` | Create a workflow, add a transition; verify `GetTransitions` returns it with correct from/to values |

**Understanding `TestWorkflowGetByProjectAndName` in detail:**

This test uses `uuid.MustParse("00000000-0000-0000-0000-000000000000")` — the all-zeros UUID. This is the ID of the default project created by the migration seed data. The test:

1. Calls `GetByProjectAndName(ctx, defaultProjectID, "default")` — this queries the `workflows` table for a workflow with this project ID and the name "default".
2. Verifies `wf.Name` is "default".
3. Verifies `wf.Statuses` has exactly 4 elements.
4. Verifies each status in order: "pending", "active", "completed", "deleted".

This works because `testStore(t)` runs all migrations, which include the INSERT statements that seed the default workflow. If the migrations change, this test will catch it.

**Understanding `TestWorkflowCreate` in detail:**

Unlike the seed data tests, this test creates everything from scratch:

1. Creates a new project using `NewProjectRepo`. The project needs `mustTimeNow()` for its `CreatedAt` field (projects have timestamps, workflows do not).
2. Creates a workflow linked to that project with custom statuses: `["backlog", "in_progress", "review", "done"]`.
3. Reads the workflow back using `GetByProjectAndName`.
4. Verifies the statuses survived the `json.Marshal` (during Create) and `json.Unmarshal` (during GetByProjectAndName) round-trip.

- [ ] **Step 2: Verify the tests do NOT compile yet**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/sqlite/`

Expected: Compilation errors because `WorkflowRepo` does not exist yet.

- [ ] **Step 3: Commit the test file**

```bash
git add internal/sqlite/workflow_test.go
git commit -m "test(sqlite): add WorkflowRepo tests including seed data validation"
```

---

### Task 4: Implement WorkflowRepo

**Files:**
- Create: `internal/sqlite/workflow.go`

Now we write the implementation that makes all the workflow tests from Task 3 pass.

- [ ] **Step 1: Write `workflow.go`**

Create the file `internal/sqlite/workflow.go` with the following exact contents:

```go
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

type WorkflowRepo struct {
	db *sql.DB
}

func NewWorkflowRepo(db *sql.DB) *WorkflowRepo {
	return &WorkflowRepo{db: db}
}

func (r *WorkflowRepo) GetByProjectAndName(ctx context.Context, projectID uuid.UUID, name string) (*domain.Workflow, error) {
	var (wf domain.Workflow; id, pid, statusesJSON string)
	err := r.db.QueryRowContext(ctx,
		`SELECT id, project_id, name, statuses FROM workflows WHERE project_id = ? AND name = ?`,
		projectID.String(), name,
	).Scan(&id, &pid, &wf.Name, &statusesJSON)
	if errors.Is(err, sql.ErrNoRows) { return nil, domain.ErrNotFound }
	if err != nil { return nil, err }
	wf.ID, err = uuid.Parse(id)
	if err != nil { return nil, err }
	wf.ProjectID, err = uuid.Parse(pid)
	if err != nil { return nil, err }
	if err := json.Unmarshal([]byte(statusesJSON), &wf.Statuses); err != nil { return nil, err }
	return &wf, nil
}

func (r *WorkflowRepo) GetTransitions(ctx context.Context, workflowID uuid.UUID) ([]*domain.WorkflowTransition, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, workflow_id, from_status, to_status FROM workflow_transitions WHERE workflow_id = ?`,
		workflowID.String())
	if err != nil { return nil, err }
	defer rows.Close()
	var result []*domain.WorkflowTransition
	for rows.Next() {
		var (t domain.WorkflowTransition; id, wid string)
		if err := rows.Scan(&id, &wid, &t.FromStatus, &t.ToStatus); err != nil { return nil, err }
		t.ID, err = uuid.Parse(id)
		if err != nil { return nil, err }
		t.WorkflowID, err = uuid.Parse(wid)
		if err != nil { return nil, err }
		result = append(result, &t)
	}
	return result, rows.Err()
}

func (r *WorkflowRepo) Create(ctx context.Context, wf *domain.Workflow) error {
	statusesJSON, err := json.Marshal(wf.Statuses)
	if err != nil { return err }
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO workflows (id, project_id, name, statuses) VALUES (?, ?, ?, ?)`,
		wf.ID.String(), wf.ProjectID.String(), wf.Name, string(statusesJSON))
	return err
}

func (r *WorkflowRepo) AddTransition(ctx context.Context, t *domain.WorkflowTransition) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO workflow_transitions (id, workflow_id, from_status, to_status) VALUES (?, ?, ?, ?)`,
		t.ID.String(), t.WorkflowID.String(), t.FromStatus, t.ToStatus)
	return err
}
```

**Line-by-line walkthrough of the important parts:**

**`WorkflowRepo` struct and constructor (lines 12-18):**
Same pattern as `TagRepo` and every other repo — holds `*sql.DB`, constructor returns `*WorkflowRepo`.

**`GetByProjectAndName` method (lines 20-35):**
This is the most interesting method because it involves JSON deserialization. Step by step:

1. Declare local variables: `wf` for the result, `id` and `pid` as strings (UUIDs stored as text), `statusesJSON` as a string (the JSON text from the database).
2. Execute a `SELECT` with two WHERE conditions: `project_id = ? AND name = ?`. The `workflows` table has a `UNIQUE(project_id, name)` constraint, so at most one row matches.
3. `Scan` reads the four columns into our local variables. If no row matches, `sql.ErrNoRows` is returned — we convert it to `domain.ErrNotFound`.
4. Parse `id` and `pid` from strings into `uuid.UUID` values.
5. **The JSON step:** `json.Unmarshal([]byte(statusesJSON), &wf.Statuses)` takes the raw JSON string (e.g., `'["pending","active","completed","deleted"]'`) and parses it into `wf.Statuses` which is `[]string`. The `[]byte()` conversion is needed because `json.Unmarshal` takes `[]byte`, not `string`.

**`GetTransitions` method (lines 37-52):**
A multi-row query that returns all transitions for a workflow. The scanning is straightforward — 4 columns, two of which are UUID strings that need parsing. Note that `FromStatus` and `ToStatus` are plain strings, so they scan directly into the struct fields without any conversion.

**`Create` method (lines 54-61):**
The reverse of `GetByProjectAndName`'s JSON handling:
1. `json.Marshal(wf.Statuses)` converts `[]string{"backlog", "in_progress", "done"}` into `[]byte(`["backlog","in_progress","done"]`)`.
2. If `json.Marshal` fails (which is extremely unlikely for a `[]string`, but we check anyway), we return the error.
3. `string(statusesJSON)` converts the `[]byte` to a `string` for the SQL parameter.

**`AddTransition` method (lines 63-68):**
A simple INSERT into `workflow_transitions`. The `UNIQUE(workflow_id, from_status, to_status)` constraint prevents duplicate transitions — if you try to add the same from/to pair twice for the same workflow, SQLite returns a constraint violation error.

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/sqlite/`

Expected: No errors.

- [ ] **Step 3: Run the workflow tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run TestWorkflow`

Expected output (all 5 tests pass):

```
=== RUN   TestWorkflowGetByProjectAndName
--- PASS: TestWorkflowGetByProjectAndName
=== RUN   TestWorkflowGetByProjectAndNameNotFound
--- PASS: TestWorkflowGetByProjectAndNameNotFound
=== RUN   TestWorkflowGetTransitions
--- PASS: TestWorkflowGetTransitions
=== RUN   TestWorkflowCreate
--- PASS: TestWorkflowCreate
=== RUN   TestWorkflowAddTransition
--- PASS: TestWorkflowAddTransition
PASS
```

If any test fails, read the error message carefully. Common issues:
- **"expected 4 statuses, got 0"** — `json.Unmarshal` may not be receiving the correct JSON string. Print `statusesJSON` to debug.
- **"expected 6, got 0"** in `TestWorkflowGetTransitions` — the migration seed data may not have inserted the transitions. Check your migration SQL files.
- **"FOREIGN KEY constraint failed"** in `TestWorkflowCreate` — the project was not created before the workflow. Make sure `projRepo.Create` runs first.

- [ ] **Step 4: Commit**

```bash
git add internal/sqlite/workflow.go internal/sqlite/workflow_test.go
git commit -m "feat(sqlite): implement WorkflowRepo with JSON column handling"
```

---

### Final Verification

This is the most important step. We run the **entire** test suite for the `internal/sqlite/` package to make sure all tests from all 7 phases pass together.

- [ ] **Step 1: Run ALL sqlite tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -count=1`

The `-v` flag shows each test name and its result. The `-count=1` flag disables test caching so every test runs fresh.

Expected: Every test from every phase passes. You should see output similar to:

```
=== RUN   TestStore...
--- PASS: TestStore...
=== RUN   TestProject...
--- PASS: TestProject...
=== RUN   TestTaskCreate
--- PASS: TestTaskCreate
=== RUN   TestTaskCreateWithNullables
--- PASS: TestTaskCreateWithNullables
... (all task CRUD tests) ...
=== RUN   TestTaskList...
--- PASS: TestTaskList...
... (all task list/filter tests from Phase 4) ...
... (all tree query tests from Phase 5) ...
... (all relation tests from Phase 6) ...
=== RUN   TestTagCreate
--- PASS: TestTagCreate
=== RUN   TestTagCreateNullColor
--- PASS: TestTagCreateNullColor
=== RUN   TestTagGetByNameNotFound
--- PASS: TestTagGetByNameNotFound
=== RUN   TestTagList
--- PASS: TestTagList
=== RUN   TestTagAssignToTask
--- PASS: TestTagAssignToTask
=== RUN   TestTagRemoveFromTask
--- PASS: TestTagRemoveFromTask
=== RUN   TestTagGetTaskTagsEmpty
--- PASS: TestTagGetTaskTagsEmpty
=== RUN   TestTagFilterIntegration
--- PASS: TestTagFilterIntegration
=== RUN   TestWorkflowGetByProjectAndName
--- PASS: TestWorkflowGetByProjectAndName
=== RUN   TestWorkflowGetByProjectAndNameNotFound
--- PASS: TestWorkflowGetByProjectAndNameNotFound
=== RUN   TestWorkflowGetTransitions
--- PASS: TestWorkflowGetTransitions
=== RUN   TestWorkflowCreate
--- PASS: TestWorkflowCreate
=== RUN   TestWorkflowAddTransition
--- PASS: TestWorkflowAddTransition
PASS
ok  	github.com/germanamz/tusk/internal/sqlite
```

If any test fails, do NOT move on. Fix the issue and re-run.

- [ ] **Step 2: Run the full project test suite**

Run: `cd /Users/germanamz/projects/tusk && go test ./... -count=1`

This runs tests in ALL packages, not just `internal/sqlite/`. Everything should pass with no regressions.

- [ ] **Step 3: Run vet**

Run: `cd /Users/germanamz/projects/tusk && go vet ./internal/sqlite/`

Expected: No output (clean). `go vet` catches common mistakes like unreachable code, incorrect format strings, or passing the wrong types to `fmt.Printf`.

- [ ] **Step 4: Confirm the interfaces are satisfied**

The compile-time checks in the test files guarantee interface satisfaction:

```go
// In tag_test.go:
var _ repository.TagRepository = (*TagRepo)(nil)

// In workflow_test.go:
var _ repository.WorkflowRepository = (*WorkflowRepo)(nil)
```

If these lines compile (which they do if the tests run), both repos implement their full interfaces.

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "feat(sqlite): complete Phase 7 — TagRepo and WorkflowRepo"
```

---

## Phase Complete: The Entire SQLite Layer is Done

Congratulations! With Phase 7 complete, every repository interface defined in `internal/repository/` now has a working SQLite implementation in `internal/sqlite/`. Here is a summary of everything that was built across all 7 phases:

| Phase | File(s) | Interface Implemented | Methods |
|-------|---------|----------------------|---------|
| 1 | `store.go`, `store_test.go` | *(infrastructure)* | `New`, `DB`, `Close`, helpers |
| 2 | `project.go` | `ProjectRepository` | `Create`, `GetByName`, `List` |
| 3 | `task.go` | `TaskRepository` (CRUD) | `Create`, `GetByID`, `GetByShortID`, `Update`, `Delete` |
| 4 | `task.go` | `TaskRepository` (List) | `List` with tag/status/priority filters |
| 5 | `task.go` | `TaskRepository` (Trees) | `GetChildren`, `GetDescendants` |
| 6 | `relation.go` | `RelationRepository` | `Create`, `GetByTask`, `Delete` |
| **7** | **`tag.go`** | **`TagRepository`** | **`Create`, `GetByName`, `List`, `AssignToTask`, `RemoveFromTask`, `GetTaskTags`** |
| **7** | **`workflow.go`** | **`WorkflowRepository`** | **`GetByProjectAndName`, `GetTransitions`, `Create`, `AddTransition`** |

**All 6 repository interfaces are now fully implemented:**

1. `ProjectRepository` — projects
2. `TaskRepository` — tasks with CRUD, filtering, and tree queries
3. `TagRepository` — tags with many-to-many task assignments
4. `WorkflowRepository` — workflows with JSON statuses and transitions
5. `RelationRepository` — task-to-task relations (blocks, depends-on)
6. *(Store itself)* — database lifecycle management

The SQLite persistence layer is production-ready. Future work will build service and CLI layers on top of these repositories.
