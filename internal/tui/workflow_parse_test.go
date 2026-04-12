package tui

import (
	"testing"

	"github.com/germanamz/tusk/config"
)

func TestParseStatusValue(t *testing.T) {
	tests := []struct {
		input         string
		expectedName  string
		expectedRoles []string
	}{
		{"pending(initial)", "pending", []string{"initial"}},
		{"active(start,highlight)", "active", []string{"start", "highlight"}},
		{"review", "review", nil},
		{"completed(terminal,done,dim)", "completed", []string{"terminal", "done", "dim"}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			name, roles := parseStatusValue(tt.input)
			if name != tt.expectedName {
				t.Fatalf("name: expected %q, got %q", tt.expectedName, name)
			}
			if len(roles) != len(tt.expectedRoles) {
				t.Fatalf("roles: expected %v, got %v", tt.expectedRoles, roles)
			}
			for i := range roles {
				if roles[i] != tt.expectedRoles[i] {
					t.Fatalf("role %d: expected %q, got %q", i, tt.expectedRoles[i], roles[i])
				}
			}
		})
	}
}

func TestParseTransitions(t *testing.T) {
	tests := []struct {
		input    string
		expected []config.WorkflowTransitionConfig
	}{
		{"pending:active", []config.WorkflowTransitionConfig{{From: "pending", To: "active"}}},
		{
			"pending:active,active:completed,active:deleted",
			[]config.WorkflowTransitionConfig{
				{From: "pending", To: "active"},
				{From: "active", To: "completed"},
				{From: "active", To: "deleted"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseTransitions(tt.input)
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if len(got) != len(tt.expected) {
				t.Fatalf("expected %d transitions, got %d", len(tt.expected), len(got))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("transition %d: expected %v, got %v", i, tt.expected[i], got[i])
				}
			}
		})
	}
}

func TestParseTransitions_Invalid(t *testing.T) {
	_, err := parseTransitions("invalid")
	if err == nil {
		t.Fatal("expected error for missing colon")
	}
}

func TestParseWorkflowCreate(t *testing.T) {
	args := []string{
		"status=pending(initial)",
		"status=active(start,highlight)",
		"status=completed(terminal,done,dim)",
		"status=deleted(terminal,delete,dim)",
		"transition=pending:active,active:completed,active:deleted",
	}
	wf, err := parseWorkflowCreate(args)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(wf.Statuses) != 4 {
		t.Fatalf("expected 4 statuses, got %d", len(wf.Statuses))
	}
	pending := wf.Statuses["pending"]
	if len(pending.Roles) != 1 || pending.Roles[0] != "initial" {
		t.Fatalf("pending roles: expected [initial], got %v", pending.Roles)
	}
	if len(wf.Transitions) != 3 {
		t.Fatalf("expected 3 transitions, got %d", len(wf.Transitions))
	}
}

func TestParseWorkflowCreate_NoRoles(t *testing.T) {
	args := []string{
		"status=pending(initial)",
		"status=active(start,highlight)",
		"status=review",
		"status=completed(terminal,done,dim)",
		"status=deleted(terminal,delete,dim)",
		"transition=pending:active,active:review,review:completed,active:deleted",
	}
	wf, err := parseWorkflowCreate(args)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	review := wf.Statuses["review"]
	if len(review.Roles) != 0 {
		t.Fatalf("review should have no roles, got %v", review.Roles)
	}
}

func TestParseWorkflowCreate_Empty(t *testing.T) {
	_, err := parseWorkflowCreate([]string{})
	if err == nil {
		t.Fatal("expected error for empty args")
	}
}

func TestParseWorkflowModify_Add(t *testing.T) {
	args := []string{
		"+status=review",
		"+transition=active:review,review:completed",
	}
	mut, err := parseWorkflowModify(args)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(mut.AddStatuses) != 1 {
		t.Fatalf("expected 1 added status, got %d", len(mut.AddStatuses))
	}
	if _, ok := mut.AddStatuses["review"]; !ok {
		t.Fatal("expected 'review' in added statuses")
	}
	if len(mut.AddTransitions) != 2 {
		t.Fatalf("expected 2 added transitions, got %d", len(mut.AddTransitions))
	}
}

func TestParseWorkflowModify_Remove(t *testing.T) {
	args := []string{
		"-status=review",
		"-transition=active:review",
	}
	mut, err := parseWorkflowModify(args)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(mut.RemoveStatuses) != 1 || mut.RemoveStatuses[0] != "review" {
		t.Fatalf("expected [review] removed, got %v", mut.RemoveStatuses)
	}
	if len(mut.RemoveTransitions) != 1 {
		t.Fatalf("expected 1 removed transition, got %d", len(mut.RemoveTransitions))
	}
}

func TestParseWorkflowModify_Set(t *testing.T) {
	args := []string{
		"status=active(start,highlight)",
	}
	mut, err := parseWorkflowModify(args)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(mut.SetStatuses) != 1 {
		t.Fatalf("expected 1 set status, got %d", len(mut.SetStatuses))
	}
	active := mut.SetStatuses["active"]
	if len(active.Roles) != 2 {
		t.Fatalf("expected 2 roles, got %v", active.Roles)
	}
}
