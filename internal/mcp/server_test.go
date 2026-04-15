package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/service"
	"github.com/germanamz/tusk/sqlite/sqlitetest"
)

// mustNew calls New and fails the test on error.
func mustNew(t *testing.T, cfg config.MCPConfig) *Server {
	t.Helper()
	s, err := New(
		nil, nil, nil, nil, nil, nil,
		nil, nil, nil,
		"test", cfg, nil,
	)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}
	return s
}

func TestNewServer(t *testing.T) {
	s := mustNew(t, config.MCPConfig{})
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
	s := mustNew(t, cfg)
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
	s := mustNew(t, cfg)

	if s.isToolEnabled("tusk_task_delete", "task") {
		t.Error("tusk_task_delete should be disabled")
	}
	if !s.isToolEnabled("tusk_task_create", "task") {
		t.Error("tusk_task_create should be enabled")
	}
}

func TestToolFiltering_DisabledGroup(t *testing.T) {
	cfg := config.MCPConfig{
		DisabledToolGroups: []string{"task_relations"},
	}
	s := mustNew(t, cfg)

	if s.isToolEnabled("tusk_task_link", "task_relations") {
		t.Error("tusk_task_link should be disabled (group 'task_relations' disabled)")
	}
	if s.isToolEnabled("tusk_task_unlink", "task_relations") {
		t.Error("tusk_task_unlink should be disabled (group 'task_relations' disabled)")
	}
	if !s.isToolEnabled("tusk_task_create", "task") {
		t.Error("tusk_task_create should be enabled (group 'task' not disabled)")
	}
}

func TestResourceFiltering_DisabledResource(t *testing.T) {
	cfg := config.MCPConfig{
		DisabledResources: []string{"tusk://projects/{name}/workflow"},
	}
	s := mustNew(t, cfg)

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
	s := mustNew(t, cfg)

	if s.isResourceEnabled("tusk://projects/{name}/workflow", "workflow") {
		t.Error("workflow resource should be disabled (group disabled)")
	}
	if !s.isResourceEnabled("tusk://projects/{name}", "project") {
		t.Error("project resource should be enabled")
	}
}

func TestRegisterTools_FiltersDisabledTools(t *testing.T) {
	full := mustNew(t, config.MCPConfig{})
	filtered := mustNew(t, config.MCPConfig{
		DisabledToolGroups: []string{"task_relations"},
	})

	if len(full.toolGroups) != 27 {
		t.Errorf("full server: expected 27 tools, got %d", len(full.toolGroups))
	}
	if len(filtered.toolGroups) != 25 {
		t.Errorf("filtered server: expected 25 tools (task_relations group disabled), got %d", len(filtered.toolGroups))
	}
}

func TestRegisterResources_FiltersDisabledResources(t *testing.T) {
	full := mustNew(t, config.MCPConfig{})
	filtered := mustNew(t, config.MCPConfig{
		DisabledResourceGroups: []string{"workflow"},
	})

	if len(full.resourceGroups) != 3 {
		t.Errorf("full server: expected 3 resources, got %d", len(full.resourceGroups))
	}
	if len(filtered.resourceGroups) != 2 {
		t.Errorf("filtered server: expected 2 resources (workflow disabled), got %d", len(filtered.resourceGroups))
	}
}

func TestValidation_UnknownEntries(t *testing.T) {
	cfg := config.MCPConfig{
		DisabledTools:          []string{"tusk_nonexistent_tool"},
		DisabledToolGroups:     []string{"nonexistent_group"},
		DisabledResources:      []string{"tusk://nonexistent/resource"},
		DisabledResourceGroups: []string{"nonexistent_res_group"},
	}

	_, err := New(
		nil, nil, nil, nil, nil, nil,
		nil, nil, nil,
		"test", cfg, nil,
	)
	if err == nil {
		t.Fatal("expected error for unknown config entries, got nil")
	}

	msg := err.Error()
	for _, want := range []string{
		"tusk_nonexistent_tool",
		"nonexistent_group",
		"tusk://nonexistent/resource",
		"nonexistent_res_group",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected error to mention %q, got: %s", want, msg)
		}
	}
}

func TestValidation_NoErrorForValidEntries(t *testing.T) {
	cfg := config.MCPConfig{
		DisabledToolGroups:     []string{"task_relations"},
		DisabledResourceGroups: []string{"workflow"},
	}

	_, err := New(
		nil, nil, nil, nil, nil, nil,
		nil, nil, nil,
		"test", cfg, nil,
	)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestServer_ReloadConfig_SmokeTest(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "tusk.toml")
	seed, err := os.ReadFile("../../config/default.toml")
	if err != nil {
		t.Fatalf("reading default.toml seed: %v", err)
	}
	if err := os.WriteFile(configPath, seed, 0o644); err != nil {
		t.Fatalf("writing seed config: %v", err)
	}

	_, projectRepo, workflowRepo := sqlitetest.NewStore(t)
	urgencyEngine := service.NewUrgencyEngine(service.UrgencyWeights{})

	srv, err := New(
		nil, nil, nil, nil, nil, nil,
		workflowRepo, projectRepo, urgencyEngine,
		"test", config.MCPConfig{},
		[]config.Option{config.WithExplicitFile(configPath)},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := srv.ReloadConfigForTest(context.Background()); err != nil {
		t.Fatalf("reloadConfig: %v", err)
	}

	wfs, err := workflowRepo.List(context.Background())
	if err != nil || len(wfs) == 0 {
		t.Fatalf("post-reload workflows: got %+v err=%v", wfs, err)
	}
}
