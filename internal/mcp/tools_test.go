package mcp

import (
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestToolResultJSON(test *testing.T) {
	data := map[string]string{"key": "value"}
	result, err := toolResultJSON(data)

	if err != nil {
		test.Fatalf("toolResultJSON() error: %v", err)
	}

	if result == nil {
		test.Fatal("toolResultJSON() returned nil")
	}
	if result.IsError {
		test.Fatal("toolResultJSON() returned error result")
	}
	if len(result.Content) != 1 {
		test.Fatalf("expected 1 content block, got %d", len(result.Content))
	}
	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		test.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	var parsed map[string]string
	if err := json.Unmarshal([]byte(textContent.Text), &parsed); err != nil {
		test.Fatalf("failed to parse JSON: %v", err)
	}
	if parsed["key"] != "value" {
		test.Fatalf("expected key=value, got key=%s", parsed["key"])
	}
}
