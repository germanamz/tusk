# Urgency Scoring — Phase 3: Per-Project Overrides and `tusk next`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-project urgency weight overrides with sparse merge, implement the `tusk next` command and `tusk_task_next` MCP tool, and wire the project overrides into the scoring pipeline.

**Architecture:** Config gains `ProjectUrgencyConfig` (pointer fields for sparse merge). `ProjectSettings` in `domain` gains urgency overrides. `TaskService.List` builds `ProjectWeights` from project config. New `TaskService.Next` method filters for actionable tasks and returns the top one. New CLI command and MCP tool expose it.

**Tech Stack:** Go, SQLite

**Prerequisites:** Phase 1 and Phase 2 must be completed.

---

## Inherits From

**Phase 1** introduced the `UrgencyEngine`, `UrgencyWeights`, `ScoringContext`, batch count repo methods, config expansion, and `Urgency` field on `domain.Task`.

**Phase 2** wired the engine into `TaskService.List`:
- `TaskService` struct has fields: `taskRepo`, `annotationRepo`, `relationRepo`, `tagRepo`, `projectRepo`, `workflowSvc`, `txProvider`, `urgencyEngine`
- `NewTaskService` takes: `tr TaskRepository, ar AnnotationRepository, rr RelationRepository, tagr TagRepository, pr ProjectRepository, ws *WorkflowService, txp TaskTxProvider, ue *UrgencyEngine`
- `TaskService.List` batch-loads counts and calls `engine.ScoreAndSort`, but passes `ProjectWeights: map[string]*UrgencyWeights{}` (empty — this is the bridge code this phase replaces)
- TUI renders an `Urg` column in list output
- All E2E tests pass

---

### Task 1: Add ProjectUrgencyConfig to config and domain

**Files:**
- Modify: `internal/config/config.go` (after `ProjectSettingsConfig`, around line 47)
- Modify: `internal/config/default.toml` (add example comment)
- Modify: `internal/domain/project_settings.go`

- [ ] **Step 1: Add ProjectUrgencyConfig to config**

In `internal/config/config.go`, add the new type after `ProjectSettingsConfig` (around line 47) and add the `Urgency` field to `ProjectSettingsConfig`:

```go
// ProjectUrgencyConfig holds per-project urgency weight overrides.
// Nil fields inherit from the global [urgency] config.
type ProjectUrgencyConfig struct {
	PriorityWeight    *float64 `mapstructure:"priority_weight"`
	DueWeight         *float64 `mapstructure:"due_weight"`
	AgeWeight         *float64 `mapstructure:"age_weight"`
	ActiveWeight      *float64 `mapstructure:"active_weight"`
	BlockingWeight    *float64 `mapstructure:"blocking_weight"`
	BlockedWeight     *float64 `mapstructure:"blocked_weight"`
	TagsWeight        *float64 `mapstructure:"tags_weight"`
	ProjectWeight     *float64 `mapstructure:"project_weight"`
	AnnotationsWeight *float64 `mapstructure:"annotations_weight"`
	WaitingWeight     *float64 `mapstructure:"waiting_weight"`
}

// ProjectSettingsConfig holds per-project automation settings.
type ProjectSettingsConfig struct {
	AutoCompleteParent *AutoCompleteParentConfig `mapstructure:"auto_complete_parent"`
	AutoRevertParent   *AutoRevertParentConfig   `mapstructure:"auto_revert_parent"`
	Urgency            *ProjectUrgencyConfig     `mapstructure:"urgency"`
}
```

- [ ] **Step 2: Add UrgencyOverrides to domain ProjectSettings**

In `internal/domain/project_settings.go`, add a domain-level override type and field:

