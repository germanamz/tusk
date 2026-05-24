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

	contextErrors, _ := body["context_errors"].([]any)

	if len(contextErrors) == 0 {
		test.Errorf("context_errors empty; want one: %v", body)
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

	missing, _ := body["missing_pinned_ids"].([]any)

	if len(missing) != 1 {
		test.Errorf("missing_pinned_ids len = %d, want 1: %v", len(missing), body)
	}
}
