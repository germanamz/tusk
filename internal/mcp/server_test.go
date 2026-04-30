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
	"github.com/mark3labs/mcp-go/mcp"
)

// mustNew calls New and fails the test on error.
func mustNew(test *testing.T, cfg config.MCPConfig) *Server {
	test.Helper()
	server, err := New(
		nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil,
		"test", cfg, nil,
	)

	if err != nil {
		test.Fatalf("New() returned unexpected error: %v", err)
	}

	return server
}

func TestNewServer(test *testing.T) {
	server := mustNew(test, config.MCPConfig{})
	if server == nil {
		test.Fatal("New() returned nil")
	}
	if server.server == nil {
		test.Fatal("New() did not initialize internal MCP server")
	}
}

func TestNewServer_WithConfig(test *testing.T) {
	cfg := config.MCPConfig{
		DisabledTools: []string{"tusk_task_delete"},
	}
	server := mustNew(test, cfg)
	if server == nil {
		test.Fatal("New() returned nil")
	}
	if server.cfg.DisabledTools[0] != "tusk_task_delete" {
		test.Fatal("config not stored on server")
	}
}

func TestToolFiltering_DisabledTool(test *testing.T) {
	cfg := config.MCPConfig{
		DisabledTools: []string{"tusk_task_delete"},
	}
	server := mustNew(test, cfg)

	if server.isToolEnabled("tusk_task_delete", "task") {
		test.Error("tusk_task_delete should be disabled")
	}
	if !server.isToolEnabled("tusk_task_create", "task") {
		test.Error("tusk_task_create should be enabled")
	}
}

func TestToolFiltering_DisabledGroup(test *testing.T) {
	cfg := config.MCPConfig{
		DisabledToolGroups: []string{"task_relations"},
	}
	server := mustNew(test, cfg)

	if server.isToolEnabled("tusk_task_link", "task_relations") {
		test.Error("tusk_task_link should be disabled (group 'task_relations' disabled)")
	}
	if server.isToolEnabled("tusk_task_unlink", "task_relations") {
		test.Error("tusk_task_unlink should be disabled (group 'task_relations' disabled)")
	}
	if !server.isToolEnabled("tusk_task_create", "task") {
		test.Error("tusk_task_create should be enabled (group 'task' not disabled)")
	}
}

func TestResourceFiltering_DisabledResource(test *testing.T) {
	cfg := config.MCPConfig{
		DisabledResources: []string{"tusk://projects/{name}/workflow"},
	}
	server := mustNew(test, cfg)

	if server.isResourceEnabled("tusk://projects/{name}/workflow", "workflow") {
		test.Error("workflow resource should be disabled")
	}
	if !server.isResourceEnabled("tusk://tasks/{short_id}", "task") {
		test.Error("task resource should be enabled")
	}
}

func TestResourceFiltering_DisabledGroup(test *testing.T) {
	cfg := config.MCPConfig{
		DisabledResourceGroups: []string{"workflow"},
	}
	server := mustNew(test, cfg)

	if server.isResourceEnabled("tusk://projects/{name}/workflow", "workflow") {
		test.Error("workflow resource should be disabled (group disabled)")
	}
	if !server.isResourceEnabled("tusk://projects/{name}", "project") {
		test.Error("project resource should be enabled")
	}
}

func TestRegisterTools_FiltersDisabledTools(test *testing.T) {
	full := mustNew(test, config.MCPConfig{})
	filtered := mustNew(test, config.MCPConfig{
		DisabledToolGroups: []string{"task_relations"},
	})

	if len(full.toolGroups) != 33 {
		test.Errorf("full server: expected 33 tools, got %d", len(full.toolGroups))
	}
	if len(filtered.toolGroups) != 31 {
		test.Errorf("filtered server: expected 31 tools (task_relations group disabled), got %d", len(filtered.toolGroups))
	}
}

