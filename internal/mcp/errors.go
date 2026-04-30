package mcp

import (
	"errors"
	"fmt"

	"github.com/germanamz/tusk/domain"
	"github.com/mark3labs/mcp-go/mcp"
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
	case errors.Is(err, domain.ErrOrderGapExhausted):
		return err.Error()
	case errors.Is(err, domain.ErrDuplicateRelation):
		return "relation already exists"
	case errors.Is(err, domain.ErrForbidden):
		if context != "" {
			return fmt.Sprintf("forbidden: %s", context)
		}
		return "forbidden"
	default:
		return fmt.Sprintf("internal error: %s", err.Error())
	}
}

// taxonomyErrorPayload is the structured tool-error payload emitted when an
// MCP tool call fails with *domain.TaxonomyError. Mirrors the shape defined
// in the Phase 5 design spec so clients can branch on `code` alone.
type taxonomyErrorPayload struct {
	Code        string           `json:"code"`
	Reason      string           `json:"reason"`
	Level       string           `json:"level,omitempty"`
	ParentLevel string           `json:"parent_level,omitempty"`
	Taxonomy    taxonomyRanksMsg `json:"taxonomy"`
}

// taxonomyRanksMsg wraps the taxonomy ranks slice in the `{"ranks": [...]}`
// envelope used everywhere else on the MCP surface.
type taxonomyRanksMsg struct {
	Ranks [][]string `json:"ranks"`
}

// taxonomyErrorResult returns an MCP error result carrying both the
// human-readable message (preserved for agents that ignore structured
// payloads) and the structured `taxonomy_violation` payload. Callers should
// check errors.As(err, *domain.TaxonomyError) before invoking this helper.
func taxonomyErrorResult(taxonomyErr *domain.TaxonomyError) *mcp.CallToolResult {
	ranks := [][]string(taxonomyErr.Taxonomy)
	if ranks == nil {
		ranks = [][]string{}
	}
	payload := taxonomyErrorPayload{
		Code:        "taxonomy_violation",
		Reason:      taxonomyErr.Reason,
		Level:       taxonomyErr.Level,
		ParentLevel: taxonomyErr.ParentLevel,
		Taxonomy:    taxonomyRanksMsg{Ranks: ranks},
	}
	result := mcp.NewToolResultError(taxonomyErr.Error())
	result.StructuredContent = payload
	return result
}
