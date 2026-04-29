package tui

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

func TestRenderWorkflowsTOML_Empty(test *testing.T) {
	got := RenderWorkflowsTOML(nil)
	if !strings.Contains(got, "# workflows") {
		test.Fatalf("expected header in empty render, got %q", got)
	}
	if strings.Contains(got, "[workflows.") {
		test.Fatalf("did not expect any workflow tables in empty render, got %q", got)
	}
}

func TestRenderWorkflowsTOML_Kanban(test *testing.T) {
	wfs := []*domain.Workflow{
		{
			Name: "kanban",
			Statuses: map[string]domain.StatusConfig{
				"pending":   {Roles: []domain.StatusRole{domain.RoleInitial}},
				"active":    {Roles: []domain.StatusRole{domain.RoleStart, domain.RoleHighlight}},
				"completed": {Roles: []domain.StatusRole{domain.RoleTerminal, domain.RoleDone, domain.RoleDim}},
			},
			Transitions: []domain.WorkflowTransition{
				{FromStatus: "pending", ToStatus: "active"},
				{FromStatus: "active", ToStatus: "completed"},
			},
		},
	}

	got := RenderWorkflowsTOML(wfs)

	want := []string{
		"[workflows.kanban.statuses.active]",
		`roles = ["start", "highlight"]`,
		"[workflows.kanban.statuses.completed]",
		`roles = ["terminal", "done", "dim"]`,
		"[workflows.kanban.statuses.pending]",
		`roles = ["initial"]`,
		"[[workflows.kanban.transitions]]",
		`from = "pending"`,
		`to = "active"`,
		`from = "active"`,
		`to = "completed"`,
	}
	for _, line := range want {
		if !strings.Contains(got, line) {
			test.Errorf("missing %q in:\n%s", line, got)
		}
	}

	// Status tables ordered alphabetically: active, completed, pending.
	iActive := strings.Index(got, "[workflows.kanban.statuses.active]")
	iCompleted := strings.Index(got, "[workflows.kanban.statuses.completed]")
	iPending := strings.Index(got, "[workflows.kanban.statuses.pending]")
	if iActive >= iCompleted || iCompleted >= iPending {
		test.Fatalf("expected sorted status order, got:\n%s", got)
	}

	// Transitions preserve domain order.
	iTrans := strings.Index(got, "[[workflows.kanban.transitions]]")
	iFromPending := strings.Index(got[iTrans:], `from = "pending"`)
	iFromActive := strings.Index(got[iTrans:], `from = "active"`)
	if iFromPending < 0 || iFromActive < 0 || iFromPending > iFromActive {
		test.Fatalf("expected pending transition before active transition, got:\n%s", got)
	}

	// Determinism: two calls produce identical output.
	again := RenderWorkflowsTOML(wfs)
	if got != again {
		test.Fatalf("renderer not deterministic:\nfirst:\n%s\nsecond:\n%s", got, again)
	}
}

func TestRenderWorkflowsTOML_MultiWorkflowSorted(test *testing.T) {
	wfs := []*domain.Workflow{
		{
			Name:     "zeta",
			Statuses: map[string]domain.StatusConfig{"x": {Roles: []domain.StatusRole{domain.RoleInitial}}},
		},
		{
			Name:     "alpha",
			Statuses: map[string]domain.StatusConfig{"y": {Roles: []domain.StatusRole{domain.RoleInitial}}},
		},
	}
	got := RenderWorkflowsTOML(wfs)
	iAlpha := strings.Index(got, "[workflows.alpha.")
	iZeta := strings.Index(got, "[workflows.zeta.")
	if iAlpha < 0 || iZeta < 0 || iAlpha > iZeta {
		test.Fatalf("expected alpha before zeta, got:\n%s", got)
	}
}

func TestRenderProjectsTOML_Empty(test *testing.T) {
	got := RenderProjectsTOML(nil, nil)
	if !strings.Contains(got, "# projects") {
		test.Fatalf("expected header in empty render, got %q", got)
	}
	if strings.Contains(got, "[projects.") {
		test.Fatalf("did not expect any project tables in empty render, got %q", got)
	}
}

