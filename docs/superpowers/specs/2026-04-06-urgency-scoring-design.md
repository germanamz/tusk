# Urgency Scoring — Design Spec

## Overview

A weighted multi-factor scoring engine that computes a numeric urgency score per task. Used for default sort order in CLI and MCP, and powers a `tusk next` command. Weights are configurable globally via `[urgency]` config and overridable per-project with sparse merge.

## Architecture

### UrgencyEngine

New type in `internal/service/urgency.go`. Stateless — receives config weights and preloaded batch data, returns scores.

```go
type UrgencyEngine struct {
    defaults UrgencyWeights
}

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
```

#### Key Methods

- **`Score(task *domain.Task, ctx ScoringContext) float64`** — Computes urgency for a single task.
- **`ScoreAndSort(tasks []*domain.Task, ctx ScoringContext)`** — Stamps `task.Urgency` on each task, sorts slice descending by urgency.

#### ScoringContext

Holds preloaded batch data to avoid N+1 queries:

```go
type ScoringContext struct {
    BlockingCount   map[uuid.UUID]int       // how many tasks each task blocks
    BlockedByCount  map[uuid.UUID]int       // how many tasks block each task
    AnnotationCount map[uuid.UUID]int
    TagCount        map[uuid.UUID]int
    ProjectWeights  map[string]*UrgencyWeights // per-project overrides (merged)
}
```

### Computation Approach

Batch query at service layer. After fetching tasks from the repository, issue bulk queries for relation counts, annotation counts, and tag counts. Build lookup maps, then score in-memory with Go. This matches the existing pattern where TUI batch-fetches tags after listing tasks.

## Factor Calculations

Each factor produces a coefficient in `[0, 1]` (or `{0, 1}` for boolean factors), multiplied by its weight to produce the factor's contribution to the total score. The total urgency is the sum of all factor contributions.

| Factor | Coefficient | Weight (default) |
|--------|-------------|------------------|
| Priority | `priority / 4.0` | 6.0 |
| Due | Sigmoid curve (see below) | 12.0 |
| Age | `min(days_since_creation / 365, 1.0)` | 2.0 |
| Active | `1.0` if `status == "active"`, else `0` | 4.0 |
| Blocking | `1.0` if task blocks other tasks, else `0` | 8.0 |
| Blocked | `1.0` if task is blocked by incomplete tasks, else `0` | -5.0 |
| Tags | `min(tag_count, 3) / 3.0` | 1.0 |
| Project | `1.0` if `project_id != ""`, else `0` | 1.0 |
| Annotations | `min(annotation_count, 2) / 2.0` | 1.0 |
| Waiting | `1.0` if `wait_until` is in the future, else `0` | -3.0 |

### Sigmoid Curve for Due Date

```
coefficient = 1 / (1 + e^(-k * (midpoint - days_until_due)))
```

- `k = 0.5` (steepness)
- `midpoint = 14` (days — inflection point)
- Tasks **past due**: `days_until_due < 0`, coefficient approaches 1.0
- Tasks **due today**: coefficient ~0.999
- Tasks **due in 14 days**: coefficient = 0.5
- Tasks **due in 30+ days**: coefficient approaches 0
- Tasks **with no due date**: contribute 0 (skip factor entirely)

These sigmoid constants are internal to the engine, not user-configurable.

### Priority Factor Detail

Priority values are 0-4. The contribution is `(priority / 4.0) * priority_weight`, yielding:

| Priority | Label | Contribution (at default weight 6.0) |
|----------|-------|---------------------------------------|
| 0 | none | 0.0 |
| 1 | low | 1.5 |
| 2 | medium | 3.0 |
| 3 | high | 4.5 |
| 4 | urgent | 6.0 |

## Domain Changes

Add a non-persisted field to `domain.Task`:

```go
Urgency float64 // Computed at read time, not stored in DB
```

This field is zero-valued by default, populated by `UrgencyEngine.ScoreAndSort`, and included in CLI/MCP output.

## Config Changes

### Expand UrgencyConfig

Add 5 new weight fields to `internal/config/config.go` `UrgencyConfig`:

```go
type UrgencyConfig struct {
    PriorityWeight    float64 `mapstructure:"priority_weight"`    // 6.0
    DueWeight         float64 `mapstructure:"due_weight"`         // 12.0
    AgeWeight         float64 `mapstructure:"age_weight"`         // 2.0
    ActiveWeight      float64 `mapstructure:"active_weight"`      // 4.0
    BlockingWeight    float64 `mapstructure:"blocking_weight"`    // 8.0
    BlockedWeight     float64 `mapstructure:"blocked_weight"`     // -5.0
    TagsWeight        float64 `mapstructure:"tags_weight"`        // 1.0
    ProjectWeight     float64 `mapstructure:"project_weight"`     // 1.0
    AnnotationsWeight float64 `mapstructure:"annotations_weight"` // 1.0
    WaitingWeight     float64 `mapstructure:"waiting_weight"`     // -3.0
}
```

### Update default.toml

Add new weight entries under `[urgency]`:

```toml
[urgency]
priority_weight    = 6.0
due_weight         = 12.0
age_weight         = 2.0
active_weight      = 4.0
blocking_weight    = 8.0
blocked_weight     = -5.0
tags_weight        = 1.0
project_weight     = 1.0
annotations_weight = 1.0
waiting_weight     = -3.0
```

