# Filter Parser Phase 3: Resolver

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `Resolver` that converts a parsed `FilterSet` AST into a `domain.TaskFilter`, performing typed value parsing and repository lookups.

**Architecture:** The `Resolver` accepts repository interfaces (`ProjectRepository`, `TaskRepository`) via constructor injection. It iterates over the `FilterSet` fields and tags, parsing values into their typed representations and performing DB lookups where needed (project name -> UUID, short ID -> UUID). Like the parser, it collects all errors rather than failing fast.

**Tech Stack:** Go standard library + project domain/repository types. Module: `github.com/germanamz/tusk`.

**Spec:** `docs/superpowers/specs/2026-04-02-filter-syntax-parser-design.md`

**Depends on:** Phase 2 (AST, parser, validators) must be complete. The `internal/filter` package must have `ast.go` (with `FilterSet`, `FieldFilter`, `TagFilter`), `parser.go` (with `Parse`), and `validators.go` (with `parsePriorityValue`).

---

### Task 1: Date Parsing Helpers

**Files:**
- Create: `internal/filter/dates.go`
- Create: `internal/filter/dates_test.go`

These are moved/adapted from `internal/tui/filter.go` (`parseDate` function at lines 71-107) into the filter package with one addition: `thisweek` support and range parsing.

- [ ] **Step 1: Write the failing tests**

Create `internal/filter/dates_test.go`:

```go
package filter

import (
	"testing"
	"time"
)

func TestParseDate_RFC3339(t *testing.T) {
	got, err := parseDate("2026-04-10T15:30:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 4, 10, 15, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseDate_DateOnly(t *testing.T) {
	got, err := parseDate("2026-04-10")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseDate_Today(t *testing.T) {
	got, err := parseDate("today")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	now := time.Now().UTC()
	want := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseDate_Tomorrow(t *testing.T) {
	got, err := parseDate("tomorrow")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	now := time.Now().UTC().AddDate(0, 0, 1)
	want := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseDate_Thisweek(t *testing.T) {
	got, err := parseDate("thisweek")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	// thisweek should be the end of the current week (next Sunday 23:59:59)
	daysUntilSunday := int(time.Saturday - today.Weekday() + 1)
	if daysUntilSunday <= 0 {
		daysUntilSunday += 7
	}
	want := today.AddDate(0, 0, daysUntilSunday)
	if !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseDate_Weekday(t *testing.T) {
	got, err := parseDate("monday")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Weekday() != time.Monday {
		t.Fatalf("expected Monday, got %s", got.Weekday())
	}
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if got.Before(today) {
		t.Fatal("expected date in the future or today")
	}
}

func TestParseDate_Invalid(t *testing.T) {
	_, err := parseDate("notadate")
	if err == nil {
		t.Fatal("expected error for invalid date")
	}
}

func TestParseDateRange(t *testing.T) {
	start, end, err := parseDateRange("today..friday")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if !start.Equal(today) {
		t.Fatalf("expected start=%v, got %v", today, start)
	}
	if end.Weekday() != time.Friday {
		t.Fatalf("expected end on Friday, got %s", end.Weekday())
	}
}

func TestParseDateRange_Absolute(t *testing.T) {
	start, end, err := parseDateRange("2026-04-01..2026-04-10")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) {
		t.Fatalf("expected start=%v, got %v", wantStart, start)
	}
	if !end.Equal(wantEnd) {
		t.Fatalf("expected end=%v, got %v", wantEnd, end)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -run TestParseDate -v`
Expected: FAIL — `parseDate` not defined in this package.

- [ ] **Step 3: Write minimal implementation**

Create `internal/filter/dates.go`:

```go
package filter

import (
	"fmt"
	"strings"
	"time"
)

// parseDate converts a string to a time.Time.
// Accepts: RFC 3339 ("2026-04-10T15:30:00Z"), date-only ("2026-04-10"),
// relative ("today", "tomorrow", "thisweek"), or weekday names ("monday"-"sunday").
func parseDate(s string) (time.Time, error) {
	lower := strings.ToLower(s)
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	switch lower {
	case "today":
		return today, nil
	case "tomorrow":
		return today.AddDate(0, 0, 1), nil
	case "thisweek":
		daysUntilSunday := int(time.Saturday - today.Weekday() + 1)
		if daysUntilSunday <= 0 {
			daysUntilSunday += 7
		}
		return today.AddDate(0, 0, daysUntilSunday), nil
	}

	// Weekday names
	weekdays := map[string]time.Weekday{
		"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday,
		"wednesday": time.Wednesday, "thursday": time.Thursday,
		"friday": time.Friday, "saturday": time.Saturday,
	}
	if target, ok := weekdays[lower]; ok {
		days := int(target - today.Weekday())
		if days <= 0 {
			days += 7
		}
		return today.AddDate(0, 0, days), nil
	}

	// RFC 3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	// Date-only
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("invalid date %q: use YYYY-MM-DD, RFC3339, today, tomorrow, thisweek, or a weekday name", s)
}

// parseDateRange splits a "start..end" string and parses both sides.
func parseDateRange(s string) (start, end time.Time, err error) {
	parts := strings.SplitN(s, "..", 2)
	if len(parts) != 2 {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid date range %q: use start..end", s)
	}
	start, err = parseDate(parts[0])
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid range start: %w", err)
	}
	end, err = parseDate(parts[1])
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid range end: %w", err)
	}
	return start, end, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -run "TestParseDate|TestParseDateRange" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/germanamz/projects/tusk
git add internal/filter/dates.go internal/filter/dates_test.go
git commit -m "feat(filter): add date parsing helpers with thisweek and range support"
```

---

### Task 2: Resolver Core

**Files:**
- Create: `internal/filter/resolve.go`
- Create: `internal/filter/resolve_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/filter/resolve_test.go`. This uses real SQLite repos (matching the existing test pattern in `internal/tui/filter_test.go`):