```go
package domain

// AutoCompleteConfig controls automatic parent completion when all children
// reach TriggerStatus. The parent is transitioned to TargetStatus.
type AutoCompleteConfig struct {
	TriggerStatus string `json:"trigger_status"`
	TargetStatus  string `json:"target_status"`
}

// AutoRevertConfig controls automatic parent revert when a child moves away
// from TriggerStatus. The parent is transitioned to TargetStatus.
type AutoRevertConfig struct {
	TriggerStatus string `json:"trigger_status"`
	TargetStatus  string `json:"target_status"`
}

// UrgencyOverrides holds per-project urgency weight overrides.
// Nil fields inherit from global defaults.
type UrgencyOverrides struct {
	PriorityWeight    *float64 `json:"priority_weight,omitempty"`
	DueWeight         *float64 `json:"due_weight,omitempty"`
	AgeWeight         *float64 `json:"age_weight,omitempty"`
	ActiveWeight      *float64 `json:"active_weight,omitempty"`
	BlockingWeight    *float64 `json:"blocking_weight,omitempty"`
	BlockedWeight     *float64 `json:"blocked_weight,omitempty"`
	TagsWeight        *float64 `json:"tags_weight,omitempty"`
	ProjectWeight     *float64 `json:"project_weight,omitempty"`
	AnnotationsWeight *float64 `json:"annotations_weight,omitempty"`
	WaitingWeight     *float64 `json:"waiting_weight,omitempty"`
}

// ProjectSettings holds per-project configuration stored as JSON in the
// projects table. Nil fields mean the feature is disabled (the default).
type ProjectSettings struct {
	AutoCompleteParent *AutoCompleteConfig `json:"auto_complete_parent,omitempty"`
	AutoRevertParent   *AutoRevertConfig   `json:"auto_revert_parent,omitempty"`
	Urgency            *UrgencyOverrides   `json:"urgency,omitempty"`
}
```

- [ ] **Step 3: Update in-memory ProjectRepository to map config urgency overrides to domain**

The in-memory `ProjectRepository` is at `internal/inmem/project.go`. It maps `config.ProjectConfig` to `domain.Project`. Check how it currently maps `ProjectSettingsConfig` to `domain.ProjectSettings` and extend it to include urgency overrides.

Read `internal/inmem/project.go` to find the mapping code. Add urgency override mapping:

```go
// Inside the mapping from config.ProjectConfig to domain.Project:
if pc.Settings.Urgency != nil {
	settings.Urgency = &domain.UrgencyOverrides{
		PriorityWeight:    pc.Settings.Urgency.PriorityWeight,
		DueWeight:         pc.Settings.Urgency.DueWeight,
		AgeWeight:         pc.Settings.Urgency.AgeWeight,
		ActiveWeight:      pc.Settings.Urgency.ActiveWeight,
		BlockingWeight:    pc.Settings.Urgency.BlockingWeight,
		BlockedWeight:     pc.Settings.Urgency.BlockedWeight,
		TagsWeight:        pc.Settings.Urgency.TagsWeight,
		ProjectWeight:     pc.Settings.Urgency.ProjectWeight,
		AnnotationsWeight: pc.Settings.Urgency.AnnotationsWeight,
		WaitingWeight:     pc.Settings.Urgency.WaitingWeight,
	}
}
```

- [ ] **Step 4: Add example to default.toml**

In `internal/config/default.toml`, add a comment example inside the existing project example block (around lines 69-72):

```toml
# Example: custom project with auto-completion and urgency overrides
# [projects.backend]
# workflow = "kanban"
# [projects.backend.settings.auto_complete_parent]
# trigger_status = "completed"
# target_status = "completed"
# [projects.backend.settings.urgency]
# blocking_weight = 15.0
```

- [ ] **Step 5: Verify compilation**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`
Expected: Clean compilation.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/default.toml \
       internal/domain/project_settings.go internal/inmem/project.go
git commit -m "feat(config): add per-project urgency weight overrides"
```

---

### Task 2: Add MergeWeights function and wire project overrides into scoring

**Files:**
- Modify: `internal/service/urgency.go`
- Modify: `internal/service/urgency_test.go`
- Modify: `internal/service/task.go` (the `List` method)

- [ ] **Step 1: Write test for MergeWeights**

In `internal/service/urgency_test.go`, add:

```go
func TestMergeWeights(t *testing.T) {
	defaults := defaultWeights()

	// Nil overrides returns defaults unchanged
	merged := MergeWeights(defaults, nil)
	if merged.Priority != 6.0 {
		t.Errorf("expected 6.0, got %.1f", merged.Priority)
	}

	// Override one field
	override := 20.0
	overrides := &domain.UrgencyOverrides{
		BlockingWeight: &override,
	}
	merged = MergeWeights(defaults, overrides)
	if merged.Blocking != 20.0 {
		t.Errorf("expected blocking 20.0, got %.1f", merged.Blocking)
	}
	if merged.Priority != 6.0 {
		t.Errorf("expected priority 6.0, got %.1f", merged.Priority)
	}
	if merged.Due != 12.0 {
		t.Errorf("expected due 12.0, got %.1f", merged.Due)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/service/ -run TestMergeWeights`
