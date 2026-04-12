package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTestConfig writes a TOML config to a temp file and returns the path.
func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	return path
}

// baseConfig is a minimal valid config with kanban workflow and default project.
const baseConfig = `
[workflows.kanban.statuses.pending]
roles = ["initial"]
[workflows.kanban.statuses.active]
roles = ["start", "highlight"]
[workflows.kanban.statuses.completed]
roles = ["terminal", "done", "dim"]
[workflows.kanban.statuses.deleted]
roles = ["terminal", "delete", "dim"]
[[workflows.kanban.transitions]]
from = "pending"
to = "active"
[[workflows.kanban.transitions]]
from = "active"
to = "completed"
[[workflows.kanban.transitions]]
from = "active"
to = "deleted"

[projects.default]
workflow = "kanban"
`

func TestCreateWorkflow(t *testing.T) {
	path := writeTestConfig(t, baseConfig)

	wf := WorkflowConfig{
		Statuses: map[string]StatusConfig{
			"todo":    {Roles: []string{"initial"}},
			"doing":   {Roles: []string{"start", "highlight"}},
			"done":    {Roles: []string{"terminal", "done", "dim"}},
			"removed": {Roles: []string{"terminal", "delete", "dim"}},
		},
		Transitions: []WorkflowTransitionConfig{
			{From: "todo", To: "doing"},
			{From: "doing", To: "done"},
			{From: "doing", To: "removed"},
		},
	}

	if err := CreateWorkflow(path, "sprint", wf); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile after create: %v", err)
	}
	if _, ok := cfg.Workflows["sprint"]; !ok {
		t.Fatal("expected 'sprint' workflow in config")
	}
	if len(cfg.Workflows["sprint"].Statuses) != 4 {
		t.Fatalf("expected 4 statuses, got %d", len(cfg.Workflows["sprint"].Statuses))
	}
	if _, ok := cfg.Workflows["kanban"]; !ok {
		t.Fatal("expected 'kanban' workflow preserved")
	}
}

func TestCreateWorkflow_AlreadyExists(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	wf := WorkflowConfig{
		Statuses: map[string]StatusConfig{
			"a": {Roles: []string{"initial"}},
			"b": {Roles: []string{"start", "highlight"}},
			"c": {Roles: []string{"terminal", "done", "dim"}},
			"d": {Roles: []string{"terminal", "delete", "dim"}},
		},
		Transitions: []WorkflowTransitionConfig{{From: "a", To: "b"}, {From: "b", To: "c"}, {From: "b", To: "d"}},
	}
	err := CreateWorkflow(path, "kanban", wf)
	if err == nil {
		t.Fatal("expected error creating duplicate workflow")
	}
}

