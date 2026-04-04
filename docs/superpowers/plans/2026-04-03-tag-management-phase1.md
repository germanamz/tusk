# Tag Management Phase 1: Domain & Repository

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `ErrTagInUse` sentinel error, `TagWithUsage` domain type, and five new `TagRepository` methods (`GetByID`, `Update`, `Delete`, `CountTasksByTagID`, `ListWithUsage`) with full SQLite implementations and unit tests.

**Architecture:** Extend the existing repository interface and SQLite implementation. No service or CLI changes in this phase — just the data layer. All new methods follow existing patterns in `internal/sqlite/tag.go` (use `DBTX` interface, `scanTag` helper, `nullableString` for nullable columns, `domain.ErrNotFound` for missing rows).

**Tech Stack:** Go, SQLite, `github.com/google/uuid`

**Spec:** `docs/superpowers/specs/2026-04-03-tag-management-design.md`

---

### Task 1: Add `ErrTagInUse` and `TagWithUsage` to domain

**Files:**
- Modify: `internal/domain/errors.go:8-17`
- Modify: `internal/domain/tag.go:1-9`

- [ ] **Step 1: Add `ErrTagInUse` sentinel error**

Open `internal/domain/errors.go` and add the new error to the `var` block. Place it after `ErrDuplicateRelation`:

```go
ErrTagInUse = errors.New("tag is assigned to tasks")
```

The full `var` block should now be:

```go
var (
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("version conflict")
	ErrCyclicBlock       = errors.New("relation would create a cycle in blocks graph")
	ErrCyclicParent      = errors.New("parent would create a cycle in task hierarchy")
	ErrInvalidTransition = errors.New("status transition not allowed by workflow")
	ErrDuplicateRelation = errors.New("relation already exists")
	ErrTagInUse          = errors.New("tag is assigned to tasks")
	ErrSourceNotFound    = fmt.Errorf("source task: %w", ErrNotFound)
	ErrTargetNotFound    = fmt.Errorf("target task: %w", ErrNotFound)
)
```

- [ ] **Step 2: Add `TagWithUsage` struct**

Open `internal/domain/tag.go` and add the struct after the existing `Tag` type:

```go
// TagWithUsage pairs a Tag with the number of tasks it is assigned to.
type TagWithUsage struct {
	Tag       Tag
	TaskCount int
}
```

The full file should now be:

```go
package domain

import "github.com/google/uuid"

type Tag struct {
	ID    uuid.UUID
	Name  string
	Color *string
}

// TagWithUsage pairs a Tag with the number of tasks it is assigned to.
type TagWithUsage struct {
	Tag       Tag
	TaskCount int
}
```

- [ ] **Step 3: Run `go vet` to verify**

Run: `go vet ./internal/domain/...`
Expected: no output (clean)

- [ ] **Step 4: Commit**

```bash
git add internal/domain/errors.go internal/domain/tag.go
git commit -m "feat(domain): add ErrTagInUse and TagWithUsage type"
```

---

### Task 2: Extend TagRepository interface and SQLite implementation

**Files:**
- Modify: `internal/repository/tag.go:10-18`
- Modify: `internal/sqlite/tag.go`

- [ ] **Step 1: Add new methods to the `TagRepository` interface**

Open `internal/repository/tag.go`. Add these five methods inside the `TagRepository` interface, after the existing `GetTaskTagsBatch` method (line 17):

```go
GetByID(ctx context.Context, id uuid.UUID) (*domain.Tag, error)
Update(ctx context.Context, tag *domain.Tag) error
Delete(ctx context.Context, id uuid.UUID) error
CountTasksByTagID(ctx context.Context, id uuid.UUID) (int, error)
ListWithUsage(ctx context.Context) ([]domain.TagWithUsage, error)
```

The full interface should now be:

```go
type TagRepository interface {
	Create(ctx context.Context, tag *domain.Tag) error
	GetByName(ctx context.Context, name string) (*domain.Tag, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Tag, error)
	List(ctx context.Context) ([]*domain.Tag, error)
	ListWithUsage(ctx context.Context) ([]domain.TagWithUsage, error)
	Update(ctx context.Context, tag *domain.Tag) error
	Delete(ctx context.Context, id uuid.UUID) error
	CountTasksByTagID(ctx context.Context, id uuid.UUID) (int, error)
	AssignToTask(ctx context.Context, taskID, tagID uuid.UUID) error
	RemoveFromTask(ctx context.Context, taskID, tagID uuid.UUID) error
	GetTaskTags(ctx context.Context, taskID uuid.UUID) ([]*domain.Tag, error)
	GetTaskTagsBatch(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID][]*domain.Tag, error)
}
```