Expected: FAIL — `MergeWeights` not defined.

- [ ] **Step 3: Implement MergeWeights in urgency.go**

In `internal/service/urgency.go`, add:

```go
// MergeWeights returns a copy of defaults with any non-nil overrides applied.
func MergeWeights(defaults UrgencyWeights, overrides *domain.UrgencyOverrides) UrgencyWeights {
	if overrides == nil {
		return defaults
	}
	merged := defaults
	if overrides.PriorityWeight != nil {
		merged.Priority = *overrides.PriorityWeight
	}
	if overrides.DueWeight != nil {
		merged.Due = *overrides.DueWeight
	}
	if overrides.AgeWeight != nil {
		merged.Age = *overrides.AgeWeight
	}
	if overrides.ActiveWeight != nil {
		merged.Active = *overrides.ActiveWeight
	}
	if overrides.BlockingWeight != nil {
		merged.Blocking = *overrides.BlockingWeight
	}
	if overrides.BlockedWeight != nil {
		merged.Blocked = *overrides.BlockedWeight
	}
	if overrides.TagsWeight != nil {
		merged.Tags = *overrides.TagsWeight
	}
	if overrides.ProjectWeight != nil {
		merged.Project = *overrides.ProjectWeight
	}
	if overrides.AnnotationsWeight != nil {
		merged.Annotations = *overrides.AnnotationsWeight
	}
	if overrides.WaitingWeight != nil {
		merged.Waiting = *overrides.WaitingWeight
	}
	return merged
}
```

You will need to add `"github.com/germanamz/tusk/internal/domain"` to the imports if not already present.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/service/ -run TestMergeWeights`
Expected: PASS

- [ ] **Step 5: Wire project overrides into TaskService.List**

In `internal/service/task.go`, in the `List` method, replace the line that builds `ProjectWeights` as an empty map:

```go
	// Before: ProjectWeights: map[string]*UrgencyWeights{},
	// After:
	projectWeights := s.buildProjectWeights(tasks)
```

And use it in the `ScoringContext`:

```go
	sctx := ScoringContext{
		BlockingCount:   blockingCounts,
		BlockedByCount:  blockedByCounts,
		AnnotationCount: annotationCounts,
		TagCount:        tagCounts,
		ProjectWeights:  projectWeights,
	}
```

Add the helper method to `TaskService`:

```go
// buildProjectWeights constructs per-project merged urgency weights
// for all distinct projects found in the task list.
func (s *TaskService) buildProjectWeights(tasks []*domain.Task) map[string]*UrgencyWeights {
	if s.urgencyEngine == nil {
		return nil
	}

	// Collect distinct project IDs
	seen := make(map[string]bool)
	for _, t := range tasks {
		seen[t.ProjectID] = true
	}

	weights := make(map[string]*UrgencyWeights, len(seen))
	for projectID := range seen {
		project, err := s.projectRepo.GetByID(context.Background(), projectID)
		if err != nil {
			continue // use engine defaults if project not found
		}
		if project.Settings.Urgency == nil {
			continue // no overrides, engine will use defaults
		}
		merged := MergeWeights(s.urgencyEngine.defaults, project.Settings.Urgency)
		weights[projectID] = &merged
	}
	return weights
}
```

You will need to add `"context"` to the imports if the `context` import is not already present (it should be from the method signatures).

- [ ] **Step 6: Verify compilation and run all tests**

Run: `cd /Users/germanamz/projects/tusk && go build ./... && go test ./...`
Expected: All pass.

- [ ] **Step 7: Commit**

```bash
git add internal/service/urgency.go internal/service/urgency_test.go internal/service/task.go
git commit -m "feat(service): wire per-project urgency weight overrides into scoring"
```

---

### Task 3: Implement TaskService.Next method

**Files:**
- Modify: `internal/service/task.go`

- [ ] **Step 1: Add the Next method to TaskService**

In `internal/service/task.go`, add after the `List` method:

```go
// Next returns the highest-urgency actionable task. Actionable means:
// non-terminal status (pending or active), not waiting, not blocked.
// Returns domain.ErrNotFound if no actionable task exists.
func (s *TaskService) Next(ctx context.Context) (*domain.Task, error) {
	// List pending and active tasks
	filter := &domain.TermFilter{TaskFilter: domain.TaskFilter{
		Statuses: []string{"pending", "active"},
	}}
	tasks, err := s.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Tasks are already sorted by urgency (descending) from List.
	// Filter out waiting and blocked tasks.
	now := time.Now()
	for _, t := range tasks {
		if t.WaitUntil != nil && t.WaitUntil.After(now) {
			continue
		}
		// Check if blocked — use the scoring context data approach:
		// Since List already scored, blocked tasks have negative contribution,
		// but we need to actually skip them.
		blockedBy, err := s.relationRepo.CountBlockedByTasks(ctx, []uuid.UUID{t.ID})
		if err != nil {
			return nil, fmt.Errorf("checking blocked status: %w", err)
		}
		if blockedBy[t.ID] > 0 {
			continue
		}
		return t, nil
	}
	return nil, domain.ErrNotFound
}
```

- [ ] **Step 2: Verify compilation**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`
Expected: Clean compilation.

