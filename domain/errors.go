package domain

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("version conflict")
	ErrCyclicBlock       = errors.New("relation would create a cycle in blocks graph")
	ErrCyclicParent      = errors.New("parent would create a cycle in task hierarchy")
	ErrInvalidTransition = errors.New("status transition not allowed by workflow")
	ErrDuplicateRelation = errors.New("relation already exists")
	ErrTagInUse          = errors.New("tag is assigned to tasks")
	ErrTaskClaimed       = errors.New("task is already claimed by another player")
	ErrNoAvailableTasks  = errors.New("no available tasks")
	ErrSourceNotFound    = fmt.Errorf("source task: %w", ErrNotFound)
	ErrTargetNotFound    = fmt.Errorf("target task: %w", ErrNotFound)
	ErrWorkflowInUse     = errors.New("workflow is referenced by one or more projects")
	ErrProjectHasTasks   = errors.New("project has referencing tasks")
	ErrInvalidWorkflow   = errors.New("invalid workflow")
	ErrBuiltInWorkflow   = errors.New("built-in workflow cannot be modified")
	ErrForbidden         = errors.New("forbidden")
	ErrTaxonomyViolation = errors.New("task violates project taxonomy")

	// ErrOrderGapExhausted indicates no float64 midpoint remains between neighbors.
	// The wrapper message produced by the service layer appends the sibling group's
	// parent short ID so the `tusk task move --resequence <parent>` command is
	// copy-pasteable.
	ErrOrderGapExhausted = errors.New("no float64 midpoint remains between neighbors")
)

// TaxonomyError describes how a task violates its project's taxonomy.
// It wraps ErrTaxonomyViolation so errors.Is(err, ErrTaxonomyViolation) is true.
type TaxonomyError struct {
	Level       string   // level the task was assigned ("" when missing)
	ParentLevel string   // parent's level ("" when no parent or parent has no level)
	Reason      string   // "missing" | "unknown_level" | "root_requires_top_rank" | "parent_rank_not_lower"
	Taxonomy    Taxonomy // taxonomy that produced the violation; for rendering
}

func (e *TaxonomyError) Error() string {
	switch e.Reason {
	case "missing":
		return "task violates project taxonomy: level is required"
	case "unknown_level":
		return fmt.Sprintf("task violates project taxonomy: level %q is not declared", e.Level)
	case "root_requires_top_rank":
		return fmt.Sprintf("task violates project taxonomy: root task with level %q must be at top rank", e.Level)
	case "parent_rank_not_lower":
		return fmt.Sprintf("task violates project taxonomy: level %q cannot sit under parent level %q", e.Level, e.ParentLevel)
	default:
		return "task violates project taxonomy"
	}
}

func (e *TaxonomyError) Unwrap() error { return ErrTaxonomyViolation }
