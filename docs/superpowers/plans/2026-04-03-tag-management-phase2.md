# Tag Management Phase 2: Service Layer

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add five new `TagService` methods (`Create`, `Delete`, `Rename`, `Modify`, `ListWithUsage`) with full unit test coverage.

**Architecture:** Extend the existing `TagService` in `internal/service/tag.go`. All methods take tag names (not IDs) as inputs — the service resolves names to IDs via `tagRepo.GetByName`. Business rules: `Create` fails on duplicate names (`ErrConflict`), `Delete` fails if tag is assigned to tasks (`ErrTagInUse`), `Rename` fails if target name exists (`ErrConflict`).

**Tech Stack:** Go, `github.com/google/uuid`

**Spec:** `docs/superpowers/specs/2026-04-03-tag-management-design.md`

**Depends on:** Phase 1 (domain types + repository methods) must be complete.

---

### Task 1: Implement `TagService.Create`

**Files:**
- Modify: `internal/service/tag.go`
- Modify: `internal/service/tag_test.go`

- [ ] **Step 1: Write failing tests for `Create`**

Add these tests to `internal/service/tag_test.go`:

```go
func TestCreate_NewTag(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	tag, err := tagSvc.Create(ctx, "feature", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tag.Name != "feature" {
		t.Fatalf("expected name 'feature', got %q", tag.Name)
	}
	if tag.Color != nil {
		t.Fatalf("expected nil color, got %v", tag.Color)
	}
}

func TestCreate_WithColor(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	color := "#ff0000"
	tag, err := tagSvc.Create(ctx, "urgent", &color)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tag.Color == nil || *tag.Color != "#ff0000" {
		t.Fatalf("expected color '#ff0000', got %v", tag.Color)
	}
}

func TestCreate_Duplicate(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	if _, err := tagSvc.Create(ctx, "dup", nil); err != nil {
		t.Fatal(err)
	}

	_, err := tagSvc.Create(ctx, "dup", nil)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestCreate_EmptyName(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	_, err := tagSvc.Create(ctx, "", nil)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestCreate_WhitespaceName(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	_, err := tagSvc.Create(ctx, "   ", nil)
	if err == nil {
		t.Fatal("expected error for whitespace-only name")
	}
}
```

You will also need to add `"errors"` to the imports block in `tag_test.go` if it's not already there.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v ./internal/service/ -run TestCreate`
Expected: compilation error — `Create` method does not exist on `TagService`.

- [ ] **Step 3: Implement `Create`**

Open `internal/service/tag.go`. Add this method after the existing `FindOrCreate` method (after line 49):

```go
// Create explicitly creates a new tag with the given name and optional color.
// Unlike FindOrCreate, this fails with ErrConflict if the tag already exists.
func (s *TagService) Create(ctx context.Context, name string, color *string) (*domain.Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("tag name must not be empty")
	}

	_, err := s.tagRepo.GetByName(ctx, name)
	if err == nil {
		return nil, fmt.Errorf("tag %q already exists: %w", name, domain.ErrConflict)
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, fmt.Errorf("looking up tag %q: %w", name, err)
	}

	tag := &domain.Tag{
		ID:    uuid.New(),
		Name:  name,
		Color: color,
	}
	if err := s.tagRepo.Create(ctx, tag); err != nil {
		return nil, fmt.Errorf("creating tag %q: %w", name, err)
	}
	return tag, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -v ./internal/service/ -run TestCreate`