- [ ] **Step 3: Commit**

```bash
git add internal/service/task.go
git commit -m "feat(service): add TaskService.Next for highest-urgency actionable task"
```

---

### Task 4: Add `tusk next` CLI command

**Files:**
- Modify: `internal/tui/commands.go` (inside `buildTaskCmds`)
- Modify: `internal/tui/commands.go` (add `runNext` method)

- [ ] **Step 1: Register the `next` command**

In `internal/tui/commands.go`, inside `buildTaskCmds()`, add the `next` command to the returned slice (around line 45, before the `return` statement). Add this entry:

```go
	nextCmd := &cobra.Command{
		Use:   "next",
		Short: "Show the highest-urgency actionable task",
		Args:  cobra.NoArgs,
		RunE:  a.runNext,
	}
```

Add `nextCmd` to the returned slice in `buildTaskCmds`.

- [ ] **Step 2: Implement runNext**

Add the `runNext` method to the `App`. Place it near `runInfo` since it has similar output:

```go
func (a *App) runNext(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	task, err := a.taskSvc.Next(ctx)
	if err != nil {
		return err
	}

	// Fetch details like runInfo does
	tags, err := a.tagSvc.GetTaskTags(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("loading tags: %w", err)
	}

	annotations, err := a.taskSvc.GetAnnotations(ctx, task.ShortID)
	if err != nil {
		return fmt.Errorf("loading annotations: %w", err)
	}

	relations, err := a.relationSvc.GetByTask(ctx, task.ShortID)
	if err != nil {
		return fmt.Errorf("loading relations: %w", err)
	}

	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), a.buildDimStatuses())
	return r.renderTaskInfo(task, tags, annotations, relations)
}
```

Check `internal/tui/commands.go` for how `runInfo` works (around line 266) to confirm the pattern matches. The `renderTaskInfo` method is in `internal/tui/render.go`.

- [ ] **Step 3: Verify compilation**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`
Expected: Clean compilation.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/commands.go
git commit -m "feat(tui): add tusk next command for highest-urgency actionable task"
```

---

### Task 5: Add `tusk_task_next` MCP tool and E2E tests

**Files:**
- Modify: `internal/mcp/server.go` (add to `validToolNames`, around line 88)
- Modify: `internal/mcp/tools.go` (add handler)
- Modify: `internal/mcp/server.go` (register tool in `registerTools`)
- Create or modify: `tests/e2e/urgency_test.go` (add `tusk next` scenarios)

- [ ] **Step 1: Add the MCP tool handler**

In `internal/mcp/tools.go`, add the handler function (near the other task handlers):

```go
// handleTaskNext returns the highest-urgency actionable task.
func (s *Server) handleTaskNext(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	task, err := s.taskSvc.Next(ctx)
	if err != nil {
		return toolError(err, "next task"), nil
	}

	resp, err := s.buildTaskGetResponse(ctx, task)
	if err != nil {
		return nil, err
	}
	return toolResultJSON(resp)
}
```

