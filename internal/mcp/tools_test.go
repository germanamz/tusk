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

// callToolRaw runs the registered handler for `name` against `args` and returns
// the raw CallToolResult without interpreting IsError.
func callToolRaw(test *testing.T, srv *mcp.Server, name string, args map[string]any) *mcpgo.CallToolResult {
	test.Helper()

	request := mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Name:      name,
			Arguments: args,
		},
	}

	result, callErr := srv.HandleToolCall(context.Background(), request)

	if callErr != nil {
		test.Fatalf("HandleToolCall(%s): %v", name, callErr)
	}

	return result
}

// decodeJSONContent unmarshals the first text content item of a CallToolResult.
func decodeJSONContent(test *testing.T, result *mcpgo.CallToolResult) map[string]any {
	test.Helper()

	if len(result.Content) == 0 {
		test.Fatal("result has no content")
	}

	textContent, ok := result.Content[0].(mcpgo.TextContent)

	if !ok {
		test.Fatalf("expected TextContent, got %T", result.Content[0])
	}

	var parsed map[string]any

	if unmarshalErr := json.Unmarshal([]byte(textContent.Text), &parsed); unmarshalErr != nil {
		test.Fatalf("unmarshal: %v\nbody: %s", unmarshalErr, textContent.Text)
	}

	return parsed
}

// newRuntimeWithWorkflow boots a Runtime with a tusk.toml that activates the
// workflow pack on tickets.
func newRuntimeWithWorkflow(test *testing.T) (*mcp.Runtime, *mcp.Server) {
	test.Helper()

	root := test.TempDir()

	manifestBody := []byte(`
[workspace]
name = "test"

[behaviors.workflow.tickets]
applies-to = ["ticket"]
states = [
  { name = "pending", initial = true },
  { name = "active" },
  { name = "completed", terminal = true, done = true },
]
transitions = [
  { from = "pending", to = "active" },
  { from = "active", to = "completed" },
]
`)

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), manifestBody, 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	return rt, mcp.NewServer(rt)
}

func TestTools_NodeModify_StructuredWorkflowRejection(test *testing.T) {
	rt, srv := newRuntimeWithWorkflow(test)
	defer rt.Close()

	// Seed a node with status=pending (initial state, no behaviors on create).
	if _, createErr := rt.NodeService.Create(node.CreateInput{
		RelPath:    "tickets/foo.md",
		Type:       "ticket",
		Properties: map[string]any{"status": "pending"},
	}); createErr != nil {
		test.Fatalf("seed Create: %v", createErr)
	}

	// Attempt illegal transition: pending → completed (not in transition table).
	result := callToolRaw(test, srv, "tusk_node_modify", map[string]any{
		"id":  "tickets/foo",
		"set": map[string]any{"status": "completed"},
	})

	if !result.IsError {
		test.Errorf("expected IsError=true")
	}

	body := decodeJSONContent(test, result)

	if body["code"] != "illegal-transition" {
		test.Errorf("body.code = %v, want illegal-transition", body["code"])
	}

	if body["pack_instance"] != "tickets" {
		test.Errorf("body.pack_instance = %v, want tickets", body["pack_instance"])
	}

	if body["from"] != "pending" || body["to"] != "completed" {
		test.Errorf("body.from/to = %v/%v", body["from"], body["to"])
	}
}

// newRuntimeWithNodeTypes seeds an mcp.Runtime backed by a workspace with a
// node-types declaration on `ticket`. Mirror of Plan 7's newRuntimeWithWorkflow
// helper.
func newRuntimeWithNodeTypes(test *testing.T) (*mcp.Runtime, *mcp.Server) {
	test.Helper()

	root := test.TempDir()

	manifestBody := []byte(`
[workspace]
name = "test"

[node-types.ticket]
properties = [
    { name = "summary",  type = "string", required = true },
    { name = "priority", type = "int" },
]
`)

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), manifestBody, 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	return rt, mcp.NewServer(rt)
}

// mustCreateNodeViaRuntime creates a node via the runtime's NodeService.
func mustCreateNodeViaRuntime(test *testing.T, rt *mcp.Runtime, relPath, nodeType string, props map[string]any) {
	test.Helper()

	if _, createErr := rt.NodeService.Create(node.CreateInput{
		RelPath:    relPath + ".md",
		Type:       nodeType,
		Properties: props,
	}); createErr != nil {
		test.Fatalf("mustCreateNodeViaRuntime %s: %v", relPath, createErr)
	}
}

