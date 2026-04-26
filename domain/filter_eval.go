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
func EvalFilter(expr FilterExpr, t *Task, tagsFor func(uuid.UUID) []string) bool {
	if expr == nil || t == nil {
		return true
	}
	switch e := expr.(type) {
	case *TermFilter:
		return evalTerm(e.TaskFilter, t, tagsFor)
	case TermFilter:
		return evalTerm(e.TaskFilter, t, tagsFor)
	case *AndFilter:
		for _, child := range e.Children {
			if !EvalFilter(child, t, tagsFor) {
				return false
			}
		}
		return true
	case AndFilter:
		for _, child := range e.Children {
			if !EvalFilter(child, t, tagsFor) {
				return false
			}
		}
		return true
	case *OrFilter:
		if len(e.Children) == 0 {
			return true
		}
		for _, child := range e.Children {
			if EvalFilter(child, t, tagsFor) {
				return true
			}
		}
		return false
	case OrFilter:
		if len(e.Children) == 0 {
			return true
		}
		for _, child := range e.Children {
			if EvalFilter(child, t, tagsFor) {
				return true
			}
		}
		return false
	case *NotFilter:
		return !EvalFilter(e.Child, t, tagsFor)
	case NotFilter:
		return !EvalFilter(e.Child, t, tagsFor)
	}
	return true
}

func evalTerm(f TaskFilter, t *Task, tagsFor func(uuid.UUID) []string) bool {
	if f.ProjectID != nil && t.ProjectID != *f.ProjectID {
		return false
	}
	if f.ParentID != nil {
		if t.ParentID == nil || *t.ParentID != *f.ParentID {
			return false
		}
	}
	if len(f.Statuses) > 0 && !slices.Contains(f.Statuses, t.Status) {
		return false
	}
	if len(f.Levels) > 0 {
		if t.Level == nil {
			return false
		}
		if !slices.Contains(f.Levels, *t.Level) {
			return false
		}
	}
	if f.PriorityMin != nil && t.Priority < *f.PriorityMin {
		return false
	}
	if f.PriorityMax != nil && t.Priority > *f.PriorityMax {
		return false
	}
	if (len(f.Tags) > 0 || len(f.ExcludeTags) > 0) && tagsFor != nil {
		taskTags := tagsFor(t.ID)
		for _, want := range f.Tags {
			if !slices.Contains(taskTags, want) {
				return false
			}
		}
		for _, exclude := range f.ExcludeTags {
			if slices.Contains(taskTags, exclude) {
				return false
			}
		}
	}
	return true
}