- [ ] **Step 2: Verify that the build fails**

Run: `go build ./...`
Expected: compilation error because `TagRepo` in `internal/sqlite/tag.go` no longer satisfies the `TagRepository` interface (missing the five new methods). The interface compliance check at `internal/sqlite/tag_test.go:12` (`var _ repository.TagRepository = (*TagRepo)(nil)`) will trigger this.

- [ ] **Step 3: Implement `GetByID` on `TagRepo`**

Open `internal/sqlite/tag.go`. Add this method after the existing `GetByName` method (after line 37):

```go
func (r *TagRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Tag, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, color FROM tags WHERE id = ?`, id.String())
	tag, err := scanTag(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return tag, err
}
```

This follows exactly the same pattern as `GetByName` (lines 29-37) — query a single row, use `scanTag`, map `sql.ErrNoRows` to `domain.ErrNotFound`.

- [ ] **Step 4: Implement `Update` on `TagRepo`**

Add this method after `GetByID`:

```go
func (r *TagRepo) Update(ctx context.Context, tag *domain.Tag) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tags SET name = ?, color = ? WHERE id = ?`,
		tag.Name, nullableString(tag.Color), tag.ID.String(),
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
```

This uses `nullableString` (defined in `internal/sqlite/store.go:245`) to handle the nullable `Color` field. It returns `domain.ErrNotFound` if no row matched the ID.

- [ ] **Step 5: Implement `Delete` on `TagRepo`**

Add this method after `Update`:

```go
func (r *TagRepo) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM tags WHERE id = ?`, id.String())
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
```

Same `RowsAffected` → `ErrNotFound` pattern used by `RemoveFromTask` (lines 63-78).

- [ ] **Step 6: Implement `CountTasksByTagID` on `TagRepo`**

Add this method after `Delete`:

```go
func (r *TagRepo) CountTasksByTagID(ctx context.Context, id uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tag_assignments WHERE tag_id = ?`, id.String(),
	).Scan(&count)
	return count, err
}
```

- [ ] **Step 7: Implement `ListWithUsage` on `TagRepo`**

Add this method after `CountTasksByTagID`:

```go
func (r *TagRepo) ListWithUsage(ctx context.Context) ([]domain.TagWithUsage, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT t.id, t.name, t.color, COUNT(ta.task_id)
		 FROM tags t
		 LEFT JOIN tag_assignments ta ON t.id = ta.tag_id
		 GROUP BY t.id, t.name, t.color`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]domain.TagWithUsage, 0)
	for rows.Next() {
		var (
			tw    domain.TagWithUsage
			id    string
			color sql.NullString
		)
		if err := rows.Scan(&id, &tw.Tag.Name, &color, &tw.TaskCount); err != nil {
			return nil, err
		}
		tw.Tag.ID, err = uuid.Parse(id)
		if err != nil {
			return nil, err
		}
		if color.Valid {
			tw.Tag.Color = &color.String
		}
		result = append(result, tw)
	}
	return result, rows.Err()
}
```

Note: we can't reuse `scanTag` here because we have an extra `COUNT` column. The scanning is done inline.

- [ ] **Step 8: Verify the build passes**

Run: `go build ./...`
Expected: clean build, no errors.

- [ ] **Step 9: Commit**

```bash
git add internal/repository/tag.go internal/sqlite/tag.go
git commit -m "feat(sqlite): implement GetByID, Update, Delete, CountTasksByTagID, ListWithUsage for tags"
```

---

### Task 3: Unit tests for new repository methods

**Files:**
- Modify: `internal/sqlite/tag_test.go`

All tests follow the existing patterns in this file. They use `testStore(t)` to get an in-memory SQLite store and `NewTagRepo(s.DB())` for the repo. Some tests need tasks — use `newTestTask()` and `mustCreateTask(t, taskRepo, task)` as done in the existing tests (e.g. line 84).

- [ ] **Step 1: Write test for `GetByID` — found**

Add to `internal/sqlite/tag_test.go`:

```go
func TestTagGetByID(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())
	ctx := context.Background()

	color := "#00ff00"
	tag := &domain.Tag{ID: uuid.New(), Name: "getbyid", Color: &color}
	if err := repo.Create(ctx, tag); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByID(ctx, tag.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "getbyid" {
		t.Fatalf("expected 'getbyid', got %q", got.Name)
	}
	if got.Color == nil || *got.Color != "#00ff00" {
		t.Fatalf("expected color '#00ff00', got %v", got.Color)
	}
}
```

- [ ] **Step 2: Write test for `GetByID` — not found**

```go
func TestTagGetByIDNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())

	_, err := repo.GetByID(context.Background(), uuid.New())
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 3: Write test for `Update` — name and color**