Expected: all 5 tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/service/tag.go internal/service/tag_test.go
git commit -m "feat(service): add TagService.Create with duplicate check"
```

---

### Task 2: Implement `TagService.Delete`

**Files:**
- Modify: `internal/service/tag.go`
- Modify: `internal/service/tag_test.go`

- [ ] **Step 1: Write failing tests for `Delete`**

Add these tests to `internal/service/tag_test.go`:

```go
func TestDelete_UnusedTag(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	if _, err := tagSvc.Create(ctx, "removable", nil); err != nil {
		t.Fatal(err)
	}

	if err := tagSvc.Delete(ctx, "removable"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify it's gone
	tags, err := tagSvc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 0 {
		t.Fatalf("expected 0 tags after delete, got %d", len(tags))
	}
}

func TestDelete_TagInUse(t *testing.T) {
	tagSvc, store := testTagEnv(t)
	ctx := context.Background()

	task := mustCreateTaskForTags(t, store)
	if err := tagSvc.AssignToTask(ctx, task.ID, []string{"busy"}); err != nil {
		t.Fatal(err)
	}

	err := tagSvc.Delete(ctx, "busy")
	if !errors.Is(err, domain.ErrTagInUse) {
		t.Fatalf("expected ErrTagInUse, got %v", err)
	}
}

func TestDelete_NotFound(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	err := tagSvc.Delete(ctx, "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v ./internal/service/ -run TestDelete`
Expected: compilation error — `Delete` method does not exist on `TagService`.

- [ ] **Step 3: Implement `Delete`**

Add this method to `internal/service/tag.go`, after the `Create` method:

```go
// Delete removes a tag by name. Returns ErrTagInUse if the tag is still
// assigned to any tasks. Returns ErrNotFound if the tag doesn't exist.
func (s *TagService) Delete(ctx context.Context, name string) error {
	tag, err := s.tagRepo.GetByName(ctx, name)
	if err != nil {
		return fmt.Errorf("looking up tag %q: %w", name, err)
	}

	count, err := s.tagRepo.CountTasksByTagID(ctx, tag.ID)
	if err != nil {
		return fmt.Errorf("counting tasks for tag %q: %w", name, err)
	}
	if count > 0 {
		return fmt.Errorf("tag %q is assigned to %d task(s): %w", name, count, domain.ErrTagInUse)
	}

	if err := s.tagRepo.Delete(ctx, tag.ID); err != nil {
		return fmt.Errorf("deleting tag %q: %w", name, err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -v ./internal/service/ -run TestDelete`
Expected: all 3 tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/service/tag.go internal/service/tag_test.go
git commit -m "feat(service): add TagService.Delete with in-use guard"
```

---

### Task 3: Implement `TagService.Rename`

**Files:**
- Modify: `internal/service/tag.go`
- Modify: `internal/service/tag_test.go`

- [ ] **Step 1: Write failing tests for `Rename`**

Add these tests to `internal/service/tag_test.go`:

```go
func TestRename_Success(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	if _, err := tagSvc.Create(ctx, "oldname", nil); err != nil {
		t.Fatal(err)
	}

	if err := tagSvc.Rename(ctx, "oldname", "newname"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	// Old name should not exist
	tags, err := tagSvc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(tags))
	}
	if tags[0].Name != "newname" {
		t.Fatalf("expected 'newname', got %q", tags[0].Name)
	}
}

func TestRename_Conflict(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	if _, err := tagSvc.Create(ctx, "aaa", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := tagSvc.Create(ctx, "bbb", nil); err != nil {
		t.Fatal(err)
	}

	err := tagSvc.Rename(ctx, "aaa", "bbb")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestRename_NotFound(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	err := tagSvc.Rename(ctx, "nonexistent", "whatever")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRename_EmptyNewName(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	if _, err := tagSvc.Create(ctx, "src", nil); err != nil {
		t.Fatal(err)
	}

	err := tagSvc.Rename(ctx, "src", "")
	if err == nil {
		t.Fatal("expected error for empty new name")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v ./internal/service/ -run TestRename`
Expected: compilation error — `Rename` method does not exist.

- [ ] **Step 3: Implement `Rename`**

Add this method to `internal/service/tag.go`, after the `Delete` method:

```go
// Rename changes a tag's name. Returns ErrNotFound if the old name doesn't
// exist, ErrConflict if the new name is already taken.
func (s *TagService) Rename(ctx context.Context, oldName, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return fmt.Errorf("new tag name must not be empty")
	}

	tag, err := s.tagRepo.GetByName(ctx, oldName)
	if err != nil {
		return fmt.Errorf("looking up tag %q: %w", oldName, err)
	}

	_, err = s.tagRepo.GetByName(ctx, newName)
	if err == nil {
		return fmt.Errorf("tag %q already exists: %w", newName, domain.ErrConflict)
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("checking tag %q: %w", newName, err)
	}

	tag.Name = newName
	if err := s.tagRepo.Update(ctx, tag); err != nil {
		return fmt.Errorf("renaming tag to %q: %w", newName, err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -v ./internal/service/ -run TestRename`
Expected: all 4 tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/service/tag.go internal/service/tag_test.go
git commit -m "feat(service): add TagService.Rename with conflict check"
```

---

### Task 4: Implement `TagService.Modify` and `TagService.ListWithUsage`

**Files:**
- Modify: `internal/service/tag.go`
- Modify: `internal/service/tag_test.go`

- [ ] **Step 1: Write failing tests for `Modify`**

Add these tests to `internal/service/tag_test.go`:

```go
func TestModify_SetColor(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	if _, err := tagSvc.Create(ctx, "plain", nil); err != nil {
		t.Fatal(err)
	}

	color := "#00ff00"
	tag, err := tagSvc.Modify(ctx, "plain", &color)
	if err != nil {
		t.Fatalf("Modify: %v", err)
	}
	if tag.Color == nil || *tag.Color != "#00ff00" {
		t.Fatalf("expected color '#00ff00', got %v", tag.Color)
	}
}

func TestModify_ClearColor(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	color := "#ff0000"
	if _, err := tagSvc.Create(ctx, "colored", &color); err != nil {
		t.Fatal(err)
	}

	tag, err := tagSvc.Modify(ctx, "colored", nil)
	if err != nil {
		t.Fatalf("Modify: %v", err)
	}
	if tag.Color != nil {
		t.Fatalf("expected nil color after clearing, got %v", tag.Color)
	}
}

func TestModify_NotFound(t *testing.T) {
	tagSvc, _ := testTagEnv(t)
	ctx := context.Background()

	color := "#aabbcc"
	_, err := tagSvc.Modify(ctx, "ghost", &color)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Write failing tests for `ListWithUsage`**

```go
func TestListWithUsage(t *testing.T) {
	tagSvc, store := testTagEnv(t)
	ctx := context.Background()

	if _, err := tagSvc.Create(ctx, "active", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := tagSvc.Create(ctx, "idle", nil); err != nil {
		t.Fatal(err)
	}

	task := mustCreateTaskForTags(t, store)
	if err := tagSvc.AssignToTask(ctx, task.ID, []string{"active"}); err != nil {
		t.Fatal(err)
	}

	results, err := tagSvc.ListWithUsage(ctx)
	if err != nil {
		t.Fatalf("ListWithUsage: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(results))
	}

	byName := map[string]domain.TagWithUsage{}
	for _, tw := range results {
		byName[tw.Tag.Name] = tw
	}
	if byName["active"].TaskCount != 1 {
		t.Fatalf("expected 'active' task count 1, got %d", byName["active"].TaskCount)
	}
	if byName["idle"].TaskCount != 0 {
		t.Fatalf("expected 'idle' task count 0, got %d", byName["idle"].TaskCount)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test -v ./internal/service/ -run "TestModify|TestListWithUsage"`
Expected: compilation error — `Modify` and `ListWithUsage` methods do not exist.

- [ ] **Step 4: Implement `Modify`**

Add this method to `internal/service/tag.go`, after the `Rename` method:

```go
// Modify updates a tag's color. Pass a non-nil pointer to set a color,
// or nil to clear it. Returns ErrNotFound if the tag doesn't exist.
func (s *TagService) Modify(ctx context.Context, name string, color *string) (*domain.Tag, error) {
	tag, err := s.tagRepo.GetByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("looking up tag %q: %w", name, err)
	}

	tag.Color = color
	if err := s.tagRepo.Update(ctx, tag); err != nil {
		return nil, fmt.Errorf("updating tag %q: %w", name, err)
	}
	return tag, nil
}
```

- [ ] **Step 5: Implement `ListWithUsage`**

Add this method after `Modify`:

```go
// ListWithUsage returns all tags with their task assignment counts.
func (s *TagService) ListWithUsage(ctx context.Context) ([]domain.TagWithUsage, error) {
	return s.tagRepo.ListWithUsage(ctx)
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test -v ./internal/service/ -run "TestModify|TestListWithUsage"`
Expected: all 4 tests pass.

- [ ] **Step 7: Run full test suite**

Run: `make test`
Expected: all tests pass, no regressions.

- [ ] **Step 8: Commit**

```bash
git add internal/service/tag.go internal/service/tag_test.go
git commit -m "feat(service): add TagService.Modify and ListWithUsage"
```
