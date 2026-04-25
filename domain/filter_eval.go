package domain

import "slices"

// EvalFilter evaluates a FilterExpr against a single Task in memory and
// returns whether the task matches. The SQL-side evaluator in `sqlite/`
// is the authoritative one for top-level queries; this helper exists for
// service-layer post-filtering (e.g. scoping rollup descendant sets).
//
// Supported leaf predicates on TermFilter.TaskFilter:
//
//	ProjectID, ParentID, Statuses, Levels, PriorityMin, PriorityMax.
//
// Predicates that depend on data beyond the Task struct itself (Tags,
// ExcludeTags, UDA, RootID) are not evaluated here: a TermFilter that
// sets only such fields evaluates as match-all. Combined with other
// supported predicates, the unsupported ones are simply ignored. The
// SQL evaluator remains the source of truth when those predicates
// actually need to constrain a result set.
func EvalFilter(expr FilterExpr, t *Task) bool {
	if expr == nil || t == nil {
		return true
	}
	switch e := expr.(type) {
	case *TermFilter:
		return evalTerm(e.TaskFilter, t)
	case TermFilter:
		return evalTerm(e.TaskFilter, t)
	case *AndFilter:
		for _, child := range e.Children {
			if !EvalFilter(child, t) {
				return false
			}
		}
		return true
	case AndFilter:
		for _, child := range e.Children {
			if !EvalFilter(child, t) {
				return false
			}
		}
		return true
	case *OrFilter:
		if len(e.Children) == 0 {
			return true
		}
		for _, child := range e.Children {
			if EvalFilter(child, t) {
				return true
			}
		}
		return false
	case OrFilter:
		if len(e.Children) == 0 {
			return true
		}
		for _, child := range e.Children {
			if EvalFilter(child, t) {
				return true
			}
		}
		return false
	case *NotFilter:
		return !EvalFilter(e.Child, t)
	case NotFilter:
		return !EvalFilter(e.Child, t)
	}
	return true
}

func evalTerm(f TaskFilter, t *Task) bool {
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
	return true
}
