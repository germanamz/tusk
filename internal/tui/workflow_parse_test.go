package tui

import (
	"testing"

	"github.com/germanamz/tusk/domain"
)

func TestParseStatusValue(test *testing.T) {
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
		test.Run(tt.input, func(test *testing.T) {
			name, roles := parseStatusValue(tt.input)
			if name != tt.expectedName {
				test.Fatalf("name: expected %q, got %q", tt.expectedName, name)
			}
			if len(roles) != len(tt.expectedRoles) {
				test.Fatalf("roles: expected %v, got %v", tt.expectedRoles, roles)
			}
			for roleIdx := range roles {
				if string(roles[roleIdx]) != tt.expectedRoles[roleIdx] {
					test.Fatalf("role %d: expected %q, got %q", roleIdx, tt.expectedRoles[roleIdx], roles[roleIdx])
				}
			}
		})
	}
}

func TestParseTransitions(test *testing.T) {
	tests := []struct {
		input    string
		expected []domain.WorkflowTransition
	}{
		{"pending:active", []domain.WorkflowTransition{{FromStatus: "pending", ToStatus: "active"}}},
		{
			"pending:active,active:completed,active:deleted",
			[]domain.WorkflowTransition{
				{FromStatus: "pending", ToStatus: "active"},
				{FromStatus: "active", ToStatus: "completed"},
				{FromStatus: "active", ToStatus: "deleted"},
			},
		},
	}
	for _, tt := range tests {
		test.Run(tt.input, func(test *testing.T) {
			got, err := parseTransitions(tt.input)

			if err != nil {
				test.Fatalf("error: %v", err)
			}

			if len(got) != len(tt.expected) {
				test.Fatalf("expected %d transitions, got %d", len(tt.expected), len(got))
			}
			for idx := range got {
				if got[idx] != tt.expected[idx] {
					test.Fatalf("transition %d: expected %v, got %v", idx, tt.expected[idx], got[idx])
				}
			}
		})
	}
}

func TestParseTransitions_Invalid(test *testing.T) {
	_, err := parseTransitions("invalid")
	if err == nil {
		test.Fatal("expected error for missing colon")
	}
}

func TestParseWorkflowCreate(test *testing.T) {
	args := []string{
		"status=pending(initial)",
		"status=active(start,highlight)",
		"status=completed(terminal,done,dim)",
		"status=deleted(terminal,delete,dim)",
		"transition=pending:active,active:completed,active:deleted",
	}

	workflow, err := parseWorkflowCreate(args)

	if err != nil {
		test.Fatalf("error: %v", err)
	}

	if len(workflow.Statuses) != 4 {
		test.Fatalf("expected 4 statuses, got %d", len(workflow.Statuses))
	}
	pending := workflow.Statuses["pending"]
	if len(pending.Roles) != 1 || pending.Roles[0] != domain.RoleInitial {
		test.Fatalf("pending roles: expected [initial], got %v", pending.Roles)
	}
	if len(workflow.Transitions) != 3 {
		test.Fatalf("expected 3 transitions, got %d", len(workflow.Transitions))
	}
}

func TestParseWorkflowCreate_NoRoles(test *testing.T) {
	args := []string{
		"status=pending(initial)",
		"status=active(start,highlight)",
		"status=review",
		"status=completed(terminal,done,dim)",
		"status=deleted(terminal,delete,dim)",
		"transition=pending:active,active:review,review:completed,active:deleted",
	}

	workflow, err := parseWorkflowCreate(args)

	if err != nil {
		test.Fatalf("error: %v", err)
	}

	review := workflow.Statuses["review"]
	if len(review.Roles) != 0 {
		test.Fatalf("review should have no roles, got %v", review.Roles)
	}
}

func TestParseWorkflowCreate_Empty(test *testing.T) {
	_, err := parseWorkflowCreate([]string{})
	if err == nil {
		test.Fatal("expected error for empty args")
	}
}

func TestParseWorkflowModify_Add(test *testing.T) {
	args := []string{
		"+status=review",
		"+transition=active:review,review:completed",
	}

	mut, err := parseWorkflowModify(args)

	if err != nil {
		test.Fatalf("error: %v", err)
	}

	if len(mut.AddStatuses) != 1 {
		test.Fatalf("expected 1 added status, got %d", len(mut.AddStatuses))
	}
	if _, ok := mut.AddStatuses["review"]; !ok {
		test.Fatal("expected 'review' in added statuses")
	}
	if len(mut.AddTransitions) != 2 {
		test.Fatalf("expected 2 added transitions, got %d", len(mut.AddTransitions))
	}
}

