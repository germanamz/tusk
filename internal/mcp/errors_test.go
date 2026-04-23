package mcp

import (
	"fmt"
	"testing"

	"github.com/germanamz/tusk/domain"
)

func TestTaxonomyErrorResult_StructuredPayload(t *testing.T) {
	cases := []struct {
		name  string
		err   *domain.TaxonomyError
		check func(t *testing.T, payload taxonomyErrorPayload)
	}{
		{
			name: "missing",
			err: &domain.TaxonomyError{
				Reason:   "missing",
				Taxonomy: domain.Taxonomy{{"milestone"}, {"story"}},
			},
			check: func(t *testing.T, p taxonomyErrorPayload) {
				if p.Code != "taxonomy_violation" {
					t.Errorf("code: got %q", p.Code)
				}
				if p.Reason != "missing" {
					t.Errorf("reason: got %q", p.Reason)
				}
				if len(p.Taxonomy.Ranks) != 2 || p.Taxonomy.Ranks[0][0] != "milestone" {
					t.Errorf("taxonomy.ranks: got %+v", p.Taxonomy.Ranks)
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
			check: func(t *testing.T, p taxonomyErrorPayload) {
				if p.Level != "bogus" {
					t.Errorf("level: got %q", p.Level)
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
			check: func(t *testing.T, p taxonomyErrorPayload) {
				if p.ParentLevel != "milestone" {
					t.Errorf("parent_level: got %q", p.ParentLevel)
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := taxonomyErrorResult(c.err)
			if result == nil || !result.IsError {
				t.Fatalf("expected error result, got %+v", result)
			}
			if len(result.Content) == 0 {
				t.Fatalf("expected text content preserved, got none")
			}
			payload, ok := result.StructuredContent.(taxonomyErrorPayload)
			if !ok {
				t.Fatalf("StructuredContent type: got %T", result.StructuredContent)
			}
			c.check(t, payload)
		})
	}
}

func TestMapError(t *testing.T) {
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapError(tt.err, tt.context)
			if got != tt.want {
				t.Errorf("mapError() = %q, want %q", got, tt.want)
			}
		})
	}
}
