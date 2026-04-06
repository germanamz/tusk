# Urgency Scoring — Phase 1: Engine Core & Config

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the urgency scoring engine with all 10 factors, expand the config with new weight fields, add `Urgency` field to `domain.Task`, and add batch count repository methods — without changing any consumer (TUI, MCP, or TaskService.List).

**Architecture:** The `UrgencyEngine` is a stateless scorer in `internal/service/urgency.go`. It receives pre-loaded batch data via a `ScoringContext` and stamps `task.Urgency` in-place. Config expands with 5 new weight fields. Repository interfaces gain batch count methods. All existing behavior is unchanged — no consumer calls the engine yet.

**Tech Stack:** Go, SQLite

**Prerequisites:** None. This phase builds on the base codebase with no prior phases required.

---

### Task 1: Expand UrgencyConfig and default.toml

**Files:**
- Modify: `internal/config/config.go` (lines 77-84, the `UrgencyConfig` struct)
- Modify: `internal/config/default.toml` (lines 20-25, the `[urgency]` section)

- [ ] **Step 1: Add 5 new weight fields to UrgencyConfig**

In `internal/config/config.go`, replace the `UrgencyConfig` struct (lines 77-84) with:

```go
// UrgencyConfig holds weights for the urgency scoring algorithm.
type UrgencyConfig struct {
	PriorityWeight    float64 `mapstructure:"priority_weight"`
	DueWeight         float64 `mapstructure:"due_weight"`
	AgeWeight         float64 `mapstructure:"age_weight"`
	ActiveWeight      float64 `mapstructure:"active_weight"`
	BlockingWeight    float64 `mapstructure:"blocking_weight"`
	BlockedWeight     float64 `mapstructure:"blocked_weight"`
	TagsWeight        float64 `mapstructure:"tags_weight"`
	ProjectWeight     float64 `mapstructure:"project_weight"`
	AnnotationsWeight float64 `mapstructure:"annotations_weight"`
	WaitingWeight     float64 `mapstructure:"waiting_weight"`
}
```

- [ ] **Step 2: Update default.toml with new weight defaults**

In `internal/config/default.toml`, replace lines 20-25 with:

```toml
[urgency]
priority_weight    = 6.0    # Weight for task priority in urgency score
due_weight         = 12.0   # Weight for due date proximity
age_weight         = 2.0    # Weight for task age
active_weight      = 4.0    # Weight for active status
blocking_weight    = 8.0    # Weight for tasks that block others
blocked_weight     = -5.0   # Weight for tasks that are blocked
tags_weight        = 1.0    # Weight for tag count
project_weight     = 1.0    # Weight for having a project
annotations_weight = 1.0    # Weight for annotation count
waiting_weight     = -3.0   # Weight for waiting tasks
```

- [ ] **Step 3: Verify compilation**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`
Expected: Clean compilation with no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go internal/config/default.toml
git commit -m "feat(config): add urgency weight fields for all 10 scoring factors"
```

---

### Task 2: Add Urgency field to domain.Task and JSON serialization types

**Files:**
- Modify: `internal/domain/task.go` (lines 11-27, the `Task` struct)
- Modify: `internal/tui/render.go` (lines 231-250, the `taskJSON` struct; lines 252-284, `toTaskJSON`)
- Modify: `internal/mcp/tools.go` (lines 29-47, the `taskResponse` struct; lines 49-81, `toTaskResponse`)

- [ ] **Step 1: Add Urgency field to domain.Task**

In `internal/domain/task.go`, add the `Urgency` field after `ModifiedAt` (line 27):

```go
type Task struct {
	ID             uuid.UUID
	ShortID        string
	ParentID       *uuid.UUID
	ProjectID      string
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
	Urgency        float64 // Computed at read time, not persisted in DB.
}
```

- [ ] **Step 2: Add urgency to TUI taskJSON struct**

In `internal/tui/render.go`, add `Urgency` to the `taskJSON` struct (after `ModifiedAt` on line 249):

