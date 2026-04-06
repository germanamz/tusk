# Urgency Scoring — Phase 2: Wire Engine into TaskService and Consumers

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate `UrgencyEngine` into `TaskService.List` so all task listings (CLI + MCP) are scored and sorted by urgency. Add the urgency column to the TUI list output.

**Architecture:** `TaskService` gains an `UrgencyEngine` dependency and a `RelationRepository` dependency. After fetching tasks, `List` batch-loads relation counts, annotation counts, and tag counts, then calls `engine.ScoreAndSort`. The TUI renders an `Urg` column. All existing commands and MCP tools get urgency-sorted results automatically.

**Tech Stack:** Go, SQLite

**Prerequisites:** Phase 1 must be completed. Phase 1 introduced:
- `UrgencyEngine`, `UrgencyWeights`, `ScoringContext` in `internal/service/urgency.go`
- `Urgency float64` field on `domain.Task`
- `urgency` field in `tui.taskJSON` and `mcp.taskResponse` (currently always 0.0)
- `CountByTasks` on `AnnotationRepository`
- `CountBlockingByTasks` / `CountBlockedByTasks` on `RelationRepository`
- 5 new weight fields in `UrgencyConfig`

---

## Inherits From

**Phase 1** added the urgency engine, batch count repo methods, the `Urgency` field on `domain.Task`, and the serialization plumbing. Everything compiles and tests pass, but no consumer calls the engine yet — all urgency scores are zero.

The implementer can rely on:
- `service.NewUrgencyEngine(weights UrgencyWeights) *UrgencyEngine`
- `engine.ScoreAndSort(tasks []*domain.Task, ctx ScoringContext)` — stamps `task.Urgency` and sorts descending
- `annotationRepo.CountByTasks(ctx, taskIDs) (map[uuid.UUID]int, error)`
- `relationRepo.CountBlockingByTasks(ctx, taskIDs) (map[uuid.UUID]int, error)`
- `relationRepo.CountBlockedByTasks(ctx, taskIDs) (map[uuid.UUID]int, error)`
- `tagRepo.GetTaskTagsBatch(ctx, taskIDs) (map[uuid.UUID][]*domain.Tag, error)` — existing method on `repository.TagRepository`

---

### Task 1: Add UrgencyEngine and RelationRepository to TaskService

**Files:**
- Modify: `internal/service/task.go` (lines 28-51, the struct and constructor)
- Modify: `cmd/tusk/main.go` (lines 63-64, where TaskService is constructed)

- [ ] **Step 1: Add new dependencies to TaskService struct**

In `internal/service/task.go`, update the struct (lines 28-34) and constructor (lines 37-51):

```go
// TaskService implements task business logic including validation,
// workflow enforcement, and optimistic locking.
type TaskService struct {
	taskRepo       repository.TaskRepository
	annotationRepo repository.AnnotationRepository
	relationRepo   repository.RelationRepository
	tagRepo        repository.TagRepository
	projectRepo    repository.ProjectRepository
	workflowSvc    *WorkflowService
	txProvider     TaskTxProvider
	urgencyEngine  *UrgencyEngine
}

// NewTaskService creates a new TaskService with the given dependencies.
func NewTaskService(
	tr repository.TaskRepository,
	ar repository.AnnotationRepository,
	rr repository.RelationRepository,
	tagr repository.TagRepository,
	pr repository.ProjectRepository,
	ws *WorkflowService,
	txp TaskTxProvider,
	ue *UrgencyEngine,
) *TaskService {
	return &TaskService{
		taskRepo:       tr,
		annotationRepo: ar,
		relationRepo:   rr,
		tagRepo:        tagr,
		projectRepo:    pr,
		workflowSvc:    ws,
		txProvider:     txp,
		urgencyEngine:  ue,
	}
}
```

- [ ] **Step 2: Update DI wiring in main.go**

In `cmd/tusk/main.go`, update the TaskService construction (around line 64). You need to add `"github.com/germanamz/tusk/internal/config"` usage for building the engine, and pass `relationRepo` and `tagRepo`:

After the line `relationSvc := service.NewRelationService(...)` (line 66), add the urgency engine construction and update the TaskService call:

```go
	urgencyEngine := service.NewUrgencyEngine(service.UrgencyWeights{
		Priority:    cfg.Urgency.PriorityWeight,
		Due:         cfg.Urgency.DueWeight,
		Age:         cfg.Urgency.AgeWeight,
		Active:      cfg.Urgency.ActiveWeight,
		Blocking:    cfg.Urgency.BlockingWeight,
		Blocked:     cfg.Urgency.BlockedWeight,
		Tags:        cfg.Urgency.TagsWeight,
		Project:     cfg.Urgency.ProjectWeight,
		Annotations: cfg.Urgency.AnnotationsWeight,
		Waiting:     cfg.Urgency.WaitingWeight,
	})

	taskSvc := service.NewTaskService(taskRepo, annotationRepo, relationRepo, tagRepo, projectRepo, workflowSvc, store, urgencyEngine)
```

