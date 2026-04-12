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

func TestParseWorkflowModifyAddsStatusWithPlus(t *testing.T) {
	mut, err := parseWorkflowModify([]string{"+status=review(highlight)"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := mut.AddStatuses["review"]
	if !ok {
		t.Fatalf("expected review in AddStatuses, got %+v", mut.AddStatuses)
	}
	if len(got.Roles) != 1 || got.Roles[0] != "highlight" {
		t.Errorf("roles = %+v, want [highlight]", got.Roles)
	}
	if _, hit := mut.SetStatuses["review"]; hit {
		t.Errorf("SetStatuses should be empty, got %+v", mut.SetStatuses)
	}
}

func TestParseWorkflowModifyRemovesStatusWithMinus(t *testing.T) {
	mut, err := parseWorkflowModify([]string{"-status=done"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mut.RemoveStatuses) != 1 || mut.RemoveStatuses[0] != "done" {
		t.Errorf("RemoveStatuses = %+v", mut.RemoveStatuses)
	}
}

func TestParseWorkflowModifySetsBareStatus(t *testing.T) {
	mut, err := parseWorkflowModify([]string{"status=active(start,highlight)"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := mut.SetStatuses["active"]
	if !ok {
		t.Fatalf("expected active in SetStatuses, got %+v", mut.SetStatuses)
	}
	if len(got.Roles) != 2 || got.Roles[0] != "start" || got.Roles[1] != "highlight" {
		t.Errorf("roles = %+v", got.Roles)
	}
}

func TestParseWorkflowModifyTransitionRequiresModifier(t *testing.T) {
	_, err := parseWorkflowModify([]string{"transition=pending:active"})
	if err == nil {
		t.Fatalf("expected error for bare transition")
	}
}

func TestParseWorkflowModifyAddAndRemoveTransitions(t *testing.T) {
	mut, err := parseWorkflowModify([]string{
		"+transition=pending:active",
		"-transition=active:done",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mut.AddTransitions) != 1 ||
		mut.AddTransitions[0] != (config.WorkflowTransitionConfig{From: "pending", To: "active"}) {
		t.Errorf("AddTransitions = %+v", mut.AddTransitions)
	}
	if len(mut.RemoveTransitions) != 1 ||
		mut.RemoveTransitions[0] != (config.WorkflowTransitionConfig{From: "active", To: "done"}) {
		t.Errorf("RemoveTransitions = %+v", mut.RemoveTransitions)
	}
}

func TestParseWorkflowCreateRejectsModifier(t *testing.T) {
	_, err := parseWorkflowCreate([]string{
		"status=pending(initial)",
		"status=active(start,highlight)",
		"status=done(terminal,done)",
		"+transition=pending:active",
	})
	if err == nil {
		t.Fatalf("expected error for modifier on workflow create")
	}
}

func TestParseWorkflowCreateHappyPath(t *testing.T) {
	wf, err := parseWorkflowCreate([]string{
		"status=pending(initial)",
		"status=active(start,highlight)",
		"status=done(terminal,done)",
		"transition=pending:active",
		"transition=active:done",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := wf.Statuses["pending"]; !ok {
		t.Errorf("missing pending status: %+v", wf.Statuses)
	}
	if len(wf.Transitions) != 2 {
		t.Errorf("transitions = %+v", wf.Transitions)
	}
}