func TestParseWorkflowModify_Remove(test *testing.T) {
	args := []string{
		"-status=review",
		"-transition=active:review",
	}

	mut, err := parseWorkflowModify(args)

	if err != nil {
		test.Fatalf("error: %v", err)
	}

	if len(mut.RemoveStatuses) != 1 || mut.RemoveStatuses[0] != "review" {
		test.Fatalf("expected [review] removed, got %v", mut.RemoveStatuses)
	}
	if len(mut.RemoveTransitions) != 1 {
		test.Fatalf("expected 1 removed transition, got %d", len(mut.RemoveTransitions))
	}
}

func TestParseWorkflowModify_Set(test *testing.T) {
	args := []string{
		"status=active(start,highlight)",
	}

	mut, err := parseWorkflowModify(args)

	if err != nil {
		test.Fatalf("error: %v", err)
	}

	if len(mut.SetStatuses) != 1 {
		test.Fatalf("expected 1 set status, got %d", len(mut.SetStatuses))
	}
	active := mut.SetStatuses["active"]
	if len(active.Roles) != 2 {
		test.Fatalf("expected 2 roles, got %v", active.Roles)
	}
}

func TestParseWorkflowModifyAddsStatusWithPlus(test *testing.T) {
	mut, err := parseWorkflowModify([]string{"+status=review(highlight)"})

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	got, ok := mut.AddStatuses["review"]
	if !ok {
		test.Fatalf("expected review in AddStatuses, got %+v", mut.AddStatuses)
	}
	if len(got.Roles) != 1 || got.Roles[0] != domain.RoleHighlight {
		test.Errorf("roles = %+v, want [highlight]", got.Roles)
	}
	if _, hit := mut.SetStatuses["review"]; hit {
		test.Errorf("SetStatuses should be empty, got %+v", mut.SetStatuses)
	}
}

func TestParseWorkflowModifyRemovesStatusWithMinus(test *testing.T) {
	mut, err := parseWorkflowModify([]string{"-status=done"})

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if len(mut.RemoveStatuses) != 1 || mut.RemoveStatuses[0] != "done" {
		test.Errorf("RemoveStatuses = %+v", mut.RemoveStatuses)
	}
}

func TestParseWorkflowModifySetsBareStatus(test *testing.T) {
	mut, err := parseWorkflowModify([]string{"status=active(start,highlight)"})

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	got, ok := mut.SetStatuses["active"]
	if !ok {
		test.Fatalf("expected active in SetStatuses, got %+v", mut.SetStatuses)
	}
	if len(got.Roles) != 2 || got.Roles[0] != domain.RoleStart || got.Roles[1] != domain.RoleHighlight {
		test.Errorf("roles = %+v", got.Roles)
	}
}

func TestParseWorkflowModifyTransitionRequiresModifier(test *testing.T) {
	_, err := parseWorkflowModify([]string{"transition=pending:active"})
	if err == nil {
		test.Fatalf("expected error for bare transition")
	}
}

func TestParseWorkflowModifyAddAndRemoveTransitions(test *testing.T) {
	mut, err := parseWorkflowModify([]string{
		"+transition=pending:active",
		"-transition=active:done",
	})

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if len(mut.AddTransitions) != 1 ||
		mut.AddTransitions[0] != (domain.WorkflowTransition{FromStatus: "pending", ToStatus: "active"}) {
		test.Errorf("AddTransitions = %+v", mut.AddTransitions)
	}
	if len(mut.RemoveTransitions) != 1 ||
		mut.RemoveTransitions[0] != (domain.WorkflowTransition{FromStatus: "active", ToStatus: "done"}) {
		test.Errorf("RemoveTransitions = %+v", mut.RemoveTransitions)
	}
}

func TestParseWorkflowCreateRejectsModifier(test *testing.T) {
	_, err := parseWorkflowCreate([]string{
		"status=pending(initial)",
		"status=active(start,highlight)",
		"status=done(terminal,done)",
		"+transition=pending:active",
	})

	if err == nil {
		test.Fatalf("expected error for modifier on workflow create")
	}
}

func TestParseWorkflowCreateHappyPath(test *testing.T) {
	workflow, err := parseWorkflowCreate([]string{
		"status=pending(initial)",
		"status=active(start,highlight)",
		"status=done(terminal,done)",
		"transition=pending:active",
		"transition=active:done",
	})

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if _, ok := workflow.Statuses["pending"]; !ok {
		test.Errorf("missing pending status: %+v", workflow.Statuses)
	}
	if len(workflow.Transitions) != 2 {
		test.Errorf("transitions = %+v", workflow.Transitions)
	}
}