The `taskSvc` line replaces the existing one at line 64.

- [ ] **Step 3: Verify compilation**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`
Expected: Clean compilation. The new parameters are wired but `List` doesn't use them yet.

- [ ] **Step 4: Commit**

```bash
git add internal/service/task.go cmd/tusk/main.go
git commit -m "refactor(service): add urgency engine and relation/tag repos to TaskService"
```

---

### Task 2: Implement urgency scoring in TaskService.List

**Files:**
- Modify: `internal/service/task.go` (lines 147-150, the `List` method)

- [ ] **Step 1: Replace the List method with urgency-scored version**

In `internal/service/task.go`, replace the `List` method (lines 147-150) with:

```go
// List returns tasks matching the given filter, scored and sorted by urgency.
func (s *TaskService) List(ctx context.Context, filter domain.FilterExpr) ([]*domain.Task, error) {
	tasks, err := s.taskRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	if len(tasks) == 0 || s.urgencyEngine == nil {
		return tasks, nil
	}

	// Collect task IDs for batch queries
	taskIDs := make([]uuid.UUID, len(tasks))
	for i, t := range tasks {
		taskIDs[i] = t.ID
	}

	// Batch-load relation counts
	blockingCounts, err := s.relationRepo.CountBlockingByTasks(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("loading blocking counts: %w", err)
	}
	blockedByCounts, err := s.relationRepo.CountBlockedByTasks(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("loading blocked-by counts: %w", err)
	}

	// Batch-load annotation counts
	annotationCounts, err := s.annotationRepo.CountByTasks(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("loading annotation counts: %w", err)
	}

	// Batch-load tag counts
	tagsByTask, err := s.tagRepo.GetTaskTagsBatch(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("loading tag counts: %w", err)
	}
	tagCounts := make(map[uuid.UUID]int, len(tagsByTask))
	for id, tags := range tagsByTask {
		tagCounts[id] = len(tags)
	}

	sctx := ScoringContext{
		BlockingCount:   blockingCounts,
		BlockedByCount:  blockedByCounts,
		AnnotationCount: annotationCounts,
		TagCount:        tagCounts,
		ProjectWeights:  map[string]*UrgencyWeights{},
	}

	s.urgencyEngine.ScoreAndSort(tasks, sctx)
	return tasks, nil
}
```

Note: `ProjectWeights` is empty for now — per-project overrides are wired in Phase 3. This is intentional and correct: all tasks use global defaults.

- [ ] **Step 2: Verify compilation and run tests**

Run: `cd /Users/germanamz/projects/tusk && go build ./... && go test ./...`
Expected: All pass. List now returns urgency-scored, sorted results. E2E tests should still pass since they don't assert on ordering.

- [ ] **Step 3: Commit**

```bash
git add internal/service/task.go
git commit -m "feat(service): integrate urgency scoring into TaskService.List"
```

---

### Task 3: Add Urgency column to TUI list output

**Files:**
- Modify: `internal/tui/render.go` (lines 289-345, `renderTaskList`)

- [ ] **Step 1: Add Urg column to the text table header and rows**

In `internal/tui/render.go`, update the `renderTaskList` method. Replace the text rendering section (lines 304-343) with a version that includes an `Urg` column between `Age` and `Title`:

Replace the header block (lines 304-317) with:

```go
	idH := r.styledHeader("ID")
	statusH := r.styledHeader("Status")
	priH := r.styledHeader("Pri")
	ageH := r.styledHeader("Age")
	urgH := r.styledHeader("Urg")
	titleH := r.styledHeader("Title")
	if _, err := fmt.Fprintf(r.w, "%s%s %s%s %s%s %s%s %s%s %s\n",
		idH, strings.Repeat(" ", max(0, 8-lipgloss.Width(idH))),
		statusH, strings.Repeat(" ", max(0, 9-lipgloss.Width(statusH))),
		priH, strings.Repeat(" ", max(0, 4-lipgloss.Width(priH))),
		ageH, strings.Repeat(" ", max(0, 5-lipgloss.Width(ageH))),
		urgH, strings.Repeat(" ", max(0, 6-lipgloss.Width(urgH))),
		titleH,
	); err != nil {
		return err
	}
```

Replace the row rendering loop (lines 318-343) with:

```go
	for _, t := range tasks {
		title := t.Title
		if tags, ok := taskTags[t.ID.String()]; ok && len(tags) > 0 {
			tagStrs := make([]string, len(tags))
			for i, tg := range tags {
				tagStrs[i] = r.styledTag(tg)
			}
			title = title + "  " + strings.Join(tagStrs, " ")
		}
		priStr := r.styledPriority(t.Priority)
		priPad := strings.Repeat(" ", max(0, 4-lipgloss.Width(priStr)))
		line := fmt.Sprintf("%-8s %-9s %s%s %-5s %-6s %s",
			t.ShortID,
			t.Status,
			priStr,
			priPad,
			formatAge(t.CreatedAt),
			formatUrgency(t.Urgency),
			title,
		)
		if r.isDimStatus(t.Status) {
			line = r.styles.Dim.Render(line)
		}
		if _, err := fmt.Fprintln(r.w, line); err != nil {
			return err
		}
	}
