package mcp

import (
	"errors"
	"fmt"

	"github.com/germanamz/tusk/domain"
)

// mapError translates domain sentinel errors into user-facing MCP error strings.
// The context parameter adds specificity (e.g., "task abc12345") for not-found errors.
func mapError(err error, context string) string {
	switch {
	case errors.Is(err, domain.ErrSourceNotFound):
		return "source task not found"
	case errors.Is(err, domain.ErrTargetNotFound):
		return "target task not found"
	case errors.Is(err, domain.ErrNotFound):
		if context != "" {
			return fmt.Sprintf("not found: %s", context)
		}
		return "not found"
	case errors.Is(err, domain.ErrConflict):
		return "version conflict: task was modified, re-fetch and retry"
	case errors.Is(err, domain.ErrInvalidTransition):
		return "invalid status transition"
	case errors.Is(err, domain.ErrCyclicBlock):
		return "would create a dependency cycle"
	case errors.Is(err, domain.ErrCyclicParent):
		return "would create a parent-child cycle"
	case errors.Is(err, domain.ErrDuplicateRelation):
		return "relation already exists"
	default:
		return fmt.Sprintf("internal error: %s", err.Error())
	}
}
