package mcp

import (
	"fmt"
	"testing"

	"github.com/germanamz/tusk/domain"
)

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