func TestRegisterResources_FiltersDisabledResources(test *testing.T) {
	full := mustNew(test, config.MCPConfig{})
	filtered := mustNew(test, config.MCPConfig{
		DisabledResourceGroups: []string{"workflow"},
	})

	if len(full.resourceGroups) != 3 {
		test.Errorf("full server: expected 3 resources, got %d", len(full.resourceGroups))
	}
	if len(filtered.resourceGroups) != 2 {
		test.Errorf("filtered server: expected 2 resources (workflow disabled), got %d", len(filtered.resourceGroups))
	}
}

func TestValidation_UnknownEntries(test *testing.T) {
	cfg := config.MCPConfig{
		DisabledTools:          []string{"tusk_nonexistent_tool"},
		DisabledToolGroups:     []string{"nonexistent_group"},
		DisabledResources:      []string{"tusk://nonexistent/resource"},
		DisabledResourceGroups: []string{"nonexistent_res_group"},
	}

	_, err := New(
		nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil,
		"test", cfg, nil,
	)
	if err == nil {
		test.Fatal("expected error for unknown config entries, got nil")
	}

	msg := err.Error()
	for _, want := range []string{
		"tusk_nonexistent_tool",
		"nonexistent_group",
		"tusk://nonexistent/resource",
		"nonexistent_res_group",
	} {
		if !strings.Contains(msg, want) {
			test.Errorf("expected error to mention %q, got: %s", want, msg)
		}
	}
}

func TestValidation_NoErrorForValidEntries(test *testing.T) {
	cfg := config.MCPConfig{
		DisabledToolGroups:     []string{"task_relations"},
		DisabledResourceGroups: []string{"workflow"},
	}

	_, err := New(
		nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil,
		"test", cfg, nil,
	)

	if err != nil {
		test.Errorf("expected no error, got: %v", err)
	}
}

func TestValidation_NoteDisable(test *testing.T) {
	test.Run("disable note tool accepted", func(test *testing.T) {
		_, err := New(
			nil, nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			"test", config.MCPConfig{DisabledTools: []string{"tusk_note_add"}}, nil,
		)

		if err != nil {
			test.Errorf("expected no error disabling tusk_note_add, got: %v", err)
		}
	})

	test.Run("disable note group accepted", func(test *testing.T) {
		_, err := New(
			nil, nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			"test", config.MCPConfig{DisabledToolGroups: []string{"note"}}, nil,
		)

		if err != nil {
			test.Errorf("expected no error disabling note group, got: %v", err)
		}
	})

	test.Run("unknown note_* rejected", func(test *testing.T) {
		_, err := New(
			nil, nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			"test", config.MCPConfig{DisabledTools: []string{"tusk_note_bogus"}}, nil,
		)
		if err == nil {
			test.Fatal("expected error for unknown tusk_note_bogus, got nil")
		}
		if !strings.Contains(err.Error(), "tusk_note_bogus") {
			test.Errorf("expected error to mention tusk_note_bogus, got: %s", err.Error())
		}
	})
}

func TestValidateConfig_BlockedFields_UnknownTool(test *testing.T) {
	_, err := New(
		nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil,
		"test", config.MCPConfig{
			BlockedFields: map[string][]string{"tusk_bogus": {"foo"}},
		}, nil,
	)
	if err == nil {
		test.Fatal("expected error for unknown tool in blocked_fields, got nil")
	}
	if !strings.Contains(err.Error(), "blocked_fields: unknown tool") {
		test.Errorf("expected error to mention 'blocked_fields: unknown tool', got: %s", err.Error())
	}
}

func TestValidateConfig_BlockedFields_UnknownField(test *testing.T) {
	_, err := New(
		nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil,
		"test", config.MCPConfig{
			BlockedFields: map[string][]string{"tusk_task_modify": {"bogus"}},
		}, nil,
	)
	if err == nil {
		test.Fatal("expected error for unknown field in blocked_fields, got nil")
	}
	if !strings.Contains(err.Error(), `has no field "bogus"`) {
		test.Errorf(`expected error to mention 'has no field "bogus"', got: %s`, err.Error())
	}
}