func TestCreateWorkflow_InvalidRoles(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	wf := WorkflowConfig{
		Statuses: map[string]StatusConfig{
			"a": {Roles: []string{"start"}}, // no initial
		},
		Transitions: []WorkflowTransitionConfig{},
	}
	err := CreateWorkflow(path, "bad", wf)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDeleteWorkflow(t *testing.T) {
	content := baseConfig + `
[workflows.sprint.statuses.todo]
roles = ["initial"]
[workflows.sprint.statuses.doing]
roles = ["start", "highlight"]
[workflows.sprint.statuses.done]
roles = ["terminal", "done", "dim"]
[workflows.sprint.statuses.removed]
roles = ["terminal", "delete", "dim"]
[[workflows.sprint.transitions]]
from = "todo"
to = "doing"
[[workflows.sprint.transitions]]
from = "doing"
to = "done"
[[workflows.sprint.transitions]]
from = "doing"
to = "removed"
`
	path := writeTestConfig(t, content)

	if err := DeleteWorkflow(path, "sprint"); err != nil {
		t.Fatalf("DeleteWorkflow: %v", err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if _, ok := cfg.Workflows["sprint"]; ok {
		t.Fatal("expected sprint removed")
	}
	if _, ok := cfg.Workflows["kanban"]; !ok {
		t.Fatal("expected kanban preserved")
	}
}

func TestDeleteWorkflow_InUse(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	err := DeleteWorkflow(path, "kanban")
	if err == nil {
		t.Fatal("expected error deleting workflow in use by project")
	}
}

func TestDeleteWorkflow_NotFound(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	err := DeleteWorkflow(path, "nonexistent")
	if err == nil {
		t.Fatal("expected error deleting nonexistent workflow")
	}
}

func TestModifyWorkflow_SetRoles(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	mut := WorkflowMutation{
		SetStatuses: map[string]StatusConfig{
			"active": {Roles: []string{"start", "highlight"}},
		},
	}
	if err := ModifyWorkflow(path, "kanban", mut); err != nil {
		t.Fatalf("ModifyWorkflow: %v", err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	active := cfg.Workflows["kanban"].Statuses["active"]
	if len(active.Roles) != 2 {
		t.Fatalf("expected 2 roles, got %d: %v", len(active.Roles), active.Roles)
	}
}

func TestModifyWorkflow_AddStatus(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	mut := WorkflowMutation{
		AddStatuses: map[string]StatusConfig{
			"review": {},
		},
		AddTransitions: []WorkflowTransitionConfig{
			{From: "active", To: "review"},
			{From: "review", To: "completed"},
		},
	}
	if err := ModifyWorkflow(path, "kanban", mut); err != nil {
		t.Fatalf("ModifyWorkflow: %v", err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if _, ok := cfg.Workflows["kanban"].Statuses["review"]; !ok {
		t.Fatal("expected 'review' status after add")
	}
	if len(cfg.Workflows["kanban"].Transitions) != 5 {
		t.Fatalf("expected 5 transitions, got %d", len(cfg.Workflows["kanban"].Transitions))
	}
}

func TestModifyWorkflow_RemoveStatus(t *testing.T) {
	content := baseConfig
	path := writeTestConfig(t, content)

	mut1 := WorkflowMutation{
		AddStatuses:    map[string]StatusConfig{"review": {}},
		AddTransitions: []WorkflowTransitionConfig{{From: "active", To: "review"}, {From: "review", To: "completed"}},
	}
	if err := ModifyWorkflow(path, "kanban", mut1); err != nil {
		t.Fatalf("setup add: %v", err)
	}

	mut2 := WorkflowMutation{
		RemoveStatuses: []string{"review"},
	}
	if err := ModifyWorkflow(path, "kanban", mut2); err != nil {
		t.Fatalf("ModifyWorkflow remove: %v", err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if _, ok := cfg.Workflows["kanban"].Statuses["review"]; ok {
		t.Fatal("expected review removed")
	}
	for _, tr := range cfg.Workflows["kanban"].Transitions {
		if tr.From == "review" || tr.To == "review" {
			t.Fatalf("transition referencing removed status: %s->%s", tr.From, tr.To)
		}
	}
}

func TestModifyWorkflow_RemoveTransitions(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	mut1 := WorkflowMutation{
		AddTransitions: []WorkflowTransitionConfig{{From: "completed", To: "pending"}},
	}
	if err := ModifyWorkflow(path, "kanban", mut1); err != nil {
		t.Fatalf("setup: %v", err)
	}

	mut2 := WorkflowMutation{
		RemoveTransitions: []WorkflowTransitionConfig{{From: "completed", To: "pending"}},
	}
	if err := ModifyWorkflow(path, "kanban", mut2); err != nil {
		t.Fatalf("remove transition: %v", err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(cfg.Workflows["kanban"].Transitions) != 3 {
		t.Fatalf("expected 3 transitions, got %d", len(cfg.Workflows["kanban"].Transitions))
	}
}

func TestModifyWorkflow_NotFound(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	err := ModifyWorkflow(path, "nonexistent", WorkflowMutation{})
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}