```go
type taskJSON struct {
	ID             string         `json:"id"`
	ShortID        string         `json:"short_id"`
	ParentID       *string        `json:"parent_id,omitempty"`
	ProjectID      string         `json:"project_id"`
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	Status         string         `json:"status"`
	Priority       int            `json:"priority"`
	Version        int            `json:"version"`
	Tags           []string       `json:"tags"`
	DueAt          *string        `json:"due_at,omitempty"`
	WaitUntil      *string        `json:"wait_until,omitempty"`
	RecurrenceRule *string        `json:"recurrence_rule,omitempty"`
	UDA            map[string]any `json:"uda,omitempty"`
	CreatedAt      string         `json:"created_at"`
	ModifiedAt     string         `json:"modified_at"`
	Urgency        float64        `json:"urgency"`
}
```

And in `toTaskJSON` (around line 252), add after the `ModifiedAt` assignment:

```go
tj.Urgency = t.Urgency
```

- [ ] **Step 3: Add urgency to MCP taskResponse struct**

In `internal/mcp/tools.go`, add `Urgency` to the `taskResponse` struct (after `ModifiedAt` on line 46):

```go
type taskResponse struct {
	ID             string         `json:"id"`
	ShortID        string         `json:"short_id"`
	ParentID       *string        `json:"parent_id,omitempty"`
	ProjectID      string         `json:"project_id"`
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	Status         string         `json:"status"`
	Priority       int            `json:"priority"`
	Version        int            `json:"version"`
	Tags           []string       `json:"tags"`
	DueAt          *string        `json:"due_at,omitempty"`
	WaitUntil      *string        `json:"wait_until,omitempty"`
	RecurrenceRule *string        `json:"recurrence_rule,omitempty"`
	UDA            map[string]any `json:"uda,omitempty"`
	CreatedAt      string         `json:"created_at"`
	ModifiedAt     string         `json:"modified_at"`
	Urgency        float64        `json:"urgency"`
}
```

And in `toTaskResponse` (around line 49), add after the `ModifiedAt` assignment:

```go
r.Urgency = t.Urgency
```

- [ ] **Step 4: Verify compilation and run existing tests**

Run: `cd /Users/germanamz/projects/tusk && go build ./... && go test ./...`
Expected: Clean compilation. Existing tests pass. The `Urgency` field is zero-valued everywhere since nothing computes it yet.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/task.go internal/tui/render.go internal/mcp/tools.go
git commit -m "feat(domain): add Urgency field to Task and JSON serialization types"
```

---

### Task 3: Add batch count methods to repository interfaces and SQLite implementations

**Files:**
- Modify: `internal/repository/annotation.go` (lines 10-14)
- Modify: `internal/repository/relation.go` (lines 10-18)
- Modify: `internal/sqlite/annotation.go` (after line 109)
- Modify: `internal/sqlite/relation.go` (after line 198)

- [ ] **Step 1: Write tests for batch annotation count**

Create `internal/sqlite/annotation_count_test.go`. Since the SQLite tests need a database, write a test that:

```go
// In internal/sqlite/annotation_count_test.go
package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
	"github.com/google/uuid"
)