```go
package filter

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
	"github.com/google/uuid"
)

// testResolver creates an in-memory SQLite store and returns a Resolver wired
// to its ProjectRepo and TaskRepo.
func testResolver(t *testing.T) (*Resolver, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	projectRepo := sqlite.NewProjectRepo(store.DB())
	taskRepo := sqlite.NewTaskRepo(store.DB())
	return NewResolver(projectRepo, taskRepo), store
}

func TestResolve_DefaultStatuses(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(tf.Statuses) != 2 || tf.Statuses[0] != "pending" || tf.Statuses[1] != "active" {
		t.Fatalf("expected default statuses [pending active], got %v", tf.Statuses)
	}
}

func TestResolve_ExplicitStatuses(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "status", Value: "completed"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(tf.Statuses) != 1 || tf.Statuses[0] != "completed" {
		t.Fatalf("expected [completed], got %v", tf.Statuses)
	}
}

func TestResolve_MultipleStatuses(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "status", Value: "pending,active,completed"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(tf.Statuses) != 3 {
		t.Fatalf("expected 3 statuses, got %d", len(tf.Statuses))
	}
}

func TestResolve_ProjectByName(t *testing.T) {
	r, _ := testResolver(t)
	// The _default project is created by the migration
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "project", Value: "_default"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.ProjectID == nil {
		t.Fatal("expected ProjectID to be set")
	}
}

func TestResolve_ProjectNotFound(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "project", Value: "nonexistent"}},
	}

	_, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), "nonexistent") {
		t.Fatalf("expected error mentioning project name, got %v", errs[0])
	}
}

func TestResolve_PrioritySingle(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "priority", Value: "3"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.PriorityMin == nil || *tf.PriorityMin != 3 {
		t.Fatalf("expected PriorityMin=3, got %v", tf.PriorityMin)
	}
	if tf.PriorityMax == nil || *tf.PriorityMax != 3 {
		t.Fatalf("expected PriorityMax=3, got %v", tf.PriorityMax)
	}
}

func TestResolve_PriorityRange(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "priority", Value: "2..4"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.PriorityMin == nil || *tf.PriorityMin != 2 {
		t.Fatalf("expected PriorityMin=2, got %v", tf.PriorityMin)
	}
	if tf.PriorityMax == nil || *tf.PriorityMax != 4 {
		t.Fatalf("expected PriorityMax=4, got %v", tf.PriorityMax)
	}
}

func TestResolve_PriorityNamed(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "priority", Value: "high"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.PriorityMin == nil || *tf.PriorityMin != 3 {
		t.Fatalf("expected PriorityMin=3 (high), got %v", tf.PriorityMin)
	}
}

func TestResolve_DueSingle(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "due", Value: "2026-04-10"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	// Single due date sets DueBefore to end of that day
	wantDate := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	if tf.DueAfter == nil || !tf.DueAfter.Equal(wantDate) {
		t.Fatalf("expected DueAfter=%v, got %v", wantDate, tf.DueAfter)
	}
	wantEnd := wantDate.AddDate(0, 0, 1)
	if tf.DueBefore == nil || !tf.DueBefore.Equal(wantEnd) {
		t.Fatalf("expected DueBefore=%v, got %v", wantEnd, tf.DueBefore)
	}
}

func TestResolve_DueRange(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "due", Value: "2026-04-01..2026-04-10"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	wantAfter := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	wantBefore := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	if tf.DueAfter == nil || !tf.DueAfter.Equal(wantAfter) {
		t.Fatalf("expected DueAfter=%v, got %v", wantAfter, tf.DueAfter)
	}
	if tf.DueBefore == nil || !tf.DueBefore.Equal(wantBefore) {
		t.Fatalf("expected DueBefore=%v, got %v", wantBefore, tf.DueBefore)
	}
}

func TestResolve_Tags(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Tags: []TagFilter{
			{Name: "api", Exclude: false},
			{Name: "docs", Exclude: true},
			{Name: "frontend", Exclude: false},
		},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(tf.Tags) != 2 || tf.Tags[0] != "api" || tf.Tags[1] != "frontend" {
		t.Fatalf("expected Tags=[api frontend], got %v", tf.Tags)
	}
	if len(tf.ExcludeTags) != 1 || tf.ExcludeTags[0] != "docs" {
		t.Fatalf("expected ExcludeTags=[docs], got %v", tf.ExcludeTags)
	}
}

func TestResolve_WaitingTrue(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "waiting", Value: "true"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.WaitingOnly == nil || !*tf.WaitingOnly {
		t.Fatal("expected WaitingOnly=true")
	}
}

func TestResolve_WaitingFalse(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "waiting", Value: "false"}},
	}

	tf, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.WaitingOnly == nil || *tf.WaitingOnly {
		t.Fatal("expected WaitingOnly=false")
	}
}

func TestResolve_ParentShortID(t *testing.T) {
	r, store := testResolver(t)
	ctx := context.Background()

	// Create a task to use as parent
	taskRepo := sqlite.NewTaskRepo(store.DB())
	parent := &domain.Task{
		ID:      uuid.New(),
		ShortID: "a3f8b2c1",
		Title:   "Parent task",
		Status:  "pending",
		Version: 1,
	}
	if err := taskRepo.Create(ctx, parent); err != nil {
		t.Fatalf("creating parent task: %v", err)
	}

	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "parent", Value: "a3f8b2c1"}},
	}

	tf, errs := r.Resolve(ctx, fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.ParentID == nil || *tf.ParentID != parent.ID {
		t.Fatalf("expected ParentID=%v, got %v", parent.ID, tf.ParentID)
	}
}

func TestResolve_TreeShortID(t *testing.T) {
	r, store := testResolver(t)
	ctx := context.Background()

	taskRepo := sqlite.NewTaskRepo(store.DB())
	root := &domain.Task{
		ID:      uuid.New(),
		ShortID: "deadbeef",
		Title:   "Root task",
		Status:  "pending",
		Version: 1,
	}
	if err := taskRepo.Create(ctx, root); err != nil {
		t.Fatalf("creating root task: %v", err)
	}

	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "tree", Value: "deadbeef"}},
	}

	tf, errs := r.Resolve(ctx, fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.RootID == nil || *tf.RootID != root.ID {
		t.Fatalf("expected RootID=%v, got %v", root.ID, tf.RootID)
	}
}

func TestResolve_ParentNotFound(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "parent", Value: "ffffffff"}},
	}

	_, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestResolve_MultipleErrors(t *testing.T) {
	r, _ := testResolver(t)
	fs := &FilterSet{
		Fields: []FieldFilter{
			{Key: "project", Value: "nonexistent"},
			{Key: "parent", Value: "ffffffff"},
		},
	}

	_, errs := r.Resolve(context.Background(), fs)
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -run TestResolve -v`
Expected: FAIL — `Resolver` type not defined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/filter/resolve.go`:

```go
package filter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
)

