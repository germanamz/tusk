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