```

- [ ] **Step 2: Add the formatUrgency helper function**

Add this function in `internal/tui/render.go`, near `formatAge` (around line 229):

```go
// formatUrgency formats an urgency score for display.
func formatUrgency(u float64) string {
	if u == 0 {
		return "  0"
	}
	return fmt.Sprintf("%.1f", u)
}
```

- [ ] **Step 3: Verify compilation and run tests**

Run: `cd /Users/germanamz/projects/tusk && go build ./... && go test ./...`
Expected: All pass. The Urg column now appears in `tusk list` text output.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/render.go
git commit -m "feat(tui): add urgency column to task list output"
```

---

### Task 4: Add E2E tests for urgency-sorted list output

**Files:**
- Create: `tests/e2e/urgency_test.go`

- [ ] **Step 1: Write E2E test for urgency sort ordering**

Create `tests/e2e/urgency_test.go`:

```go
package e2e

import (
	"testing"
)

func TestUrgencySorting(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "list_sorted_by_urgency",
			Steps: []Step{
				// Create a low-priority task
				{Args: []string{"add", "Low prio task", "priority:1"}},
				// Create a high-priority task
				{Args: []string{"add", "High prio task", "priority:4"}},
				// Create a medium-priority task
				{Args: []string{"add", "Med prio task", "priority:2"}},
				// List — should be sorted: high, med, low
				{
					Args: []string{"list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 3 {
							t.Fatalf("expected 3 tasks, got %d", len(arr))
						}
						first := arr[0].(map[string]any)
						last := arr[len(arr)-1].(map[string]any)
						assertEqual(t, first["title"], "High prio task")
						assertEqual(t, last["title"], "Low prio task")

						// Verify urgency field is present and non-zero for all
						for _, item := range arr {
							m := item.(map[string]any)
							urg, ok := m["urgency"].(float64)
							if !ok {
								t.Fatal("urgency field missing or not a number")
							}
							if urg <= 0 {
								t.Errorf("expected positive urgency for %s, got %.2f", m["title"], urg)
							}
						}

						// Verify descending order
						firstUrg := first["urgency"].(float64)
						lastUrg := last["urgency"].(float64)
						if firstUrg <= lastUrg {
							t.Errorf("expected descending urgency: first=%.2f, last=%.2f", firstUrg, lastUrg)
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						// Verify Urg column header exists
						assertContains(t, output, "Urg")
						// Verify high priority task appears in output
						assertContains(t, output, "High prio task")
					},
				},
			},
		},
		{
			Name: "active_task_ranks_higher",
			Steps: []Step{
				// Create two equal-priority tasks
				{Args: []string{"add", "Pending task", "priority:2"}},
				{Args: []string{"add", "Active task", "priority:2"}},
				// Start the second task
				{Args: []string{"start", "$1.short_id"}},
				// List — active should rank higher
				{
					Args: []string{"list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 2 {
							t.Fatalf("expected 2 tasks, got %d", len(arr))
						}
						first := arr[0].(map[string]any)
						assertEqual(t, first["title"], "Active task")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
```

- [ ] **Step 2: Run the E2E test**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./tests/e2e/ -run TestUrgencySorting`
Expected: PASS

- [ ] **Step 3: Run full test suite**

Run: `cd /Users/germanamz/projects/tusk && go test ./...`
Expected: All pass.

- [ ] **Step 4: Commit**

```bash
git add tests/e2e/urgency_test.go
git commit -m "test(e2e): add urgency sorting tests"
```

---

## Changes Introduced

**New files:**
- `tests/e2e/urgency_test.go` — E2E tests for urgency sort ordering

**Modified types:**
- `service.TaskService` — added `relationRepo`, `tagRepo`, and `urgencyEngine` fields
- `service.NewTaskService` — constructor signature changed (added 3 new parameters: `rr repository.RelationRepository`, `tagr repository.TagRepository`, `ue *UrgencyEngine`)

**Modified files:**
- `internal/service/task.go` — `TaskService` struct, constructor, and `List` method now perform batch scoring
- `cmd/tusk/main.go` — constructs `UrgencyEngine` from config, passes `relationRepo`, `tagRepo`, and engine to `NewTaskService`
- `internal/tui/render.go` — `renderTaskList` renders `Urg` column; new `formatUrgency` helper

**Bridge code:**
- `ProjectWeights` in `ScoringContext` is passed as an empty map. Per-project overrides are wired in **Phase 3**.

**User-visible behavior changes:**
- `tusk list` (text) now shows an `Urg` column
- `tusk list` (JSON) now includes a non-zero `urgency` field
- Task lists are sorted by urgency descending (highest urgency first)
- MCP `tusk_task_list` responses include `urgency` and are sorted