func TestValidateConfig_BlockedFields_DottedField(test *testing.T) {
	_, err := New(
		nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil,
		"test", config.MCPConfig{
			BlockedFields: map[string][]string{"tusk_task_modify": {"uda.env"}},
		}, nil,
	)
	if err == nil {
		test.Fatal("expected error for dotted sub-key in blocked_fields, got nil")
	}
	if !strings.Contains(err.Error(), "dotted sub-keys not yet supported") {
		test.Errorf("expected error to mention 'dotted sub-keys not yet supported', got: %s", err.Error())
	}
}

func TestValidateConfig_BlockedFields_Valid(test *testing.T) {
	_, err := New(
		nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil,
		"test", config.MCPConfig{
			BlockedFields: map[string][]string{"tusk_project_modify": {"workflow"}},
		}, nil,
	)

	if err != nil {
		test.Errorf("expected no error for valid blocked_fields entry, got: %v", err)
	}
}

func TestServer_ReloadConfig_SmokeTest(test *testing.T) {
	dir := test.TempDir()
	configPath := filepath.Join(dir, "tusk.toml")

	seedBytes, readErr := os.ReadFile("../../config/default.toml")

	if readErr != nil {
		test.Fatalf("reading default.toml seed: %v", readErr)
	}

	if err := os.WriteFile(configPath, seedBytes, 0o644); err != nil {
		test.Fatalf("writing seed config: %v", err)
	}

	_, projectRepo, workflowRepo := sqlitetest.NewStore(test)
	urgencyEngine := service.NewUrgencyEngine(service.UrgencyWeights{})

	server, newErr := New(
		nil, nil, nil, nil, nil, nil, nil,
		workflowRepo, projectRepo, urgencyEngine,
		"test", config.MCPConfig{},
		[]config.Option{config.WithExplicitFile(configPath)},
	)

	if newErr != nil {
		test.Fatalf("New: %v", newErr)
	}

	if err := server.ReloadConfigForTest(context.Background()); err != nil {
		test.Fatalf("reloadConfig: %v", err)
	}

	wfs, listErr := workflowRepo.List(context.Background())

	if listErr != nil || len(wfs) == 0 {
		test.Fatalf("post-reload workflows: got %+v err=%v", wfs, listErr)
	}
}

func TestReloadConfig_BlockedFieldsHotSwap(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")

	initial, loadErr := config.LoadFile("../../config/default.toml")

	if loadErr != nil {
		test.Fatalf("reading default.toml seed: %v", loadErr)
	}

	initial.MCP.BlockedFields = nil

	if err := config.WriteConfig(initial, path); err != nil {
		test.Fatalf("writing initial config: %v", err)
	}

	_, projectRepo, workflowRepo := sqlitetest.NewStore(test)
	urgencyEngine := service.NewUrgencyEngine(service.UrgencyWeights{})

	loadOpts := []config.Option{config.WithExplicitFile(path)}

	server, newErr := New(
		nil, nil, nil, nil, nil, nil, nil,
		workflowRepo, projectRepo, urgencyEngine,
		"test", initial.MCP, loadOpts,
	)

	if newErr != nil {
		test.Fatalf("New: %v", newErr)
	}

	req := blockedReq(map[string]any{
		"name":     "backend",
		"version":  float64(1),
		"workflow": "kanban",
	})
	if res := server.checkBlocked("tusk_project_modify", req); res != nil {
		test.Fatalf("pre-reload: expected no block, got %s", res.Content[0].(mcp.TextContent).Text)
	}

	updated, reloadErr := config.LoadFile(path)

	if reloadErr != nil {
		test.Fatalf("re-reading config: %v", reloadErr)
	}

	updated.MCP.BlockedFields = map[string][]string{
		"tusk_project_modify": {"workflow"},
	}

	if err := config.WriteConfig(updated, path); err != nil {
		test.Fatalf("rewriting config: %v", err)
	}

	if err := server.ReloadConfigForTest(context.Background()); err != nil {
		test.Fatalf("reloadConfig: %v", err)
	}

	res := server.checkBlocked("tusk_project_modify", req)
	if res == nil {
		test.Fatal("post-reload: expected block, got nil")
	}
	msg := res.Content[0].(mcp.TextContent).Text
	if !strings.Contains(msg, "mcp.blocked_fields.tusk_project_modify") {
		test.Errorf("block message missing config-key hint: %q", msg)
	}
}
