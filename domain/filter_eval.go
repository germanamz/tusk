package domain

import (
	"slices"

	"github.com/google/uuid"
)

// EvalFilter evaluates a FilterExpr against a single Task in memory and
// returns whether the task matches. The SQL-side evaluator in `sqlite/`
// is the authoritative one for top-level queries; this helper exists for
// service-layer post-filtering (e.g. scoping rollup descendant sets).
//
// Supported leaf predicates on TermFilter.TaskFilter:
//
//	ProjectID, ParentID, Statuses, Levels, PriorityMin, PriorityMax,
//	Tags, ExcludeTags.
//
// Tags / ExcludeTags require a non-nil tagsFor lookup. When tagsFor is
// nil they evaluate as match-all so callers without a tag side-table
// retain the prior "ignore tag predicates" behavior.
//
// RootID is treated as match-all here. The only caller is the
// SummarizeBlocks descendant pass, where each task being evaluated is
// already a descendant of a block that was itself selected by the same
// RootID predicate via the SQL evaluator — so RootID is transitively
// satisfied for every descendant we receive. Honoring it again would
// require an in-memory ancestry walk.
//
// Predicates that depend on data the helper cannot see (UDA, due dates,
// title/description substring) are not evaluated and effectively
// match-all. Combined with supported predicates, the unsupported ones
// are simply ignored. The SQL evaluator remains the source of truth
// when those predicates actually need to constrain a result set.
func EvalFilter(expr FilterExpr, task *Task, tagsFor func(uuid.UUID) []string) bool {
	if expr == nil || task == nil {
		return true
	}
	switch node := expr.(type) {
	case *TermFilter:
		return evalTerm(node.TaskFilter, task, tagsFor)
	case TermFilter:
		return evalTerm(node.TaskFilter, task, tagsFor)
	case *AndFilter:
		for _, child := range node.Children {
			if !EvalFilter(child, task, tagsFor) {
				return false
			}
		}
		return true
	case AndFilter:
		for _, child := range node.Children {
			if !EvalFilter(child, task, tagsFor) {
				return false
			}
		}
		return true
	case *OrFilter:
		if len(node.Children) == 0 {
			return true
		}
		for _, child := range node.Children {
			if EvalFilter(child, task, tagsFor) {
				return true
			}
		}
		return false
	case OrFilter:
		if len(node.Children) == 0 {
			return true
		}
		for _, child := range node.Children {
			if EvalFilter(child, task, tagsFor) {
				return true
			}
		}
		return false
	case *NotFilter:
		return !EvalFilter(node.Child, task, tagsFor)
	case NotFilter:
		return !EvalFilter(node.Child, task, tagsFor)
	}
	return true
}

func evalTerm(filter TaskFilter, task *Task, tagsFor func(uuid.UUID) []string) bool {
	if filter.ProjectID != nil && task.ProjectID != *filter.ProjectID {
		return false
	}
	if filter.ParentID != nil {
		if task.ParentID == nil || *task.ParentID != *filter.ParentID {
			return false
		}
	}
	if len(filter.Statuses) > 0 && !slices.Contains(filter.Statuses, task.Status) {
		return false
	}
	if len(filter.Levels) > 0 {
		if task.Level == nil {
			return false
		}
		if !slices.Contains(filter.Levels, *task.Level) {
			return false
		}
	}
	if filter.PriorityMin != nil && task.Priority < *filter.PriorityMin {
		return false
	}
	if filter.PriorityMax != nil && task.Priority > *filter.PriorityMax {
		return false
	}
	if (len(filter.Tags) > 0 || len(filter.ExcludeTags) > 0) && tagsFor != nil {
		taskTags := tagsFor(task.ID)
		for _, want := range filter.Tags {
			if !slices.Contains(taskTags, want) {
				return false
			}
		}
		for _, exclude := range filter.ExcludeTags {
			if slices.Contains(taskTags, exclude) {
				return false
			}
		}
	}
	return true
}