func TestAnnotationRepo_CountByTasks(t *testing.T) {
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	db := store.DB()

	taskRepo := sqlite.NewTaskRepo(db)
	annRepo := sqlite.NewAnnotationRepo(db)
	ctx := context.Background()

	// Create two tasks
	t1 := &domain.Task{
		ID: uuid.New(), ShortID: "aaaaaaaa", ProjectID: "default",
		Title: "Task 1", Status: "pending", Version: 1,
		UDA: map[string]any{},
		CreatedAt: time.Now().UTC(), ModifiedAt: time.Now().UTC(),
	}
	t2 := &domain.Task{
		ID: uuid.New(), ShortID: "bbbbbbbb", ProjectID: "default",
		Title: "Task 2", Status: "pending", Version: 1,
		UDA: map[string]any{},
		CreatedAt: time.Now().UTC(), ModifiedAt: time.Now().UTC(),
	}
	if err := taskRepo.Create(ctx, t1); err != nil {
		t.Fatal(err)
	}
	if err := taskRepo.Create(ctx, t2); err != nil {
		t.Fatal(err)
	}

	// Add 2 annotations to t1, 1 to t2
	for _, ann := range []*domain.Annotation{
		{ID: uuid.New(), TaskID: t1.ID, Body: "note 1", CreatedAt: time.Now().UTC()},
		{ID: uuid.New(), TaskID: t1.ID, Body: "note 2", CreatedAt: time.Now().UTC()},
		{ID: uuid.New(), TaskID: t2.ID, Body: "note 3", CreatedAt: time.Now().UTC()},
	} {
		if err := annRepo.Create(ctx, ann); err != nil {
			t.Fatal(err)
		}
	}

	counts, err := annRepo.CountByTasks(ctx, []uuid.UUID{t1.ID, t2.ID})
	if err != nil {
		t.Fatal(err)
	}
	if counts[t1.ID] != 2 {
		t.Fatalf("expected 2 annotations for t1, got %d", counts[t1.ID])
	}
	if counts[t2.ID] != 1 {
		t.Fatalf("expected 1 annotation for t2, got %d", counts[t2.ID])
	}

	// Empty input returns empty map
	empty, err := annRepo.CountByTasks(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty map, got %v", empty)
	}
}
```

- [ ] **Step 2: Add CountByTasks to AnnotationRepository interface**

In `internal/repository/annotation.go`, add the new method:

```go
type AnnotationRepository interface {
	Create(ctx context.Context, ann *domain.Annotation) error
	GetByTask(ctx context.Context, taskID uuid.UUID) ([]*domain.Annotation, error)
	Delete(ctx context.Context, id uuid.UUID) error
	CountByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int, error)
}
```

- [ ] **Step 3: Implement CountByTasks in SQLite AnnotationRepo**

In `internal/sqlite/annotation.go`, add after the `Delete` method:

```go
// CountByTasks returns annotation counts for each task ID in a single query.
// Tasks with zero annotations are not included in the returned map.
func (r *AnnotationRepo) CountByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	if len(taskIDs) == 0 {
		return map[uuid.UUID]int{}, nil
	}

	placeholders := make([]string, len(taskIDs))
	args := make([]any, len(taskIDs))
	for i, id := range taskIDs {
		placeholders[i] = "?"
		args[i] = id.String()
	}

	query := fmt.Sprintf(
		`SELECT task_id, COUNT(*) FROM annotations WHERE task_id IN (%s) GROUP BY task_id`,
		strings.Join(placeholders, ","),
	)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[uuid.UUID]int)
	for rows.Next() {
		var idStr string
		var count int
		if err := rows.Scan(&idStr, &count); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return nil, fmt.Errorf("parsing task_id %q: %w", idStr, err)
		}
		counts[id] = count
	}
	return counts, rows.Err()
}
```

You will need to add `"fmt"` and `"strings"` to the imports in `internal/sqlite/annotation.go` if not already present.

- [ ] **Step 4: Run the annotation count test**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/sqlite/ -run TestAnnotationRepo_CountByTasks`
Expected: PASS

- [ ] **Step 5: Write tests for batch relation counts**

Create `internal/sqlite/relation_count_test.go`:

```go
package sqlite_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
	"github.com/google/uuid"
)

func TestRelationRepo_CountBlockingByTasks(t *testing.T) {
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	db := store.DB()

	taskRepo := sqlite.NewTaskRepo(db)
	relRepo := sqlite.NewRelationRepo(db)
	ctx := context.Background()

	// Create 3 tasks: A blocks B and C
	tasks := make([]*domain.Task, 3)
	for i := range tasks {
		tasks[i] = &domain.Task{
			ID: uuid.New(), ShortID: fmt.Sprintf("%08d", i), ProjectID: "default",
			Title: fmt.Sprintf("Task %d", i), Status: "pending", Version: 1,
			UDA: map[string]any{},
			CreatedAt: time.Now().UTC(), ModifiedAt: time.Now().UTC(),
		}
		if err := taskRepo.Create(ctx, tasks[i]); err != nil {
			t.Fatal(err)
		}
	}

	// A blocks B
	if err := relRepo.Create(ctx, &domain.Relation{
		ID: uuid.New(), SourceID: tasks[0].ID, TargetID: tasks[1].ID,
		RelationType: "blocks", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	// A blocks C
	if err := relRepo.Create(ctx, &domain.Relation{
		ID: uuid.New(), SourceID: tasks[0].ID, TargetID: tasks[2].ID,
		RelationType: "blocks", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	ids := []uuid.UUID{tasks[0].ID, tasks[1].ID, tasks[2].ID}

	// CountBlockingByTasks: A blocks 2, B blocks 0, C blocks 0
	blocking, err := relRepo.CountBlockingByTasks(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	if blocking[tasks[0].ID] != 2 {
		t.Fatalf("expected A blocking 2, got %d", blocking[tasks[0].ID])
	}
	if blocking[tasks[1].ID] != 0 {
		t.Fatalf("expected B blocking 0, got %d", blocking[tasks[1].ID])
	}

	// CountBlockedByTasks: A blocked by 0, B blocked by 1, C blocked by 1
	blockedBy, err := relRepo.CountBlockedByTasks(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	if blockedBy[tasks[1].ID] != 1 {
		t.Fatalf("expected B blocked_by 1, got %d", blockedBy[tasks[1].ID])
	}
	if blockedBy[tasks[2].ID] != 1 {
		t.Fatalf("expected C blocked_by 1, got %d", blockedBy[tasks[2].ID])
	}

	// Empty input returns empty map
	empty, err := relRepo.CountBlockingByTasks(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty map, got %v", empty)
	}
}
```

You will need `"fmt"` in the imports.

- [ ] **Step 6: Add batch count methods to RelationRepository interface**

In `internal/repository/relation.go`, add after `Exists`:

```go
type RelationRepository interface {
	Create(ctx context.Context, rel *domain.Relation) error
	Delete(ctx context.Context, id uuid.UUID) error
	DeleteByFields(ctx context.Context, sourceID, targetID uuid.UUID, relType string) error
	GetByTask(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error)
	GetBlocking(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error)
	GetBlockedBy(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error)
	Exists(ctx context.Context, sourceID, targetID uuid.UUID, relType string) (bool, error)
	CountBlockingByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int, error)
	CountBlockedByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int, error)
}
```

- [ ] **Step 7: Implement batch count methods in SQLite RelationRepo**

In `internal/sqlite/relation.go`, add after the `Exists` method (before `scanRelation`):

```go
// CountBlockingByTasks returns, for each task ID, how many other tasks it blocks.
// Tasks that block nothing are not included in the returned map.
func (r *RelationRepo) CountBlockingByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	return r.countRelationsByTasks(ctx, taskIDs, "source_id")
}

// CountBlockedByTasks returns, for each task ID, how many other tasks block it.
// Tasks that are not blocked are not included in the returned map.
func (r *RelationRepo) CountBlockedByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	return r.countRelationsByTasks(ctx, taskIDs, "target_id")
}

// countRelationsByTasks is a shared helper for counting blocking relations.
// column is either "source_id" (for blocking count) or "target_id" (for blocked-by count).
func (r *RelationRepo) countRelationsByTasks(ctx context.Context, taskIDs []uuid.UUID, column string) (map[uuid.UUID]int, error) {
	if len(taskIDs) == 0 {
		return map[uuid.UUID]int{}, nil
	}

	placeholders := make([]string, len(taskIDs))
	args := make([]any, len(taskIDs))
	for i, id := range taskIDs {
		placeholders[i] = "?"
		args[i] = id.String()
	}

	query := fmt.Sprintf(
		`SELECT %s, COUNT(*) FROM relations WHERE %s IN (%s) AND relation_type = 'blocks' GROUP BY %s`,
		column, column, strings.Join(placeholders, ","), column,
	)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[uuid.UUID]int)
	for rows.Next() {
		var idStr string
		var count int
		if err := rows.Scan(&idStr, &count); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return nil, fmt.Errorf("parsing id %q: %w", idStr, err)
		}
		counts[id] = count
	}
	return counts, rows.Err()
}
```

