package mcp

import (
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestToolResultJSON(t *testing.T) {
	data := map[string]string{"key": "value"}
	result, err := toolResultJSON(data)
	if err != nil {
		t.Fatalf("toolResultJSON() error: %v", err)
	}
	if result == nil {
		t.Fatal("toolResultJSON() returned nil")
	}
	if result.IsError {
		t.Fatal("toolResultJSON() returned error result")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}
	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	var parsed map[string]string
	if err := json.Unmarshal([]byte(textContent.Text), &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if parsed["key"] != "value" {
		t.Fatalf("expected key=value, got key=%s", parsed["key"])
	}
}
