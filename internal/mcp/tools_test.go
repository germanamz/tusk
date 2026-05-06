package mcp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/mcp"
	"github.com/germanamz/tusk/internal/node"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// callTool is the harness every tool test uses. It runs the registered handler
// for `name` against `args` and returns the parsed JSON payload from the
// success result, or an error if the tool returned an MCP error.
func callTool(test *testing.T, srv *mcp.Server, name string, args map[string]any) (map[string]any, error) {
	test.Helper()

	request := mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Name:      name,
			Arguments: args,
		},
	}

	result, callErr := srv.HandleToolCall(context.Background(), request)

	if callErr != nil {
		return nil, callErr
	}

	if result.IsError {
		return nil, fmtError(result)
	}

	if len(result.Content) == 0 {
		return map[string]any{}, nil
	}

	textContent, ok := result.Content[0].(mcpgo.TextContent)

	if !ok {
		test.Fatalf("expected TextContent, got %T", result.Content[0])
	}

	var parsed map[string]any

	if unmarshalErr := json.Unmarshal([]byte(textContent.Text), &parsed); unmarshalErr != nil {
		test.Fatalf("unmarshal: %v\nbody: %s", unmarshalErr, textContent.Text)
	}

	return parsed, nil
}

func fmtError(result *mcpgo.CallToolResult) error {
	if len(result.Content) == 0 {
		return errMCP("(empty)")
	}

	if textContent, ok := result.Content[0].(mcpgo.TextContent); ok {
		return errMCP(textContent.Text)
	}

	return errMCP("(non-text error)")
}

type mcpError string

func (err mcpError) Error() string { return string(err) }

func errMCP(message string) error { return mcpError(message) }

func bootRuntime(test *testing.T) *mcp.Runtime {
	test.Helper()

	root := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(`[workspace]
name = "x"
`), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	return rt
}

func TestTool_Status(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	rt.Nodes.Upsert(index.NodeRow{ID: "tickets/a", Type: "ticket", Path: "tickets/a.md", PropertiesJSON: "{}", LastChecksum: "x"})
	rt.Nodes.Upsert(index.NodeRow{ID: "notes/b", Type: "note", Path: "notes/b.md", PropertiesJSON: "{}", LastChecksum: "x"})

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_status", map[string]any{})

	if callErr != nil {
		test.Fatalf("tusk_status: %v", callErr)
	}

	counts, _ := body["nodes_by_type"].(map[string]any)

	if counts["ticket"].(float64) != 1 || counts["note"].(float64) != 1 {
		test.Errorf("counts = %v", counts)
	}
}

func TestTool_NodeGet(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	if _, createErr := rt.NodeService.Create(node.CreateInput{
		RelPath: "notes/hi.md",
		Type:    "note",
		Title:   "Hi there",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_node_get", map[string]any{"id": "notes/hi"})

	if callErr != nil {
		test.Fatalf("tusk_node_get: %v", callErr)
	}

	if body["id"] != "notes/hi" {
		test.Errorf("id = %v, want notes/hi", body["id"])
	}

	if body["type"] != "note" {
		test.Errorf("type = %v, want note", body["type"])
	}

	if body["title"] != "Hi there" {
		test.Errorf("title = %v, want 'Hi there'", body["title"])
	}
}

func TestTool_NodeList(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	for _, id := range []string{"notes/a", "notes/b"} {
		rt.Nodes.Upsert(index.NodeRow{ID: id, Type: "note", Path: id + ".md", PropertiesJSON: "{}", LastChecksum: "x"})
	}

	rt.Nodes.Upsert(index.NodeRow{ID: "tickets/x", Type: "ticket", Path: "tickets/x.md", PropertiesJSON: "{}", LastChecksum: "x"})

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_node_list", map[string]any{"type": "note"})

	if callErr != nil {
		test.Fatalf("tusk_node_list: %v", callErr)
	}

	results, _ := body["results"].([]any)

	if len(results) != 2 {
		test.Errorf("len(results) = %d, want 2", len(results))
	}
}

func TestTool_EdgeList(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	rt.Edges.UpsertAll("tickets/a", "tickets/a.md", []index.EdgeRow{
		{Type: "blocks", SourceID: "tickets/a", TargetID: "tickets/b", Ordinal: 0, SourcePath: "tickets/a.md"},
	})

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_edge_list", map[string]any{"from": "tickets/a"})

	if callErr != nil {
		test.Fatalf("tusk_edge_list: %v", callErr)
	}

	results, _ := body["results"].([]any)

	if len(results) != 1 {
		test.Fatalf("len(results) = %d, want 1", len(results))
	}

	first := results[0].(map[string]any)

	if first["type"] != "blocks" || first["target_id"] != "tickets/b" {
		test.Errorf("first = %v", first)
	}
}

func TestTool_Query(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	rt.Nodes.Upsert(index.NodeRow{ID: "tickets/a", Type: "ticket", Path: "tickets/a.md", Title: "Auth bug", PropertiesJSON: "{}", LastChecksum: "x"})
	rt.Nodes.Upsert(index.NodeRow{ID: "notes/x", Type: "note", Path: "notes/x.md", Title: "X", PropertiesJSON: "{}", LastChecksum: "x"})

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_query", map[string]any{"filter": "type=ticket"})

	if callErr != nil {
		test.Fatalf("tusk_query: %v", callErr)
	}

	results, _ := body["results"].([]any)

	if len(results) != 1 {
		test.Fatalf("len(results) = %d, want 1", len(results))
	}
}

func TestTool_Doctor_CleanReport(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_doctor", map[string]any{})

	if callErr != nil {
		test.Fatalf("tusk_doctor: %v", callErr)
	}

	issues, _ := body["issues"].([]any)

	if len(issues) != 0 {
		test.Errorf("expected 0 issues, got %d", len(issues))
	}
}

func TestTool_NodeCreate(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_node_create", map[string]any{
		"path":  "notes/hello.md",
		"type":  "note",
		"title": "Hello",
		"body":  "World",
	})

	if callErr != nil {
		test.Fatalf("tusk_node_create: %v", callErr)
	}

	if body["id"] != "notes/hello" {
		test.Errorf("id = %v", body["id"])
	}

	row, getErr := rt.Nodes.Get("notes/hello")

	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if row.Title != "Hello" {
		test.Errorf("title = %q", row.Title)
	}
}