### Per-Project Urgency Overrides

New config type with pointer fields for sparse merge:

```go
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
```

Added to `ProjectSettingsConfig`:

```go
type ProjectSettingsConfig struct {
    AutoCompleteParent *AutoCompleteParentConfig `mapstructure:"auto_complete_parent"`
    AutoRevertParent   *AutoRevertParentConfig   `mapstructure:"auto_revert_parent"`
    Urgency            *ProjectUrgencyConfig     `mapstructure:"urgency"`
}
```

**Merge logic**: For each weight field in `ProjectUrgencyConfig`, if non-nil use it; otherwise fall back to the global `UrgencyConfig` value. The engine resolves merged weights per project at scoring time using the `ProjectWeights` map in `ScoringContext`.

### Config Example

```toml
[projects.backend]
workflow = "kanban"

[projects.backend.settings.urgency]
blocking_weight = 15.0   # blocking is extra important in this project
# all other weights inherit from [urgency] globals
```

## Integration Points

### TaskService.List

After fetching tasks from the repository:

1. Collect all task IDs
2. Batch-load: blocking counts, blocked-by counts, annotation counts, tag counts
3. Build `ScoringContext` with batch data + per-project merged weights
4. Call `engine.ScoreAndSort(tasks, ctx)`
5. Return sorted tasks

The `UrgencyEngine` is injected into `TaskService` at construction time in `cmd/tusk/main.go`.

### TaskService.Next

New method:

1. Call `List` with a filter expression equivalent to `status:pending,active` (non-terminal, non-deleted statuses)
2. The list is already sorted by urgency descending
3. Skip tasks where `wait_until` is in the future or `blocked_by_count > 0`
4. Return `tasks[0]` or `domain.ErrNotFound` if no actionable task exists

### TUI

- **`tusk list`**: Tasks arrive pre-sorted by urgency. Add an `Urg` column to the table output, formatted as `%.1f`.
- **`tusk next`**: New Cobra command. Calls `TaskService.Next`, renders single task using the existing info-style output.

### MCP

- **`tusk_task_list`**: Results arrive pre-sorted. Add `urgency` field (float64) to `taskResponse` JSON.
- **`tusk_task_next`**: New MCP tool. Calls `TaskService.Next`, returns single `taskResponse`.

## New Repository Methods

Batch count methods to avoid N+1 queries:

```go
// AnnotationRepository
CountByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int, error)

// RelationRepository
CountBlockingByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int, error)
CountBlockedByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int, error)
```

SQLite implementations use `WHERE task_id IN (?, ?, ...) GROUP BY task_id` patterns.

Tag counts are derived from the existing batch tag fetch (count entries per task ID from the returned `map[uuid.UUID][]*domain.Tag`).

## Testing Strategy

### Unit Tests (`internal/service/urgency_test.go`)

- Each factor in isolation: given a task with specific fields and a scoring context, assert the score contribution matches expected value
- Sigmoid curve edge cases: past due, due today, due in 14 days, due in 30+ days, no due date
- Combined scoring: task with multiple factors, verify total
- Per-project weight override merge: project overrides one weight, others fall back to global
- Sort order: multiple tasks scored and sorted, verify descending order

### E2E Tests (`tests/e2e/`)

- Create tasks with different priorities, verify `tusk list` output order
- Create tasks with due dates at different distances, verify ordering
- `tusk next` returns the highest-urgency actionable task
- `tusk next` with no actionable tasks returns appropriate error
- Verify urgency column appears in list output

## Files to Create/Modify

| File | Action | Purpose |
|------|--------|---------|
| `internal/service/urgency.go` | Implement | UrgencyEngine, UrgencyWeights, ScoringContext, Score, ScoreAndSort |
| `internal/service/urgency_test.go` | Create | Unit tests for all factors and merge logic |
| `internal/domain/task.go` | Modify | Add `Urgency float64` field |
| `internal/config/config.go` | Modify | Add 5 new weights to UrgencyConfig, add ProjectUrgencyConfig, add Urgency to ProjectSettingsConfig |
| `internal/config/default.toml` | Modify | Add new weight defaults |
| `internal/domain/project_settings.go` | Modify | Add UrgencyWeights to ProjectSettings |
| `internal/repository/annotation.go` | Modify | Add CountByTasks method |
| `internal/repository/relation.go` | Modify | Add CountBlockingByTasks, CountBlockedByTasks |
| `internal/sqlite/annotation.go` | Modify | Implement CountByTasks |
| `internal/sqlite/relation.go` | Modify | Implement CountBlockingByTasks, CountBlockedByTasks |
| `internal/service/task.go` | Modify | Integrate UrgencyEngine into List, add Next method |
| `internal/tui/commands.go` | Modify | Add `tusk next` command, add Urgency column to list |
| `internal/tui/render.go` | Modify | Render urgency score in list table |
| `internal/mcp/tools.go` | Modify | Add urgency to taskResponse, add tusk_task_next tool |
| `cmd/tusk/main.go` | Modify | Wire UrgencyEngine into TaskService |
| `tests/e2e/` | Create/Modify | E2E tests for list ordering and next command |
