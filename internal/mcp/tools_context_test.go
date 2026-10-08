package mcp_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/mcp"
)

func TestTool_Context_NoContextBlock(test *testing.T) {
	rt := bootRuntimeWithAlias(test, "")
	defer rt.Close()

	srv := mcp.NewServer(rt)
	body, callErr := callTool(test, srv, "tusk_context", map[string]any{})

	if callErr != nil {
		test.Fatalf("tusk_context: %v", callErr)
	}

	if len(body) != 0 {
		test.Errorf("expected empty envelope when no [context] declared; got %v", body)
	}
}

func TestTool_Context_PinnedAndInclude(test *testing.T) {
	rt := bootRuntimeWithAlias(test, `[alias.snap]
command = "status"

[context]
pinned  = ["notes/alpha"]
include = ["snap"]
`)
	defer rt.Close()

	if upsertErr := rt.Nodes.Upsert(index.NodeRow{
		ID:             "notes/alpha",
		Type:           "note",
		Path:           "notes/alpha.md",
		Title:          "Alpha",
		PropertiesJSON: "{}",
		LastChecksum:   "x",
	}); upsertErr != nil {
		test.Fatalf("Upsert: %v", upsertErr)
	}

	srv := mcp.NewServer(rt)
	body, callErr := callTool(test, srv, "tusk_context", map[string]any{})

	if callErr != nil {
		test.Fatalf("tusk_context: %v", callErr)
	}

	pinned, _ := body["pinned"].([]any)

	if len(pinned) != 1 {
		test.Fatalf("pinned len = %d, want 1: %v", len(pinned), body["pinned"])
	}

	aliasEnv, _ := body["aliases"].(map[string]any)

	if _, ok := aliasEnv["snap"]; !ok {
		test.Errorf("aliases.snap missing: %v", aliasEnv)
	}
}

func TestTool_Context_InlineRecent(test *testing.T) {
	rt := bootRuntimeWithAlias(test, `[context]

[context.recent]
command = "node list"
args.filter = "type=note"
`)
	defer rt.Close()

	if upsertErr := rt.Nodes.Upsert(index.NodeRow{
		ID: "notes/alpha", Type: "note", Path: "notes/alpha.md", PropertiesJSON: "{}", LastChecksum: "x",
	}); upsertErr != nil {
		test.Fatalf("Upsert: %v", upsertErr)
	}

	srv := mcp.NewServer(rt)
	body, callErr := callTool(test, srv, "tusk_context", map[string]any{})

	if callErr != nil {
		test.Fatalf("tusk_context: %v", callErr)
	}

	recent, _ := body["recent"].([]any)

	if len(recent) == 0 {
		test.Fatalf("recent empty; want one row: %v", body)
	}
}

func TestTool_Doctor_SurfacesContextErrors(test *testing.T) {
	rt := bootRuntimeWithAlias(test, `[context]
recent = "unknown-alias"
`)
	defer rt.Close()

	srv := mcp.NewServer(rt)
	body, callErr := callTool(test, srv, "tusk_doctor", map[string]any{})

	if callErr != nil {
		test.Fatalf("tusk_doctor: %v", callErr)
	}

	contextErrors := doctorIssuesOfKind(test, body, "context-invalid")

	if len(contextErrors) == 0 || contextErrors[0]["severity"] != "error" {
		test.Errorf("context-invalid issues = %v, want one error: %v", contextErrors, body)
	}

	if errorCount, _ := body["error_count"].(float64); errorCount < 1 {
		test.Errorf("error_count = %v, want >= 1", body["error_count"])
	}
}

func TestTool_Doctor_SurfacesMissingPinned(test *testing.T) {
	rt := bootRuntimeWithAlias(test, `[context]
pinned = ["notes/ghost"]
`)
	defer rt.Close()

	srv := mcp.NewServer(rt)
	body, callErr := callTool(test, srv, "tusk_doctor", map[string]any{})

	if callErr != nil {
		test.Fatalf("tusk_doctor: %v", callErr)
	}

	missing := doctorIssuesOfKind(test, body, "context-pinned-missing")

	if len(missing) != 1 || missing[0]["node_id"] != "notes/ghost" {
		test.Errorf("context-pinned-missing issues = %v, want one for notes/ghost: %v", missing, body)
	}
}

// doctorIssuesOfKind returns the tusk_doctor issues (from a decoded response
// or a doctor alias result) whose kind matches.
func doctorIssuesOfKind(test *testing.T, body map[string]any, kind string) []map[string]any {
	test.Helper()

	issues, ok := body["issues"].([]any)

	if !ok {
		test.Fatalf("issues missing or wrong type: %T %v", body["issues"], body)
	}

	var matched []map[string]any

	for _, raw := range issues {
		issue, _ := raw.(map[string]any)

		if issue["kind"] == kind {
			matched = append(matched, issue)
		}
	}

	return matched
}
