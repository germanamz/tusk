package mcp

import (
	"testing"

	"github.com/germanamz/tusk/internal/config"
)

func TestNewServer(t *testing.T) {
	s := New(nil, nil, nil, nil, nil, "test", config.MCPConfig{})
	if s == nil {
		t.Fatal("New() returned nil")
	}
	if s.server == nil {
		t.Fatal("New() did not initialize internal MCP server")
	}
}

func TestNewServer_WithConfig(t *testing.T) {
	cfg := config.MCPConfig{
		DisabledTools: []string{"tusk_task_delete"},
	}
	s := New(nil, nil, nil, nil, nil, "test", cfg)
	if s == nil {
		t.Fatal("New() returned nil")
	}
	if s.cfg.DisabledTools[0] != "tusk_task_delete" {
		t.Fatal("config not stored on server")
	}
}

func TestToolFiltering_DisabledTool(t *testing.T) {
	cfg := config.MCPConfig{
		DisabledTools: []string{"tusk_task_delete"},
	}
	s := New(nil, nil, nil, nil, nil, "test", cfg)

	if s.isToolEnabled("tusk_task_delete", "task") {
		t.Error("tusk_task_delete should be disabled")
	}
	if !s.isToolEnabled("tusk_task_create", "task") {
		t.Error("tusk_task_create should be enabled")
	}
}

func TestToolFiltering_DisabledGroup(t *testing.T) {
	cfg := config.MCPConfig{
		DisabledToolGroups: []string{"relation"},
	}
	s := New(nil, nil, nil, nil, nil, "test", cfg)

	if s.isToolEnabled("tusk_relation_add", "relation") {
		t.Error("tusk_relation_add should be disabled (group 'relation' disabled)")
	}
	if s.isToolEnabled("tusk_relation_remove", "relation") {
		t.Error("tusk_relation_remove should be disabled (group 'relation' disabled)")
	}
	if !s.isToolEnabled("tusk_task_create", "task") {
		t.Error("tusk_task_create should be enabled (group 'task' not disabled)")
	}
}

func TestResourceFiltering_DisabledResource(t *testing.T) {
	cfg := config.MCPConfig{
		DisabledResources: []string{"tusk://projects/{name}/workflow"},
	}
	s := New(nil, nil, nil, nil, nil, "test", cfg)

	if s.isResourceEnabled("tusk://projects/{name}/workflow", "workflow") {
		t.Error("workflow resource should be disabled")
	}
	if !s.isResourceEnabled("tusk://tasks/{short_id}", "task") {
		t.Error("task resource should be enabled")
	}
}

func TestResourceFiltering_DisabledGroup(t *testing.T) {
	cfg := config.MCPConfig{
		DisabledResourceGroups: []string{"workflow"},
	}
	s := New(nil, nil, nil, nil, nil, "test", cfg)

	if s.isResourceEnabled("tusk://projects/{name}/workflow", "workflow") {
		t.Error("workflow resource should be disabled (group disabled)")
	}
	if !s.isResourceEnabled("tusk://projects/{name}", "project") {
		t.Error("project resource should be enabled")
	}
}

func TestRegisterTools_FiltersDisabledTools(t *testing.T) {
	full := New(nil, nil, nil, nil, nil, "test", config.MCPConfig{})
	filtered := New(nil, nil, nil, nil, nil, "test", config.MCPConfig{
		DisabledToolGroups: []string{"relation"},
	})

	if len(full.toolGroups) != 13 {
		t.Errorf("full server: expected 13 tools, got %d", len(full.toolGroups))
	}
	if len(filtered.toolGroups) != 11 {
		t.Errorf("filtered server: expected 11 tools (relation group disabled), got %d", len(filtered.toolGroups))
	}
}

func TestRegisterResources_FiltersDisabledResources(t *testing.T) {
	full := New(nil, nil, nil, nil, nil, "test", config.MCPConfig{})
	filtered := New(nil, nil, nil, nil, nil, "test", config.MCPConfig{
		DisabledResourceGroups: []string{"workflow"},
	})

	if len(full.resourceGroups) != 3 {
		t.Errorf("full server: expected 3 resources, got %d", len(full.resourceGroups))
	}
	if len(filtered.resourceGroups) != 2 {
		t.Errorf("filtered server: expected 2 resources (workflow disabled), got %d", len(filtered.resourceGroups))
	}
}