func TestTools_NodeModify_PropertyTypeMismatchStructuredRejection(test *testing.T) {
	rt, harness := newRuntimeWithNodeTypes(test)
	defer rt.Close()

	mustCreateNodeViaRuntime(test, rt, "tickets/foo", "ticket", map[string]any{"summary": "hi"})

	result := callToolRaw(test, harness, "tusk_node_modify", map[string]any{
		"id":  "tickets/foo",
		"set": map[string]any{"priority": "high"},
	})

	if !result.IsError {
		test.Fatalf("expected IsError=true")
	}

	body := decodeJSONContent(test, result)

	if body["error"] != "node-types-rejection" {
		test.Errorf("body.error = %v, want node-types-rejection", body["error"])
	}

	errors, ok := body["errors"].([]any)

	if !ok || len(errors) == 0 {
		test.Fatalf("body.errors absent; body = %v", body)
	}

	first, _ := errors[0].(map[string]any)

	if first["kind"] != "type-mismatch" || first["property"] != "priority" {
		test.Errorf("errors[0] = %v", first)
	}
}

func TestTools_NodeModify_UndeclaredPropertyWarnsOnSuccess(test *testing.T) {
	rt, harness := newRuntimeWithNodeTypes(test)
	defer rt.Close()

	mustCreateNodeViaRuntime(test, rt, "tickets/foo", "ticket", map[string]any{"summary": "hi"})

	result := callToolRaw(test, harness, "tusk_node_modify", map[string]any{
		"id":  "tickets/foo",
		"set": map[string]any{"assignee": "bob"},
	})

	if result.IsError {
		test.Errorf("expected success result, got error: %v", result)
	}

	body := decodeJSONContent(test, result)

	warnings, ok := body["warnings"].([]any)

	if !ok || len(warnings) == 0 {
		test.Fatalf("warnings absent; body = %v", body)
	}

	first, _ := warnings[0].(map[string]any)

	if first["kind"] != "property-drift" || first["property"] != "assignee" {
		test.Errorf("warnings[0] = %v", first)
	}
}

func TestTools_NodeModify_RecoveryWarnsOnSuccess(test *testing.T) {
	rt, srv := newRuntimeWithWorkflow(test)
	defer rt.Close()

	// Seed node with off-schema status by writing directly to disk and indexing,
	// bypassing the behavior engine (which would reject "blocked").
	if mkErr := os.MkdirAll(filepath.Join(rt.Root, "tickets"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	nodeBody := []byte("---\ntype: ticket\nstatus: blocked\n---\n\nbody\n")

	if writeErr := os.WriteFile(filepath.Join(rt.Root, "tickets/foo.md"), nodeBody, 0o644); writeErr != nil {
		test.Fatalf("write node: %v", writeErr)
	}

	// Index it via reindex so the repo has the row.
	if _, callErr := callTool(test, srv, "tusk_reindex", map[string]any{"no_embed": true}); callErr != nil {
		test.Fatalf("reindex: %v", callErr)
	}

	// Modify to declared state; "blocked" is an orphan, so recovery fires.
	result := callToolRaw(test, srv, "tusk_node_modify", map[string]any{
		"id":  "tickets/foo",
		"set": map[string]any{"status": "active"},
	})

	if result.IsError {
		body := decodeJSONContent(test, result)
		test.Fatalf("expected success result for recovery, got error: %v", body)
	}

	body := decodeJSONContent(test, result)

	warnings, ok := body["warnings"].([]any)

	if !ok || len(warnings) == 0 {
		test.Fatalf("warnings absent; body = %v", body)
	}

	first, ok := warnings[0].(map[string]any)

	if !ok {
		test.Fatalf("warnings[0] is not an object: %T", warnings[0])
	}

	if first["kind"] != "workflow-recovered" {
		test.Errorf("warnings[0].kind = %v, want workflow-recovered", first["kind"])
	}

	if first["from"] != "blocked" || first["to"] != "active" {
		test.Errorf("warnings[0] from/to = %v/%v", first["from"], first["to"])
	}
}