// Resolver converts a parsed FilterSet into a domain.TaskFilter.
type Resolver struct {
	projectRepo repository.ProjectRepository
	taskRepo    repository.TaskRepository
}

// NewResolver creates a Resolver with the given repositories.
func NewResolver(projectRepo repository.ProjectRepository, taskRepo repository.TaskRepository) *Resolver {
	return &Resolver{
		projectRepo: projectRepo,
		taskRepo:    taskRepo,
	}
}

// Resolve converts the AST into a domain.TaskFilter. Resolution errors
// (e.g., project not found) are collected rather than failing fast.
func (r *Resolver) Resolve(ctx context.Context, fs *FilterSet) (*domain.TaskFilter, []error) {
	var tf domain.TaskFilter
	var errs []error

	// Tags
	if inc := fs.IncludeTags(); len(inc) > 0 {
		tf.Tags = inc
	}
	if exc := fs.ExcludeTags(); len(exc) > 0 {
		tf.ExcludeTags = exc
	}

	// Default statuses when none specified
	hasStatus := false

	for _, field := range fs.Fields {
		switch field.Key {
		case "status":
			hasStatus = true
			tf.Statuses = strings.Split(field.Value, ",")

		case "project":
			project, err := r.projectRepo.GetByName(ctx, field.Value)
			if err != nil {
				if errors.Is(err, domain.ErrNotFound) {
					errs = append(errs, fmt.Errorf("project %q not found", field.Value))
				} else {
					errs = append(errs, fmt.Errorf("looking up project %q: %w", field.Value, err))
				}
				continue
			}
			tf.ProjectID = &project.ID

		case "priority":
			if strings.Contains(field.Value, "..") {
				parts := strings.SplitN(field.Value, "..", 2)
				min, err := parsePriorityValue(parts[0])
				if err != nil {
					errs = append(errs, fmt.Errorf("priority range min: %w", err))
					continue
				}
				max, err := parsePriorityValue(parts[1])
				if err != nil {
					errs = append(errs, fmt.Errorf("priority range max: %w", err))
					continue
				}
				tf.PriorityMin = &min
				tf.PriorityMax = &max
			} else {
				v, err := parsePriorityValue(field.Value)
				if err != nil {
					errs = append(errs, fmt.Errorf("priority: %w", err))
					continue
				}
				tf.PriorityMin = &v
				tf.PriorityMax = &v
			}

		case "due":
			if strings.Contains(field.Value, "..") {
				start, end, err := parseDateRange(field.Value)
				if err != nil {
					errs = append(errs, fmt.Errorf("due range: %w", err))
					continue
				}
				tf.DueAfter = &start
				tf.DueBefore = &end
			} else {
				d, err := parseDate(field.Value)
				if err != nil {
					errs = append(errs, fmt.Errorf("due: %w", err))
					continue
				}
				tf.DueAfter = &d
				end := d.AddDate(0, 0, 1)
				tf.DueBefore = &end
			}

		case "parent":
			task, err := r.taskRepo.GetByShortID(ctx, field.Value)
			if err != nil {
				if errors.Is(err, domain.ErrNotFound) {
					errs = append(errs, fmt.Errorf("parent task %q not found", field.Value))
				} else {
					errs = append(errs, fmt.Errorf("looking up parent %q: %w", field.Value, err))
				}
				continue
			}
			tf.ParentID = &task.ID

		case "tree":
			task, err := r.taskRepo.GetByShortID(ctx, field.Value)
			if err != nil {
				if errors.Is(err, domain.ErrNotFound) {
					errs = append(errs, fmt.Errorf("tree root task %q not found", field.Value))
				} else {
					errs = append(errs, fmt.Errorf("looking up tree root %q: %w", field.Value, err))
				}
				continue
			}
			tf.RootID = &task.ID

		case "waiting":
			v := field.Value == "true"
			tf.WaitingOnly = &v
		}
	}

	if !hasStatus {
		tf.Statuses = []string{"pending", "active"}
	}

	return &tf, errs
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -run TestResolve -v`
Expected: PASS

- [ ] **Step 5: Run all filter package tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
cd /Users/germanamz/projects/tusk
git add internal/filter/resolve.go internal/filter/resolve_test.go
git commit -m "feat(filter): add Resolver to convert FilterSet AST to domain.TaskFilter"
```

---

### Task 3: End-to-End Parse + Resolve Tests

**Files:**
- Create: `internal/filter/integration_test.go`

These tests verify the full pipeline: raw filter string -> `Parse()` -> `Resolve()` -> `domain.TaskFilter`. They mirror the existing test scenarios from `internal/tui/filter_test.go` to ensure behavioral equivalence.

- [ ] **Step 1: Write the tests**

Create `internal/filter/integration_test.go`:

```go
package filter

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
	"github.com/google/uuid"
)

func testSetup(t *testing.T) (*Resolver, *sqlite.TaskRepo) {
	t.Helper()
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	projectRepo := sqlite.NewProjectRepo(store.DB())
	taskRepo := sqlite.NewTaskRepo(store.DB())
	return NewResolver(projectRepo, taskRepo), taskRepo
}

func TestIntegration_DefaultFilter(t *testing.T) {
	r, _ := testSetup(t)
	fs, parseErrs := Parse("")
	if len(parseErrs) != 0 {
		t.Fatalf("parse errors: %v", parseErrs)
	}
	tf, resolveErrs := r.Resolve(context.Background(), fs)
	if len(resolveErrs) != 0 {
		t.Fatalf("resolve errors: %v", resolveErrs)
	}
	if len(tf.Statuses) != 2 || tf.Statuses[0] != "pending" || tf.Statuses[1] != "active" {
		t.Fatalf("expected default statuses [pending active], got %v", tf.Statuses)
	}
}

func TestIntegration_ComplexFilter(t *testing.T) {
	r, _ := testSetup(t)
	fs, parseErrs := Parse("status:completed project:_default priority:2..4 +api -docs")
	if len(parseErrs) != 0 {
		t.Fatalf("parse errors: %v", parseErrs)
	}
	tf, resolveErrs := r.Resolve(context.Background(), fs)
	if len(resolveErrs) != 0 {
		t.Fatalf("resolve errors: %v", resolveErrs)
	}

	if len(tf.Statuses) != 1 || tf.Statuses[0] != "completed" {
		t.Fatalf("expected [completed], got %v", tf.Statuses)
	}
	if tf.ProjectID == nil {
		t.Fatal("expected ProjectID to be set")
	}
	if tf.PriorityMin == nil || *tf.PriorityMin != 2 {
		t.Fatalf("expected PriorityMin=2, got %v", tf.PriorityMin)
	}
	if tf.PriorityMax == nil || *tf.PriorityMax != 4 {
		t.Fatalf("expected PriorityMax=4, got %v", tf.PriorityMax)
	}
	if len(tf.Tags) != 1 || tf.Tags[0] != "api" {
		t.Fatalf("expected Tags=[api], got %v", tf.Tags)
	}
	if len(tf.ExcludeTags) != 1 || tf.ExcludeTags[0] != "docs" {
		t.Fatalf("expected ExcludeTags=[docs], got %v", tf.ExcludeTags)
	}
}

func TestIntegration_ParentFilter(t *testing.T) {
	r, taskRepo := testSetup(t)
	ctx := context.Background()

	parent := &domain.Task{
		ID:      uuid.New(),
		ShortID: "abcd1234",
		Title:   "Parent",
		Status:  "pending",
		Version: 1,
	}
	if err := taskRepo.Create(ctx, parent); err != nil {
		t.Fatalf("creating task: %v", err)
	}

	fs, parseErrs := Parse("parent:abcd1234 status:active")
	if len(parseErrs) != 0 {
		t.Fatalf("parse errors: %v", parseErrs)
	}
	tf, resolveErrs := r.Resolve(ctx, fs)
	if len(resolveErrs) != 0 {
		t.Fatalf("resolve errors: %v", resolveErrs)
	}
	if tf.ParentID == nil || *tf.ParentID != parent.ID {
		t.Fatalf("expected ParentID=%v, got %v", parent.ID, tf.ParentID)
	}
	if len(tf.Statuses) != 1 || tf.Statuses[0] != "active" {
		t.Fatalf("expected [active], got %v", tf.Statuses)
	}
}

func TestIntegration_TreeFilter(t *testing.T) {
	r, taskRepo := testSetup(t)
	ctx := context.Background()

	root := &domain.Task{
		ID:      uuid.New(),
		ShortID: "deadbeef",
		Title:   "Root",
		Status:  "pending",
		Version: 1,
	}
	if err := taskRepo.Create(ctx, root); err != nil {
		t.Fatalf("creating task: %v", err)
	}

	fs, parseErrs := Parse("tree:deadbeef")
	if len(parseErrs) != 0 {
		t.Fatalf("parse errors: %v", parseErrs)
	}
	tf, resolveErrs := r.Resolve(ctx, fs)
	if len(resolveErrs) != 0 {
		t.Fatalf("resolve errors: %v", resolveErrs)
	}
	if tf.RootID == nil || *tf.RootID != root.ID {
		t.Fatalf("expected RootID=%v, got %v", root.ID, tf.RootID)
	}
}

func TestIntegration_ParseAndResolveErrors(t *testing.T) {
	r, _ := testSetup(t)
	// "foo:bar" triggers a parse error; "project:nonexistent" triggers a resolve error
	fs, parseErrs := Parse("foo:bar project:nonexistent status:active")
	if len(parseErrs) != 1 {
		t.Fatalf("expected 1 parse error, got %d: %v", len(parseErrs), parseErrs)
	}
	_, resolveErrs := r.Resolve(context.Background(), fs)
	if len(resolveErrs) != 1 {
		t.Fatalf("expected 1 resolve error, got %d: %v", len(resolveErrs), resolveErrs)
	}
}

func TestIntegration_TitleExtraction(t *testing.T) {
	fs, parseErrs := Parse("Implement auth middleware project:backend +api priority:3")
	if len(parseErrs) != 0 {
		t.Fatalf("parse errors: %v", parseErrs)
	}
	if fs.Title() != "Implement auth middleware" {
		t.Fatalf("expected title %q, got %q", "Implement auth middleware", fs.Title())
	}
}
```

- [ ] **Step 2: Run tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -run TestIntegration -v`
Expected: All PASS

- [ ] **Step 3: Run all filter package tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -v`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
cd /Users/germanamz/projects/tusk
git add internal/filter/integration_test.go
git commit -m "test(filter): add end-to-end integration tests for Parse + Resolve pipeline"
```
