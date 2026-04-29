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
		sort.SliceStable(tasks, func(ii, jj int) bool {
			return siblingLess(tasks[ii], tasks[jj])
		})
	case "urgency":
		sort.SliceStable(tasks, func(ii, jj int) bool {
			return tasks[ii].Urgency > tasks[jj].Urgency
		})
	case "created":
		sort.SliceStable(tasks, func(ii, jj int) bool {
			return tasks[ii].CreatedAt.Before(tasks[jj].CreatedAt)
		})
	case "priority":
		sort.SliceStable(tasks, func(ii, jj int) bool {
			if tasks[ii].Priority != tasks[jj].Priority {
				return tasks[ii].Priority > tasks[jj].Priority
			}
			return tasks[ii].Urgency > tasks[jj].Urgency
		})
	case "due":
		sort.SliceStable(tasks, func(ii, jj int) bool {
			switch {
			case tasks[ii].DueAt == nil && tasks[jj].DueAt != nil:
				return false
			case tasks[ii].DueAt != nil && tasks[jj].DueAt == nil:
				return true
			case tasks[ii].DueAt != nil && tasks[jj].DueAt != nil && !tasks[ii].DueAt.Equal(*tasks[jj].DueAt):
				return tasks[ii].DueAt.Before(*tasks[jj].DueAt)
			}
			return tasks[ii].Urgency > tasks[jj].Urgency
		})
	}
}

// siblingLess mirrors the SQL ORDER BY used by TaskRepository.GetChildren:
// order ASC NULLS LAST, then created_at ASC, then id ASC as the final
// tie-breaker. Duplicated from service/task_resequence.go intentionally to
// avoid leaking a sort helper across the public service API.
func siblingLess(left, right *domain.Task) bool {
	switch {
	case left.Order == nil && right.Order != nil:
		return false
	case left.Order != nil && right.Order == nil:
		return true
	case left.Order != nil && right.Order != nil && *left.Order != *right.Order:
		return *left.Order < *right.Order
	}
	if !left.CreatedAt.Equal(right.CreatedAt) {
		return left.CreatedAt.Before(right.CreatedAt)
	}
	return left.ID.String() < right.ID.String()
}
