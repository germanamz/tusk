package mcp

import (
	"fmt"
	"testing"

	"github.com/germanamz/tusk/domain"
)

func TestTaxonomyErrorResult_StructuredPayload(test *testing.T) {
	cases := []struct {
		name  string
		err   *domain.TaxonomyError
		check func(test *testing.T, payload taxonomyErrorPayload)
	}{
		{
			name: "missing",
			err: &domain.TaxonomyError{
				Reason:   "missing",
				Taxonomy: domain.Taxonomy{{"milestone"}, {"story"}},
			},
			check: func(test *testing.T, payload taxonomyErrorPayload) {
				if payload.Code != "taxonomy_violation" {
					test.Errorf("code: got %q", payload.Code)
				}
				if payload.Reason != "missing" {
					test.Errorf("reason: got %q", payload.Reason)
				}
				if len(payload.Taxonomy.Ranks) != 2 || payload.Taxonomy.Ranks[0][0] != "milestone" {
					test.Errorf("taxonomy.ranks: got %+v", payload.Taxonomy.Ranks)
				}
			},
		},
		{
			name: "unknown_level",
			err: &domain.TaxonomyError{
				Reason:   "unknown_level",
				Level:    "bogus",
				Taxonomy: domain.Taxonomy{{"milestone"}},
			},
			check: func(test *testing.T, payload taxonomyErrorPayload) {
				if payload.Level != "bogus" {
					test.Errorf("level: got %q", payload.Level)
				}
			},
		},
		{
			name: "parent_rank_not_lower",
			err: &domain.TaxonomyError{
				Reason:      "parent_rank_not_lower",
				Level:       "milestone",
				ParentLevel: "milestone",
				Taxonomy:    domain.Taxonomy{{"milestone"}, {"story"}},
			},
			check: func(test *testing.T, payload taxonomyErrorPayload) {
				if payload.ParentLevel != "milestone" {
					test.Errorf("parent_level: got %q", payload.ParentLevel)
				}
			},
		},
	}
	for _, caseItem := range cases {
		test.Run(caseItem.name, func(test *testing.T) {
			result := taxonomyErrorResult(caseItem.err)
			if result == nil || !result.IsError {
				test.Fatalf("expected error result, got %+v", result)
			}
			if len(result.Content) == 0 {
				test.Fatalf("expected text content preserved, got none")
			}
			payload, ok := result.StructuredContent.(taxonomyErrorPayload)
			if !ok {
				test.Fatalf("StructuredContent type: got %T", result.StructuredContent)
			}
			caseItem.check(test, payload)
		})
	}
}

func TestMapError(test *testing.T) {
	tests := []struct {
		name    string
		err     error
		context string
		want    string
	}{
		{
			name:    "ErrNotFound",
			err:     domain.ErrNotFound,
			context: "task abc12345",
			want:    "not found: task abc12345",
		},
		{
			name:    "wrapped ErrNotFound",
			err:     fmt.Errorf("lookup: %w", domain.ErrNotFound),
			context: "task abc12345",
			want:    "not found: task abc12345",
		},
		{
			name: "ErrConflict",
			err:  domain.ErrConflict,
			want: "version conflict: task was modified, re-fetch and retry",
		},
		{
			name: "ErrInvalidTransition",
			err:  domain.ErrInvalidTransition,
			want: "invalid status transition",
		},
		{
			name: "ErrCyclicBlock",
			err:  domain.ErrCyclicBlock,
			want: "would create a dependency cycle",
		},
		{
			name: "ErrCyclicParent",
			err:  domain.ErrCyclicParent,
			want: "would create a parent-child cycle",
		},
		{
			name: "ErrDuplicateRelation",
			err:  domain.ErrDuplicateRelation,
			want: "relation already exists",
		},
		{
			name:    "ErrSourceNotFound",
			err:     domain.ErrSourceNotFound,
			context: "task src123",
			want:    "source task not found",
		},
		{
			name:    "ErrTargetNotFound",
			err:     domain.ErrTargetNotFound,
			context: "task tgt456",
			want:    "target task not found",
		},
		{
			name:    "ErrForbidden with context",
			err:     domain.ErrForbidden,
			context: "archive note",
			want:    "forbidden: archive note",
		},
		{
			name: "ErrForbidden no context",
			err:  domain.ErrForbidden,
			want: "forbidden",
		},
		{
			name: "unknown error",
			err:  fmt.Errorf("db connection lost"),
			want: "internal error: db connection lost",
		},
	}

	for _, testCase := range tests {
		test.Run(testCase.name, func(test *testing.T) {
			got := mapError(testCase.err, testCase.context)
			if got != testCase.want {
				test.Errorf("mapError() = %q, want %q", got, testCase.want)
			}
		})
	}
}
