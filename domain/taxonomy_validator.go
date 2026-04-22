package domain

// ValidationContext carries everything TaxonomyValidator needs without
// requiring repository access.
type ValidationContext struct {
	Taxonomy    Taxonomy
	ParentLevel *string // nil when the task has no parent; "" when parent has no level
}

// TaxonomyValidator applies a project's Taxonomy to a task. It is pure —
// no repository access, no side effects.
type TaxonomyValidator struct{}

// Check returns nil when task satisfies vc.Taxonomy, otherwise a *TaxonomyError
// wrapping ErrTaxonomyViolation. Empty taxonomies accept any task state.
func (TaxonomyValidator) Check(vc ValidationContext, task *Task) error {
	if vc.Taxonomy.IsEmpty() {
		return nil
	}

	taxonomy := vc.Taxonomy

	if task.Level == nil || *task.Level == "" {
		return &TaxonomyError{Reason: "missing", Taxonomy: taxonomy}
	}

	taskLevel := *task.Level
	taskRank, ok := taxonomy.RankOf(taskLevel)
	if !ok {
		return &TaxonomyError{
			Level:    taskLevel,
			Reason:   "unknown_level",
			Taxonomy: taxonomy,
		}
	}

	if vc.ParentLevel == nil {
		if taskRank != 0 {
			return &TaxonomyError{
				Level:    taskLevel,
				Reason:   "root_requires_top_rank",
				Taxonomy: taxonomy,
			}
		}
		return nil
	}

	parentLevel := *vc.ParentLevel
	if parentLevel == "" {
		// Parent has no level — treat as lowest-possible rank constraint.
		// A task with a declared level cannot sit under a level-less parent.
		return &TaxonomyError{
			Level:       taskLevel,
			ParentLevel: parentLevel,
			Reason:      "parent_rank_not_lower",
			Taxonomy:    taxonomy,
		}
	}

	parentRank, ok := taxonomy.RankOf(parentLevel)
	if !ok || parentRank >= taskRank {
		return &TaxonomyError{
			Level:       taskLevel,
			ParentLevel: parentLevel,
			Reason:      "parent_rank_not_lower",
			Taxonomy:    taxonomy,
		}
	}

	return nil
}