You will need to add `"fmt"` and `"strings"` to the imports in `internal/sqlite/relation.go` if not already present.

- [ ] **Step 8: Run the relation count tests**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/sqlite/ -run TestRelationRepo_CountBlockingByTasks`
Expected: PASS

- [ ] **Step 9: Verify full compilation and all tests pass**

Run: `cd /Users/germanamz/projects/tusk && go build ./... && go test ./...`
Expected: All tests pass.

- [ ] **Step 10: Commit**

```bash
git add internal/repository/annotation.go internal/repository/relation.go \
       internal/sqlite/annotation.go internal/sqlite/relation.go \
       internal/sqlite/annotation_count_test.go internal/sqlite/relation_count_test.go
git commit -m "feat(repository): add batch count methods for annotations and relations"
```

---

### Task 4: Implement UrgencyEngine with all 10 factors

**Files:**
- Create: `internal/service/urgency.go`
- Create: `internal/service/urgency_test.go`

- [ ] **Step 1: Write tests for each urgency factor**

Create `internal/service/urgency_test.go`:

```go
package service

import (
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

func defaultWeights() UrgencyWeights {
	return UrgencyWeights{
		Priority:    6.0,
		Due:         12.0,
		Age:         2.0,
		Active:      4.0,
		Blocking:    8.0,
		Blocked:     -5.0,
		Tags:        1.0,
		Project:     1.0,
		Annotations: 1.0,
		Waiting:     -3.0,
	}
}

func emptyContext() ScoringContext {
	return ScoringContext{
		BlockingCount:   map[uuid.UUID]int{},
		BlockedByCount:  map[uuid.UUID]int{},
		AnnotationCount: map[uuid.UUID]int{},
		TagCount:        map[uuid.UUID]int{},
		ProjectWeights:  map[string]*UrgencyWeights{},
	}
}

func TestUrgencyPriorityFactor(t *testing.T) {
	engine := NewUrgencyEngine(defaultWeights())
	ctx := emptyContext()

	tests := []struct {
		priority int
		want     float64
	}{
		{0, 0.0},
		{1, 1.5},
		{2, 3.0},
		{3, 4.5},
		{4, 6.0},
	}
	for _, tt := range tests {
		task := &domain.Task{ID: uuid.New(), Priority: tt.priority, Status: "pending", CreatedAt: time.Now()}
		got := engine.Score(task, ctx)
		// Score includes age and project factors too, so extract priority contribution
		baseline := engine.Score(&domain.Task{ID: uuid.New(), Priority: 0, Status: "pending", CreatedAt: task.CreatedAt}, ctx)
		contrib := got - baseline
		if diff := contrib - tt.want; diff > 0.01 || diff < -0.01 {
			t.Errorf("priority %d: got contribution %.2f, want %.2f", tt.priority, contrib, tt.want)
		}
	}
}

func TestUrgencyDueDateFactor(t *testing.T) {
	engine := NewUrgencyEngine(defaultWeights())
	ctx := emptyContext()
	now := time.Now()
	created := now.Add(-24 * time.Hour)

	// No due date: contribution is 0
	noDue := &domain.Task{ID: uuid.New(), Status: "pending", CreatedAt: created}
	baseScore := engine.Score(noDue, ctx)

	// Past due: high contribution
	pastDue := time.Now().Add(-48 * time.Hour)
	past := &domain.Task{ID: uuid.New(), Status: "pending", CreatedAt: created, DueAt: &pastDue}
	pastScore := engine.Score(past, ctx)
	if pastScore-baseScore < 10.0 {
		t.Errorf("past due contribution too low: %.2f", pastScore-baseScore)
	}

	// Due in 30 days: low contribution
	far := time.Now().Add(30 * 24 * time.Hour)
	farTask := &domain.Task{ID: uuid.New(), Status: "pending", CreatedAt: created, DueAt: &far}
	farScore := engine.Score(farTask, ctx)
	if farScore-baseScore > 3.0 {
		t.Errorf("far due contribution too high: %.2f", farScore-baseScore)
	}
}

func TestUrgencyActiveFactor(t *testing.T) {
	engine := NewUrgencyEngine(defaultWeights())
	ctx := emptyContext()
	created := time.Now()

	pending := &domain.Task{ID: uuid.New(), Status: "pending", CreatedAt: created}
	active := &domain.Task{ID: uuid.New(), Status: "active", CreatedAt: created}

	diff := engine.Score(active, ctx) - engine.Score(pending, ctx)
	if diff < 3.9 || diff > 4.1 {
		t.Errorf("active factor: got diff %.2f, want ~4.0", diff)
	}
}

func TestUrgencyBlockingFactor(t *testing.T) {
	engine := NewUrgencyEngine(defaultWeights())
	id := uuid.New()
	ctx := emptyContext()
	ctxBlocking := emptyContext()
	ctxBlocking.BlockingCount[id] = 2

	task := &domain.Task{ID: id, Status: "pending", CreatedAt: time.Now()}
	diff := engine.Score(task, ctxBlocking) - engine.Score(task, ctx)
	if diff < 7.9 || diff > 8.1 {
		t.Errorf("blocking factor: got diff %.2f, want ~8.0", diff)
	}
}

func TestUrgencyBlockedFactor(t *testing.T) {
	engine := NewUrgencyEngine(defaultWeights())
	id := uuid.New()
	ctx := emptyContext()
	ctxBlocked := emptyContext()
	ctxBlocked.BlockedByCount[id] = 1

	task := &domain.Task{ID: id, Status: "pending", CreatedAt: time.Now()}
	diff := engine.Score(task, ctxBlocked) - engine.Score(task, ctx)
	if diff > -4.9 || diff < -5.1 {
		t.Errorf("blocked factor: got diff %.2f, want ~-5.0", diff)
	}
}

func TestUrgencyWaitingFactor(t *testing.T) {
	engine := NewUrgencyEngine(defaultWeights())
	ctx := emptyContext()
	created := time.Now()

	noWait := &domain.Task{ID: uuid.New(), Status: "pending", CreatedAt: created}
	future := time.Now().Add(24 * time.Hour)
	waiting := &domain.Task{ID: uuid.New(), Status: "pending", CreatedAt: created, WaitUntil: &future}

	diff := engine.Score(waiting, ctx) - engine.Score(noWait, ctx)
	if diff > -2.9 || diff < -3.1 {
		t.Errorf("waiting factor: got diff %.2f, want ~-3.0", diff)
	}
}

func TestUrgencyScoreAndSort(t *testing.T) {
	engine := NewUrgencyEngine(defaultWeights())
	ctx := emptyContext()
	now := time.Now()

	high := &domain.Task{ID: uuid.New(), Priority: 4, Status: "active", CreatedAt: now}
	low := &domain.Task{ID: uuid.New(), Priority: 0, Status: "pending", CreatedAt: now}
	mid := &domain.Task{ID: uuid.New(), Priority: 2, Status: "pending", CreatedAt: now}

	tasks := []*domain.Task{low, mid, high}
	engine.ScoreAndSort(tasks, ctx)

	if tasks[0].ID != high.ID {
		t.Error("expected highest urgency task first")
	}
	if tasks[2].ID != low.ID {
		t.Error("expected lowest urgency task last")
	}
	for _, task := range tasks {
		if task.Urgency == 0 && task.Priority > 0 {
			t.Errorf("task %s: urgency should be non-zero", task.ShortID)
		}
	}
}

func TestUrgencyProjectWeightOverride(t *testing.T) {
	engine := NewUrgencyEngine(defaultWeights())

	overridePriority := 20.0
	ctx := ScoringContext{
		BlockingCount:   map[uuid.UUID]int{},
		BlockedByCount:  map[uuid.UUID]int{},
		AnnotationCount: map[uuid.UUID]int{},
		TagCount:        map[uuid.UUID]int{},
		ProjectWeights: map[string]*UrgencyWeights{
			"custom": {
				Priority:    overridePriority,
				Due:         12.0,
				Age:         2.0,
				Active:      4.0,
				Blocking:    8.0,
				Blocked:     -5.0,
				Tags:        1.0,
				Project:     1.0,
				Annotations: 1.0,
				Waiting:     -3.0,
			},
		},
	}

	defaultTask := &domain.Task{ID: uuid.New(), Priority: 4, Status: "pending", ProjectID: "default", CreatedAt: time.Now()}
	customTask := &domain.Task{ID: uuid.New(), Priority: 4, Status: "pending", ProjectID: "custom", CreatedAt: time.Now()}

	defaultScore := engine.Score(defaultTask, ctx)
	customScore := engine.Score(customTask, ctx)

	if customScore <= defaultScore {
		t.Errorf("custom project should score higher (%.2f) than default (%.2f)", customScore, defaultScore)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/service/ -run TestUrgency`
Expected: FAIL — `NewUrgencyEngine`, `UrgencyWeights`, `ScoringContext` not defined.

- [ ] **Step 3: Implement UrgencyEngine**

Replace the contents of `internal/service/urgency.go` with:

```go
package service

import (
	"math"
	"sort"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

// UrgencyWeights holds the weight for each scoring factor.
type UrgencyWeights struct {
	Priority    float64
	Due         float64
	Age         float64
	Active      float64
	Blocking    float64
	Blocked     float64
	Tags        float64
	Project     float64
	Annotations float64
	Waiting     float64
}

// ScoringContext holds preloaded batch data needed for urgency scoring.
// All maps use task ID as key. Tasks missing from a map are treated as zero.
type ScoringContext struct {
	BlockingCount   map[uuid.UUID]int
	BlockedByCount  map[uuid.UUID]int
	AnnotationCount map[uuid.UUID]int
	TagCount        map[uuid.UUID]int
	ProjectWeights  map[string]*UrgencyWeights // per-project weight overrides (fully merged)
}

// UrgencyEngine computes urgency scores for tasks.
type UrgencyEngine struct {
	defaults UrgencyWeights
}

// NewUrgencyEngine creates an engine with the given default weights.
func NewUrgencyEngine(defaults UrgencyWeights) *UrgencyEngine {
	return &UrgencyEngine{defaults: defaults}
}

// Score computes the urgency score for a single task.
func (e *UrgencyEngine) Score(task *domain.Task, ctx ScoringContext) float64 {
	w := e.weightsFor(task.ProjectID, ctx)

	var score float64

	// Priority: priority / 4.0 * weight
	if task.Priority > 0 {
		score += (float64(task.Priority) / 4.0) * w.Priority
	}

	// Due date: sigmoid curve
	if task.DueAt != nil {
		score += dueDateCoefficient(*task.DueAt) * w.Due
	}

	// Age: min(days / 365, 1.0) * weight
	age := time.Since(task.CreatedAt).Hours() / 24.0
	score += math.Min(age/365.0, 1.0) * w.Age

	// Active status
	if task.Status == "active" {
		score += w.Active
	}

	// Blocking
	if ctx.BlockingCount[task.ID] > 0 {
		score += w.Blocking
	}

	// Blocked
	if ctx.BlockedByCount[task.ID] > 0 {
		score += w.Blocked
	}

	// Tags
	tagCount := ctx.TagCount[task.ID]
	if tagCount > 0 {
		score += math.Min(float64(tagCount)/3.0, 1.0) * w.Tags
	}

	// Project
	if task.ProjectID != "" {
		score += w.Project
	}

	// Annotations
	annCount := ctx.AnnotationCount[task.ID]
	if annCount > 0 {
		score += math.Min(float64(annCount)/2.0, 1.0) * w.Annotations
	}

	// Waiting
	if task.WaitUntil != nil && task.WaitUntil.After(time.Now()) {
		score += w.Waiting
	}

	return score
}

// ScoreAndSort computes urgency for all tasks and sorts them descending.
func (e *UrgencyEngine) ScoreAndSort(tasks []*domain.Task, ctx ScoringContext) {
	for _, t := range tasks {
		t.Urgency = e.Score(t, ctx)
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Urgency > tasks[j].Urgency
	})
}

// weightsFor returns the effective weights for a task's project.
// If per-project overrides exist in the context, those are used; otherwise defaults.
func (e *UrgencyEngine) weightsFor(projectID string, ctx ScoringContext) UrgencyWeights {
	if pw, ok := ctx.ProjectWeights[projectID]; ok {
		return *pw
	}
	return e.defaults
}

// dueDateCoefficient returns a value between 0 and 1 based on how close the due date is.
// Uses a sigmoid curve: coefficient = 1 / (1 + e^(-k * (midpoint - days_until_due)))
// k = 0.5 (steepness), midpoint = 14 (days, inflection point).
// Past-due tasks approach 1.0. Far-future tasks approach 0.0.
func dueDateCoefficient(dueAt time.Time) float64 {
	daysUntilDue := time.Until(dueAt).Hours() / 24.0
	const k = 0.5
	const midpoint = 14.0
	return 1.0 / (1.0 + math.Exp(-k*(midpoint-daysUntilDue)))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/service/ -run TestUrgency`
Expected: All PASS.

- [ ] **Step 5: Run full test suite**

Run: `cd /Users/germanamz/projects/tusk && go test ./...`
Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/service/urgency.go internal/service/urgency_test.go
git commit -m "feat(service): implement UrgencyEngine with all 10 scoring factors"
```

---

## Changes Introduced

**New files:**
- `internal/service/urgency.go` — `UrgencyEngine`, `UrgencyWeights`, `ScoringContext`, `Score`, `ScoreAndSort`
- `internal/service/urgency_test.go` — Unit tests for all 10 factors, sorting, and project weight overrides
- `internal/sqlite/annotation_count_test.go` — Test for `CountByTasks`
- `internal/sqlite/relation_count_test.go` — Test for `CountBlockingByTasks` / `CountBlockedByTasks`

**Modified interfaces:**
- `repository.AnnotationRepository` — added `CountByTasks(ctx, []uuid.UUID) (map[uuid.UUID]int, error)`
- `repository.RelationRepository` — added `CountBlockingByTasks(ctx, []uuid.UUID) (map[uuid.UUID]int, error)` and `CountBlockedByTasks(ctx, []uuid.UUID) (map[uuid.UUID]int, error)`

**Modified types:**
- `domain.Task` — added `Urgency float64` field (zero-valued, not yet computed by any consumer)
- `config.UrgencyConfig` — added `ActiveWeight`, `TagsWeight`, `ProjectWeight`, `AnnotationsWeight`, `WaitingWeight`
- `tui.taskJSON` — added `Urgency float64` field (serialized as `"urgency"`)
- `mcp.taskResponse` — added `Urgency float64` field (serialized as `"urgency"`)

**Modified files:**
- `internal/config/default.toml` — 5 new weight defaults under `[urgency]`
- `internal/sqlite/annotation.go` — `CountByTasks` implementation
- `internal/sqlite/relation.go` — `CountBlockingByTasks`, `CountBlockedByTasks` implementations

**Bridge code:** None. The `UrgencyEngine` exists but is not yet called by `TaskService.List`. All urgency scores remain zero. No consumer behavior changes.

**User-visible behavior preserved:** All existing CLI commands, MCP tools, and E2E tests work identically. The `urgency` field appears in JSON output but is always `0.0` until Phase 2 wires the engine.
