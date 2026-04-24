package tui

import (
	"fmt"
	"sort"

	"github.com/germanamz/tusk/domain"
)

// validSortModes enumerates the values accepted by the --sort flag on
// `tusk task list` and `tusk task tree`. Kept here so both commands pull from
// a single source of truth.
var validSortModes = map[string]struct{}{
	"order":    {},
	"urgency":  {},
	"created":  {},
	"priority": {},
	"due":      {},
}

// validateSortMode returns an error when mode is not a recognized sort value.
// An empty mode is treated as "use the command's default" and returns nil.
func validateSortMode(mode string) error {
	if mode == "" {
		return nil
	}
	if _, ok := validSortModes[mode]; !ok {
		return fmt.Errorf("invalid --sort %q: expected one of order|urgency|created|priority|due", mode)
	}
	return nil
}

// sortTasks re-orders the slice in place according to the given mode. Urgency
// values are assumed pre-computed by the service for the "urgency", "priority",
// and "due" modes; modes that fall back on urgency as a tie-breaker use
// whatever Urgency is on the task (zero when not scored).
func sortTasks(tasks []*domain.Task, mode string) {
	switch mode {
	case "order":
		sort.SliceStable(tasks, func(i, j int) bool {
			return siblingLess(tasks[i], tasks[j])
		})
	case "urgency":
		sort.SliceStable(tasks, func(i, j int) bool {
			return tasks[i].Urgency > tasks[j].Urgency
		})
	case "created":
		sort.SliceStable(tasks, func(i, j int) bool {
			return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
		})
	case "priority":
		sort.SliceStable(tasks, func(i, j int) bool {
			if tasks[i].Priority != tasks[j].Priority {
				return tasks[i].Priority > tasks[j].Priority
			}
			return tasks[i].Urgency > tasks[j].Urgency
		})
	case "due":
		sort.SliceStable(tasks, func(i, j int) bool {
			switch {
			case tasks[i].DueAt == nil && tasks[j].DueAt != nil:
				return false
			case tasks[i].DueAt != nil && tasks[j].DueAt == nil:
				return true
			case tasks[i].DueAt != nil && tasks[j].DueAt != nil && !tasks[i].DueAt.Equal(*tasks[j].DueAt):
				return tasks[i].DueAt.Before(*tasks[j].DueAt)
			}
			return tasks[i].Urgency > tasks[j].Urgency
		})
	}
}

// siblingLess mirrors the SQL ORDER BY used by TaskRepository.GetChildren:
// order ASC NULLS LAST, then created_at ASC, then id ASC as the final
// tie-breaker. Duplicated from service/task_resequence.go intentionally to
// avoid leaking a sort helper across the public service API.
func siblingLess(a, b *domain.Task) bool {
	switch {
	case a.Order == nil && b.Order != nil:
		return false
	case a.Order != nil && b.Order == nil:
		return true
	case a.Order != nil && b.Order != nil && *a.Order != *b.Order:
		return *a.Order < *b.Order
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	return a.ID.String() < b.ID.String()
}
