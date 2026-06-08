package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// rewriteManifest replaces tusk.toml at root with the given body. The
// node-types use TOML table syntax ([node-types.<name>] with properties),
// matching manifest.Load's real schema.
func rewriteManifest(test *testing.T, root, body string) {
	test.Helper()

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(body), 0o644); writeErr != nil {
		test.Fatalf("rewrite tusk.toml: %v", writeErr)
	}
}

// TestReloadTool_HappyPath validates that a successful reload returns
// diff + epoch + reindex report with no validation errors.
func TestReloadTool_HappyPath(test *testing.T) {
	// Real workspace (tusk.toml + seeded node on disk) and a populated index.
	root := setupServerWorkspace(test)
	srv := newServerForRoot(test, root)

	// Modify tusk.toml to add a new node-type "decision".
	rewriteManifest(test, root, `[workspace]
name = "test"

[node-types.decision]
properties = [
    { name = "summary", type = "string", required = true },
]
`)

	// Call tusk_reload (default args).
	request := mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Name:      "tusk_reload",
			Arguments: map[string]any{},
		},
	}

	result, callErr := reloadToolHandler(context.Background(), request, srv)
	if callErr != nil {
		test.Fatalf("reload tool: %v", callErr)
	}

	// Parse response JSON from the tool result's text payload.
	var response map[string]any
	if parseErr := json.Unmarshal([]byte(textOf(result)), &response); parseErr != nil {
		test.Fatalf("parse response: %v", parseErr)
	}

	// Assertions:
	// - manifest_epoch > 0 (was bumped)
	if epoch, ok := response["manifest_epoch"].(float64); !ok || epoch == 0 {
		test.Errorf("expected manifest_epoch > 0, got %v", response["manifest_epoch"])
	}

	// - diff contains added node type "decision"
	if diff, ok := response["diff"].(map[string]any); ok {
		if nodeTypes, ok := diff["node_types"].(map[string]any); ok {
			if added, ok := nodeTypes["added"].([]any); ok {
				found := false
				for _, item := range added {
					if item == "decision" {
						found = true
						break
					}
				}
				if !found {
					test.Errorf("expected 'decision' in added node-types")
				}
			}
		}
	}

	// - reindex.kicked: true (by default)
	if reindex, ok := response["reindex"].(map[string]any); ok {
		if !reindex["kicked"].(bool) {
			test.Errorf("expected reindex.kicked=true")
		}
	}

	// - validation_errors empty
	if validationErrors, ok := response["validation_errors"].([]any); !ok || len(validationErrors) > 0 {
		test.Errorf("expected empty validation_errors, got %v", response["validation_errors"])
	}
}

// TestReloadTool_BlockingError_ParseFailure validates that a TOML parse error
// is blocking: returns validation_errors, no epoch bump, old manifest served.
func TestReloadTool_BlockingError_ParseFailure(test *testing.T) {
	root := setupServerWorkspace(test)
	srv := newServerForRoot(test, root)

	recordedEpoch := srv.seenManifestEpoch.Load()

	// Replace tusk.toml with invalid TOML (unclosed table header).
	rewriteManifest(test, root, "[workspace\nname = \"test\"")

	// Call tusk_reload.
	request := mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Name:      "tusk_reload",
			Arguments: map[string]any{},
		},
	}

	result, callErr := reloadToolHandler(context.Background(), request, srv)
	if callErr != nil {
		test.Fatalf("reload tool: %v", callErr)
	}

	var response map[string]any
	if parseErr := json.Unmarshal([]byte(textOf(result)), &response); parseErr != nil {
		test.Fatalf("parse response: %v", parseErr)
	}

	// Assertions:
	// - validation_errors is non-empty
	if validationErrors, ok := response["validation_errors"].([]any); !ok || len(validationErrors) == 0 {
		test.Errorf("expected non-empty validation_errors on parse failure")
	}

	// - manifest_epoch unchanged
	if epoch, ok := response["manifest_epoch"].(float64); !ok || int64(epoch) != recordedEpoch {
		test.Errorf("expected manifest_epoch unchanged (was %d), got %v", recordedEpoch, response["manifest_epoch"])
	}

	// - reindex.kicked: false
	if reindex, ok := response["reindex"].(map[string]any); ok {
		if reindex["kicked"].(bool) {
			test.Errorf("expected reindex.kicked=false on blocking error")
		}
	}
}

// TestReloadTool_NoReindex validates that no_reindex=true prevents reindex kick.
func TestReloadTool_NoReindex(test *testing.T) {
	root := setupServerWorkspace(test)
	srv := newServerForRoot(test, root)

	rewriteManifest(test, root, `[workspace]
name = "test"

[node-types.decision]
properties = [
    { name = "summary", type = "string", required = true },
]
`)

	// Call tusk_reload with no_reindex=true.
	request := mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Name: "tusk_reload",
			Arguments: map[string]any{
				"no_reindex": true,
			},
		},
	}

	result, callErr := reloadToolHandler(context.Background(), request, srv)
	if callErr != nil {
		test.Fatalf("reload tool: %v", callErr)
	}

	var response map[string]any
	if parseErr := json.Unmarshal([]byte(textOf(result)), &response); parseErr != nil {
		test.Fatalf("parse response: %v", parseErr)
	}

	// Assertion: reindex.kicked: false
	if reindex, ok := response["reindex"].(map[string]any); ok {
		if reindex["kicked"].(bool) {
			test.Errorf("expected reindex.kicked=false when no_reindex=true")
		}
	}
}