- [ ] **Step 2: Register the tool in server.go**

In `internal/mcp/server.go`, add `"tusk_task_next": true` to the `validToolNames` map (around line 88).

In the `registerTools` method (in `server.go`), add the tool registration. Find where the other task tools are registered and add:

```go
	s.addTool("task", mcp.NewTool(
		"tusk_task_next",
		mcp.WithDescription("Get the highest-urgency actionable task (not waiting, not blocked)"),
	), s.handleTaskNext)
```

- [ ] **Step 3: Add E2E tests for tusk next**

In `tests/e2e/urgency_test.go`, add to the scenarios slice (or add a new test function):

```go
func TestTaskNext(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "next_returns_highest_urgency",
			Steps: []Step{
				{Args: []string{"add", "Low prio", "priority:1"}},
				{Args: []string{"add", "High prio", "priority:4"}},
				{
					Args: []string{"next"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["title"], "High prio")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "High prio")
					},
				},
			},
		},
		{
			Name: "next_no_actionable_tasks",
			Steps: []Step{
				{Args: []string{"add", "Task 1"}},
				{Args: []string{"start", "$0.short_id"}},
				{Args: []string{"done", "$0.short_id"}},
				{
					Args:    []string{"next"},
					WantErr: true,
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
```

- [ ] **Step 4: Run E2E tests**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./tests/e2e/ -run "TestTaskNext|TestUrgencySorting"`
Expected: PASS

- [ ] **Step 5: Run full test suite**

Run: `cd /Users/germanamz/projects/tusk && go test ./...`
Expected: All pass.

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/server.go internal/mcp/tools.go tests/e2e/urgency_test.go
git commit -m "feat(mcp): add tusk_task_next tool and E2E tests for next command"
```

---

## Changes Introduced

**New files:** None (all changes are modifications).

**Modified types:**
- `config.ProjectSettingsConfig` — added `Urgency *ProjectUrgencyConfig` field
- `config.ProjectUrgencyConfig` — new type with pointer fields for sparse merge
- `domain.ProjectSettings` — added `Urgency *UrgencyOverrides` field
- `domain.UrgencyOverrides` — new type mirroring config overrides

**New functions:**
- `service.MergeWeights(defaults UrgencyWeights, overrides *domain.UrgencyOverrides) UrgencyWeights`
- `service.TaskService.Next(ctx) (*domain.Task, error)`
- `service.TaskService.buildProjectWeights(tasks) map[string]*UrgencyWeights`
- `mcp.Server.handleTaskNext`

**New CLI command:** `tusk next` — shows highest-urgency actionable task

**New MCP tool:** `tusk_task_next` — returns highest-urgency actionable task

**Bridge code removed:**
- Phase 2's empty `ProjectWeights: map[string]*UrgencyWeights{}` in `TaskService.List` is replaced with `s.buildProjectWeights(tasks)` which reads actual project urgency config.

**Modified files:**
- `internal/config/config.go` — `ProjectUrgencyConfig`, `ProjectSettingsConfig.Urgency`
- `internal/config/default.toml` — example comment for urgency overrides
- `internal/domain/project_settings.go` — `UrgencyOverrides`, `ProjectSettings.Urgency`
- `internal/inmem/project.go` — maps config urgency to domain
- `internal/service/urgency.go` — `MergeWeights` function
- `internal/service/urgency_test.go` — `TestMergeWeights`
- `internal/service/task.go` — `Next` method, `buildProjectWeights` helper, wired overrides in `List`
- `internal/tui/commands.go` — `next` command registration and `runNext`
- `internal/mcp/server.go` — `tusk_task_next` registration and validation
- `internal/mcp/tools.go` — `handleTaskNext` handler
- `tests/e2e/urgency_test.go` — `TestTaskNext` scenarios

**User-visible behavior preserved:**
- All existing commands work identically
- `tusk list` continues to sort by urgency (now with per-project overrides when configured)
- MCP tools continue to work; `tusk_task_next` is additive

**New user-visible behavior:**
- `tusk next` shows the highest-urgency actionable task
- `tusk_task_next` MCP tool returns the highest-urgency actionable task
- Per-project urgency overrides take effect when configured in `[projects.<id>.settings.urgency]`