```go
func TestTagUpdate(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())
	ctx := context.Background()

	tag := &domain.Tag{ID: uuid.New(), Name: "old-name"}
	if err := repo.Create(ctx, tag); err != nil {
		t.Fatal(err)
	}

	newColor := "#abcdef"
	tag.Name = "new-name"
	tag.Color = &newColor
	if err := repo.Update(ctx, tag); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.GetByID(ctx, tag.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "new-name" {
		t.Fatalf("expected 'new-name', got %q", got.Name)
	}
	if got.Color == nil || *got.Color != "#abcdef" {
		t.Fatalf("expected color '#abcdef', got %v", got.Color)
	}
}
```

- [ ] **Step 4: Write test for `Update` — not found**

```go
func TestTagUpdateNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())

	tag := &domain.Tag{ID: uuid.New(), Name: "ghost"}
	err := repo.Update(context.Background(), tag)
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 5: Write test for `Delete`**

```go
func TestTagDelete(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())
	ctx := context.Background()

	tag := &domain.Tag{ID: uuid.New(), Name: "deleteme"}
	if err := repo.Create(ctx, tag); err != nil {
		t.Fatal(err)
	}

	if err := repo.Delete(ctx, tag.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := repo.GetByID(ctx, tag.ID)
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}
```

- [ ] **Step 6: Write test for `Delete` — not found**

```go
func TestTagDeleteNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())

	err := repo.Delete(context.Background(), uuid.New())
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 7: Write test for `CountTasksByTagID`**

```go
func TestTagCountTasksByTagID(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	tagRepo := NewTagRepo(s.DB())
	ctx := context.Background()

	tag := &domain.Tag{ID: uuid.New(), Name: "counted"}
	if err := tagRepo.Create(ctx, tag); err != nil {
		t.Fatal(err)
	}

	// No assignments yet
	count, err := tagRepo.CountTasksByTagID(ctx, tag.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}

	// Assign to two tasks
	t1 := newTestTask()
	t2 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)
	if err := tagRepo.AssignToTask(ctx, t1.ID, tag.ID); err != nil {
		t.Fatal(err)
	}
	if err := tagRepo.AssignToTask(ctx, t2.ID, tag.ID); err != nil {
		t.Fatal(err)
	}

	count, err = tagRepo.CountTasksByTagID(ctx, tag.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2, got %d", count)
	}
}
```

- [ ] **Step 8: Write test for `ListWithUsage`**

```go
func TestTagListWithUsage(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	tagRepo := NewTagRepo(s.DB())
	ctx := context.Background()

	color := "#ff0000"
	tag1 := &domain.Tag{ID: uuid.New(), Name: "used", Color: &color}
	tag2 := &domain.Tag{ID: uuid.New(), Name: "unused"}
	if err := tagRepo.Create(ctx, tag1); err != nil {
		t.Fatal(err)
	}
	if err := tagRepo.Create(ctx, tag2); err != nil {
		t.Fatal(err)
	}

	task := newTestTask()
	mustCreateTask(t, taskRepo, task)
	if err := tagRepo.AssignToTask(ctx, task.ID, tag1.ID); err != nil {
		t.Fatal(err)
	}

	results, err := tagRepo.ListWithUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(results))
	}

	byName := map[string]domain.TagWithUsage{}
	for _, tw := range results {
		byName[tw.Tag.Name] = tw
	}

	usedTW := byName["used"]
	if usedTW.TaskCount != 1 {
		t.Fatalf("expected 'used' task count 1, got %d", usedTW.TaskCount)
	}
	if usedTW.Tag.Color == nil || *usedTW.Tag.Color != "#ff0000" {
		t.Fatalf("expected color '#ff0000', got %v", usedTW.Tag.Color)
	}

	unusedTW := byName["unused"]
	if unusedTW.TaskCount != 0 {
		t.Fatalf("expected 'unused' task count 0, got %d", unusedTW.TaskCount)
	}
}
```

- [ ] **Step 9: Run all tag repo tests**

Run: `go test -v ./internal/sqlite/ -run TestTag`
Expected: all tests pass, including both old and new ones.

- [ ] **Step 10: Run full test suite to check for regressions**

Run: `make test`
Expected: all tests pass.

- [ ] **Step 11: Commit**

```bash
git add internal/sqlite/tag_test.go
git commit -m "test(sqlite): add unit tests for new TagRepo methods"
```
