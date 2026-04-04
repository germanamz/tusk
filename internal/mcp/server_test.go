package mcp

import (
	"testing"
)

func TestNewServer(t *testing.T) {
	// New should not panic with nil services (used for testing tool registration).
	s := New(nil, nil, nil, nil, "test")
	if s == nil {
		t.Fatal("New() returned nil")
	}
	if s.server == nil {
		t.Fatal("New() did not initialize internal MCP server")
	}
}