func TestTool_NodeModify(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	if _, createErr := rt.NodeService.Create(node.CreateInput{
		RelPath: "notes/x.md",
		Type:    "note",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_node_modify", map[string]any{
		"id":  "notes/x",
		"set": map[string]any{"priority": float64(5)},
	})

	if callErr != nil {
		test.Fatalf("tusk_node_modify: %v", callErr)
	}

	if body["id"] != "notes/x" {
		test.Errorf("id = %v", body["id"])
	}
}

func TestTool_NodeMove(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	if _, createErr := rt.NodeService.Create(node.CreateInput{
		RelPath: "notes/old.md",
		Type:    "note",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_node_move", map[string]any{
		"id":       "notes/old",
		"new_path": "notes/new.md",
	})

	if callErr != nil {
		test.Fatalf("tusk_node_move: %v", callErr)
	}

	if body["new_id"] != "notes/new" {
		test.Errorf("new_id = %v", body["new_id"])
	}
}

func TestTool_NodeDelete(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	if _, createErr := rt.NodeService.Create(node.CreateInput{
		RelPath: "notes/del.md",
		Type:    "note",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	srv := mcp.NewServer(rt)

	if _, callErr := callTool(test, srv, "tusk_node_delete", map[string]any{"id": "notes/del"}); callErr != nil {
		test.Fatalf("tusk_node_delete: %v", callErr)
	}

	if _, getErr := rt.Nodes.Get("notes/del"); getErr == nil {
		test.Errorf("expected Get error after delete")
	}
}

func bootRuntimeWithEdgeTypes(test *testing.T) *mcp.Runtime {
	test.Helper()

	root := test.TempDir()

	manifest := `[workspace]
name = "x"

[edge-types.blocks]
description = "blocks another ticket"
from = ["*"]
to   = ["*"]
cardinality = "many-to-many"
acyclic = true
`

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifest), 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	return rt
}

func TestTool_EdgeAddRemove(test *testing.T) {
	rt := bootRuntimeWithEdgeTypes(test)
	defer rt.Close()

	rt.Nodes.Upsert(index.NodeRow{ID: "tickets/a", Type: "ticket", Path: "tickets/a.md", PropertiesJSON: "{}", LastChecksum: "x"})
	rt.Nodes.Upsert(index.NodeRow{ID: "tickets/b", Type: "ticket", Path: "tickets/b.md", PropertiesJSON: "{}", LastChecksum: "x"})

	srv := mcp.NewServer(rt)

	if _, callErr := callTool(test, srv, "tusk_edge_add", map[string]any{
		"type":      "blocks",
		"source_id": "tickets/a",
		"target_id": "tickets/b",
	}); callErr != nil {
		test.Fatalf("tusk_edge_add: %v", callErr)
	}

	rows, _ := rt.Edges.ListBySource("tickets/a")

	if len(rows) != 1 {
		test.Fatalf("len(rows) = %d after add, want 1", len(rows))
	}

	if _, callErr := callTool(test, srv, "tusk_edge_remove", map[string]any{
		"type":      "blocks",
		"source_id": "tickets/a",
		"target_id": "tickets/b",
	}); callErr != nil {
		test.Fatalf("tusk_edge_remove: %v", callErr)
	}

	rows, _ = rt.Edges.ListBySource("tickets/a")

	if len(rows) != 0 {
		test.Errorf("expected 0 rows after remove, got %d", len(rows))
	}
}

func TestTool_Reindex(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	if writeErr := os.WriteFile(filepath.Join(rt.Root, "notes/x.md"),
		[]byte("---\ntype: note\ntitle: x\n---\n\nbody\n"), 0o644); writeErr != nil {
		_ = os.MkdirAll(filepath.Join(rt.Root, "notes"), 0o755)
		os.WriteFile(filepath.Join(rt.Root, "notes/x.md"), []byte("---\ntype: note\ntitle: x\n---\n\nbody\n"), 0o644)
	}

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_reindex", map[string]any{})

	if callErr != nil {
		test.Fatalf("tusk_reindex: %v", callErr)
	}

	if body["indexed"].(float64) < 1 {
		test.Errorf("expected indexed >= 1, got %v", body["indexed"])
	}
}