func TestRenderProjectsTOML_DefaultKanban(test *testing.T) {
	wfID := uuid.New()
	workflow := &domain.Workflow{ID: wfID, Name: "kanban"}
	projects := []*domain.Project{
		{Name: "default", WorkflowID: wfID},
	}
	got := RenderProjectsTOML(projects, map[uuid.UUID]*domain.Workflow{wfID: workflow})

	want := []string{
		"[projects.default]",
		`workflow = "kanban"`,
	}
	for _, line := range want {
		if !strings.Contains(got, line) {
			test.Errorf("missing %q in:\n%s", line, got)
		}
	}
}

func TestRenderProjectsTOML_UrgencyOverrides(test *testing.T) {
	wfID := uuid.New()
	workflow := &domain.Workflow{ID: wfID, Name: "kanban"}
	blocking := 15.0
	due := 9.5
	projects := []*domain.Project{
		{
			Name:       "backend",
			WorkflowID: wfID,
			Settings: domain.ProjectSettings{
				Urgency: &domain.UrgencyOverrides{
					BlockingWeight: &blocking,
					DueWeight:      &due,
				},
			},
		},
	}
	got := RenderProjectsTOML(projects, map[uuid.UUID]*domain.Workflow{wfID: workflow})

	want := []string{
		"[projects.backend]",
		`workflow = "kanban"`,
		"[projects.backend.settings.urgency]",
		"blocking_weight = 15.0",
		"due_weight = 9.5",
	}
	for _, line := range want {
		if !strings.Contains(got, line) {
			test.Errorf("missing %q in:\n%s", line, got)
		}
	}
	// Unset urgency fields must be omitted.
	if strings.Contains(got, "priority_weight") {
		test.Errorf("expected unset priority_weight to be omitted, got:\n%s", got)
	}
}

func TestRenderProjectsTOML_AutoCompleteRevert(test *testing.T) {
	wfID := uuid.New()
	workflow := &domain.Workflow{ID: wfID, Name: "kanban"}
	projects := []*domain.Project{
		{
			Name:       "flows",
			WorkflowID: wfID,
			Settings: domain.ProjectSettings{
				AutoCompleteParent: &domain.AutoCompleteConfig{
					TriggerStatus: "completed",
					TargetStatus:  "completed",
				},
				AutoRevertParent: &domain.AutoRevertConfig{
					TriggerStatus: "active",
					TargetStatus:  "pending",
				},
			},
		},
	}
	got := RenderProjectsTOML(projects, map[uuid.UUID]*domain.Workflow{wfID: workflow})
	want := []string{
		"[projects.flows.settings.auto_complete_parent]",
		`trigger_status = "completed"`,
		`target_status = "completed"`,
		"[projects.flows.settings.auto_revert_parent]",
		`trigger_status = "active"`,
		`target_status = "pending"`,
	}
	for _, line := range want {
		if !strings.Contains(got, line) {
			test.Errorf("missing %q in:\n%s", line, got)
		}
	}
}

func TestRenderProjectsTOML_Deterministic(test *testing.T) {
	wfID := uuid.New()
	workflow := &domain.Workflow{ID: wfID, Name: "kanban"}
	lookup := map[uuid.UUID]*domain.Workflow{wfID: workflow}
	projects := []*domain.Project{
		{Name: "zeta", WorkflowID: wfID},
		{Name: "alpha", WorkflowID: wfID},
	}
	first := RenderProjectsTOML(projects, lookup)
	second := RenderProjectsTOML(projects, lookup)
	if first != second {
		test.Fatalf("renderer not deterministic:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	iAlpha := strings.Index(first, "[projects.alpha]")
	iZeta := strings.Index(first, "[projects.zeta]")
	if iAlpha < 0 || iZeta < 0 || iAlpha > iZeta {
		test.Fatalf("expected alpha before zeta, got:\n%s", first)
	}
}
