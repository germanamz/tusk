package mcp_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// TestTool_NodeListStableOrder documents the API contract: tusk_node_list
// returns rows sorted by id ASC unless the caller passes a sort. Previously
// enforced by NodeRepo.List; preserved through the query.ListRun refactor
// via a default Sort = "+id" in the MCP handler.
func TestTool_NodeListStableOrder(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	// Insert out of alphabetical order to ensure the test actually exercises
	// the sort rather than relying on insertion order.
	for _, id := range []string{"notes/c", "notes/a", "notes/b"} {
		rt.Nodes.Upsert(index.NodeRow{ID: id, Type: "note", Path: id + ".md", PropertiesJSON: "{}", LastChecksum: "x"})
	}

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_node_list", map[string]any{"type": "note"})

	if callErr != nil {
		test.Fatalf("tusk_node_list: %v", callErr)
	}

	results, _ := body["results"].([]any)

	if len(results) != 3 {
		test.Fatalf("len(results) = %d, want 3", len(results))
	}

	wantIDs := []string{"notes/a", "notes/b", "notes/c"}

	for idx, want := range wantIDs {
		got, _ := results[idx].(map[string]any)

		if got["id"] != want {
			test.Errorf("results[%d].id = %v, want %q (full order: %v)", idx, got["id"], want, results)
		}
	}
}

func TestTool_EdgeList(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	// Seed the source node so the FK added by the P2 migration on
	// edges.source_id is satisfied.
	rt.Nodes.Upsert(index.NodeRow{ID: "tickets/a", Type: "ticket", Path: "tickets/a.md", Title: "A", PropertiesJSON: "{}", LastChecksum: "x"})

	rt.Edges.UpsertAll("tickets/a", "tickets/a.md", []index.EdgeRow{
		{Type: "blocks", SourceID: "tickets/a", TargetID: "tickets/b", SourcePath: "tickets/a.md"},
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

// TestTool_Query_GraphExpansionArgsAccepted confirms the new Phase 3
// arguments are accepted without changing behaviour. The query plumbing
// stays inert until Task 2 wires the walker in.
func TestTool_Query_GraphExpansionArgsAccepted(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	rt.Nodes.Upsert(index.NodeRow{ID: "tickets/a", Type: "ticket", Path: "tickets/a.md", Title: "T", PropertiesJSON: "{}", LastChecksum: "x"})

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_query", map[string]any{
		"filter":           "type=ticket",
		"graph_expand":     true,
		"hops":             2,
		"graph_weight":     0.3,
		"graph_edge_types": []any{"references", "parent"},
		"explain":          true,
	})

	if callErr != nil {
		test.Fatalf("tusk_query: %v", callErr)
	}

	results, _ := body["results"].([]any)

	if len(results) != 1 {
		test.Fatalf("len(results) = %d, want 1", len(results))
	}
}

// TestTool_Query_RejectsInvalidHops asserts hops outside {1,2} surfaces a
// tool error, mirroring the CLI behaviour.
func TestTool_Query_RejectsInvalidHops(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_query", map[string]any{
		"filter": "type=ticket",
		"hops":   5,
	})

	if callErr == nil {
		test.Fatalf("expected error for hops=5, got body=%v", body)
	}
}

// TestTool_Query_RejectsInvalidGraphWeight asserts weights outside [0,1]
// surface a tool error.
func TestTool_Query_RejectsInvalidGraphWeight(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_query", map[string]any{
		"filter":       "type=ticket",
		"graph_weight": 1.5,
	})

	if callErr == nil {
		test.Fatalf("expected error for graph_weight=1.5, got body=%v", body)
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

func TestEdgeAddMCP_WritesFrontmatter(test *testing.T) {
	root := test.TempDir()

	manifest := `[workspace]
name = "x"

[node-types.ticket]
properties = [{ name = "priority", type = "enum", values = ["low", "high"] }]

[edge-types.blocks]
from        = ["ticket"]
to          = ["ticket"]
cardinality = "many-to-many"
`

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifest), 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer rt.Close()

	srv := mcp.NewServer(rt)

	for _, spec := range []struct {
		path  string
		title string
	}{
		{path: "tickets/a.md", title: "A"},
		{path: "tickets/b.md", title: "B"},
	} {
		if _, callErr := callTool(test, srv, "tusk_node_create", map[string]any{
			"path":  spec.path,
			"type":  "ticket",
			"title": spec.title,
		}); callErr != nil {
			test.Fatalf("tusk_node_create %s: %v", spec.path, callErr)
		}
	}

	if _, callErr := callTool(test, srv, "tusk_edge_add", map[string]any{
		"type":      "blocks",
		"source_id": "tickets/a",
		"target_id": "tickets/b",
	}); callErr != nil {
		test.Fatalf("tusk_edge_add: %v", callErr)
	}

	body, readErr := os.ReadFile(filepath.Join(rt.Root, "tickets/a.md"))

	if readErr != nil {
		test.Fatalf("read tickets/a.md: %v", readErr)
	}

	if !strings.Contains(string(body), "blocks: tickets/b") {
		test.Errorf("expected blocks: tickets/b in frontmatter, got:\n%s", body)
	}
}

func TestEdgeRemoveMCP_RemovesFromFrontmatter(test *testing.T) {
	root := test.TempDir()

	manifest := `[workspace]
name = "x"

[node-types.ticket]
properties = [{ name = "priority", type = "enum", values = ["low", "high"] }]

[edge-types.blocks]
from        = ["ticket"]
to          = ["ticket"]
cardinality = "many-to-many"
`

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifest), 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer rt.Close()

	srv := mcp.NewServer(rt)

	for _, spec := range []struct {
		path  string
		title string
	}{
		{path: "tickets/a.md", title: "A"},
		{path: "tickets/b.md", title: "B"},
	} {
		if _, callErr := callTool(test, srv, "tusk_node_create", map[string]any{
			"path":  spec.path,
			"type":  "ticket",
			"title": spec.title,
		}); callErr != nil {
			test.Fatalf("tusk_node_create %s: %v", spec.path, callErr)
		}
	}

	if _, callErr := callTool(test, srv, "tusk_edge_add", map[string]any{
		"type":      "blocks",
		"source_id": "tickets/a",
		"target_id": "tickets/b",
	}); callErr != nil {
		test.Fatalf("seed tusk_edge_add: %v", callErr)
	}

	if _, callErr := callTool(test, srv, "tusk_edge_remove", map[string]any{
		"type":      "blocks",
		"source_id": "tickets/a",
		"target_id": "tickets/b",
	}); callErr != nil {
		test.Fatalf("tusk_edge_remove: %v", callErr)
	}

	body, readErr := os.ReadFile(filepath.Join(rt.Root, "tickets/a.md"))

	if readErr != nil {
		test.Fatalf("read tickets/a.md: %v", readErr)
	}

	if strings.Contains(string(body), "blocks") {
		test.Errorf("blocks key should have been removed from frontmatter, got:\n%s", body)
	}

	rows, listErr := rt.Edges.ListBySource("tickets/a")

	if listErr != nil {
		test.Fatalf("ListBySource: %v", listErr)
	}

	for _, row := range rows {
		if row.Type == "blocks" && row.TargetID == "tickets/b" {
			test.Errorf("expected blocks edge gone from index; found: %+v", row)
		}
	}
}

func TestEdgeRemoveMCP_SweepsLegacyMCPRow(test *testing.T) {
	root := test.TempDir()

	manifest := `[workspace]
name = "x"

[node-types.ticket]
properties = [{ name = "priority", type = "enum", values = ["low", "high"] }]

[edge-types.blocks]
from        = ["ticket"]
to          = ["ticket"]
cardinality = "many-to-many"
`

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifest), 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer rt.Close()

	srv := mcp.NewServer(rt)

	for _, spec := range []struct {
		path  string
		title string
	}{
		{path: "tickets/a.md", title: "A"},
		{path: "tickets/b.md", title: "B"},
	} {
		if _, callErr := callTool(test, srv, "tusk_node_create", map[string]any{
			"path":  spec.path,
			"type":  "ticket",
			"title": spec.title,
		}); callErr != nil {
			test.Fatalf("tusk_node_create %s: %v", spec.path, callErr)
		}
	}

	// Seed a legacy __mcp__ row directly (bypassing the MCP tool). This simulates
	// a row left over from a pre-frontmatter "tusk_edge_add" call.
	if upsertErr := rt.Edges.UpsertAll("tickets/a", index.MCPSourcePath, []index.EdgeRow{
		{Type: "blocks", SourceID: "tickets/a", TargetID: "tickets/b", SourcePath: index.MCPSourcePath},
	}); upsertErr != nil {
		test.Fatalf("seed __mcp__: %v", upsertErr)
	}

	// Remove via the MCP tool. The sweep should clear the legacy row.
	if _, callErr := callTool(test, srv, "tusk_edge_remove", map[string]any{
		"type":      "blocks",
		"source_id": "tickets/a",
		"target_id": "tickets/b",
	}); callErr != nil {
		test.Fatalf("tusk_edge_remove: %v", callErr)
	}

	rows, listErr := rt.Edges.ListBySource("tickets/a")

	if listErr != nil {
		test.Fatalf("list: %v", listErr)
	}

	for _, row := range rows {
		if row.SourcePath == index.MCPSourcePath {
			test.Errorf("legacy __mcp__ row should have been swept; still present: %+v", row)
		}
	}
}

func TestDoctorMCP_AutoMigratesLegacyRows(test *testing.T) {
	root := test.TempDir()

	manifest := `[workspace]
name = "x"

[node-types.ticket]
properties = [{ name = "priority", type = "enum", values = ["low", "high"] }]

[edge-types.blocks]
from        = ["ticket"]
to          = ["ticket"]
cardinality = "many-to-many"
`

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifest), 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer rt.Close()

	srv := mcp.NewServer(rt)

	for _, spec := range []struct {
		path  string
		title string
	}{
		{path: "tickets/a.md", title: "A"},
		{path: "tickets/b.md", title: "B"},
	} {
		if _, callErr := callTool(test, srv, "tusk_node_create", map[string]any{
			"path":  spec.path,
			"type":  "ticket",
			"title": spec.title,
		}); callErr != nil {
			test.Fatalf("tusk_node_create %s: %v", spec.path, callErr)
		}
	}

	// Seed a legacy __cli__ row directly (bypassing the MCP tool) to simulate
	// a row left over from a pre-frontmatter `tusk edge add` invocation.
	if upsertErr := rt.Edges.UpsertAll("tickets/a", index.CLISourcePath, []index.EdgeRow{
		{Type: "blocks", SourceID: "tickets/a", TargetID: "tickets/b", SourcePath: index.CLISourcePath},
	}); upsertErr != nil {
		test.Fatalf("seed __cli__: %v", upsertErr)
	}

	body, callErr := callTool(test, srv, "tusk_doctor", map[string]any{})

	if callErr != nil {
		test.Fatalf("tusk_doctor: %v", callErr)
	}

	migratedCount, _ := body["migrated_count"].(float64)

	if migratedCount < 1 {
		test.Errorf("expected migrated_count >= 1, got %v (body=%v)", body["migrated_count"], body)
	}

	fileBody, readErr := os.ReadFile(filepath.Join(rt.Root, "tickets/a.md"))

	if readErr != nil {
		test.Fatalf("read tickets/a.md: %v", readErr)
	}

	if !strings.Contains(string(fileBody), "blocks: tickets/b") {
		test.Errorf("doctor (MCP) should have migrated the legacy CLI edge into frontmatter, got:\n%s", fileBody)
	}

	rows, listErr := rt.Edges.ListBySource("tickets/a")

	if listErr != nil {
		test.Fatalf("list: %v", listErr)
	}

	for _, row := range rows {
		if row.SourcePath == index.CLISourcePath {
			test.Errorf("expected legacy __cli__ row to be removed; still present: %+v", row)
		}
	}
}

func TestDoctorMCP_NoMigrateReportsLegacyRowsAsDrift(test *testing.T) {
	root := test.TempDir()

	manifest := `[workspace]
name = "x"

[node-types.ticket]
properties = [{ name = "priority", type = "enum", values = ["low", "high"] }]

[edge-types.blocks]
from        = ["ticket"]
to          = ["ticket"]
cardinality = "many-to-many"
`

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifest), 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer rt.Close()

	srv := mcp.NewServer(rt)

	for _, spec := range []struct {
		path  string
		title string
	}{
		{path: "tickets/a.md", title: "A"},
		{path: "tickets/b.md", title: "B"},
	} {
		if _, callErr := callTool(test, srv, "tusk_node_create", map[string]any{
			"path":  spec.path,
			"type":  "ticket",
			"title": spec.title,
		}); callErr != nil {
			test.Fatalf("tusk_node_create %s: %v", spec.path, callErr)
		}
	}

	if upsertErr := rt.Edges.UpsertAll("tickets/a", index.CLISourcePath, []index.EdgeRow{
		{Type: "blocks", SourceID: "tickets/a", TargetID: "tickets/b", SourcePath: index.CLISourcePath},
	}); upsertErr != nil {
		test.Fatalf("seed __cli__: %v", upsertErr)
	}

	originalBody, readErr := os.ReadFile(filepath.Join(rt.Root, "tickets/a.md"))

	if readErr != nil {
		test.Fatalf("read tickets/a.md: %v", readErr)
	}

	body, callErr := callTool(test, srv, "tusk_doctor", map[string]any{"no_migrate": true})

	if callErr != nil {
		test.Fatalf("tusk_doctor: %v", callErr)
	}

	// File must be unchanged under --no-migrate.
	afterBody, _ := os.ReadFile(filepath.Join(rt.Root, "tickets/a.md"))

	if string(afterBody) != string(originalBody) {
		test.Errorf("tusk_doctor no_migrate=true should leave frontmatter unchanged; before:\n%s\nafter:\n%s", originalBody, afterBody)
	}

	// Response must NOT include a migration report.
	if _, present := body["migrated_count"]; present {
		test.Errorf("tusk_doctor no_migrate=true should omit migrated_count from response; got: %v", body)
	}

	// Legacy row must still be present in the index.
	rows, listErr := rt.Edges.ListBySource("tickets/a")

	if listErr != nil {
		test.Fatalf("list: %v", listErr)
	}

	var sawLegacy bool

	for _, row := range rows {
		if row.SourcePath == index.CLISourcePath {
			sawLegacy = true
		}
	}

	if !sawLegacy {
		test.Errorf("tusk_doctor no_migrate=true should leave the legacy __cli__ row in place; rows: %+v", rows)
	}

	// Response must include a legacy-cli-edge drift issue.
	issues, _ := body["issues"].([]any)

	var sawDrift bool

	for _, raw := range issues {
		entry, ok := raw.(map[string]any)

		if !ok {
			continue
		}

		if entry["kind"] == "legacy-cli-edge" && entry["node_id"] == "tickets/a" {
			sawDrift = true
		}
	}

	if !sawDrift {
		test.Errorf("tusk_doctor no_migrate=true should surface a legacy-cli-edge issue for tickets/a; issues: %v", issues)
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

// newRuntimeWithRefTypes boots a Runtime with a tusk.toml that declares
// person and ticket node-types where ticket.assignee is a ref to person.
func newRuntimeWithRefTypes(test *testing.T) (*mcp.Runtime, *mcp.Server) {
	test.Helper()

	root := test.TempDir()

	manifestBody := []byte(`
[workspace]
name = "test"

[node-types.person]
properties = [{ name = "name", type = "string", required = true }]

[node-types.ticket]
properties = [{ name = "assignee", type = "ref", to = "person" }]
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

func TestTools_NodeCreate_RefDanglingReturnsStructuredKind(test *testing.T) {
	rt, srv := newRuntimeWithRefTypes(test)
	defer rt.Close()

	result := callToolRaw(test, srv, "tusk_node_create", map[string]any{
		"path":  "tickets/auth.md",
		"type":  "ticket",
		"title": "Auth",
		"properties": map[string]any{
			"assignee": "missing",
		},
	})

	if !result.IsError {
		test.Errorf("expected IsError=true")
	}

	body := decodeJSONContent(test, result)

	if body["ok"] != false {
		test.Errorf("ok = %v, want false", body["ok"])
	}

	errorsRaw, ok := body["errors"].([]any)

	if !ok || len(errorsRaw) != 1 {
		test.Fatalf("errors = %v, want one element", body["errors"])
	}

	first, _ := errorsRaw[0].(map[string]any)

	if first["kind"] != "ref_dangling" {
		test.Errorf("kind = %v, want ref_dangling", first["kind"])
	}

	if first["property"] != "assignee" {
		test.Errorf("property = %v, want assignee", first["property"])
	}
}

// snippetStubEmbedder is a deterministic embedder for MCP semantic tests:
// every payload maps to the same unit vector, so any seeded chunk vector
// that equals it scores 1.0.
type snippetStubEmbedder struct{}

func (snippetStubEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}

func (snippetStubEmbedder) Model() string { return "stub" }
func (snippetStubEmbedder) Dim() int      { return 3 }

func TestTool_Query_SemanticIncludesSnippet(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	rt.Embedder = snippetStubEmbedder{}

	rt.Nodes.Upsert(index.NodeRow{
		ID:             "notes/snippet",
		Type:           "note",
		Path:           "notes/snippet.md",
		Title:          "Snippet target",
		PropertiesJSON: "{}",
		LastChecksum:   "x",
	})

	if upsertErr := rt.Embeddings.Upsert(index.EmbeddingRow{
		NodeID:      "notes/snippet",
		ChunkIdx:    0,
		Model:       "stub",
		ContentHash: "h1",
		Vector:      []float32{1, 0, 0},
		Dim:         3,
		Body:        "This is the MCP snippet body content.",
	}); upsertErr != nil {
		test.Fatalf("Upsert: %v", upsertErr)
	}

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_query", map[string]any{
		"filter":   "type=note",
		"semantic": "anything",
	})

	if callErr != nil {
		test.Fatalf("tusk_query: %v", callErr)
	}

	results, _ := body["results"].([]any)

	if len(results) == 0 {
		test.Fatalf("results empty: %v", body)
	}

	first := results[0].(map[string]any)

	snippet, ok := first["snippet"].(string)

	if !ok {
		test.Fatalf("first missing snippet key: %v", first)
	}

	if snippet == "" {
		test.Errorf("snippet empty")
	}

	if !contains(snippet, "MCP snippet body") {
		test.Errorf("snippet content unexpected: %q", snippet)
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

// seedSemanticVault upserts n note nodes and one chunk per node with the
// given per-index vector and body. Vectors must be dim 3.
func seedSemanticVault(test *testing.T, rt *mcp.Runtime, n int, vectorFor func(i int) []float32) {
	test.Helper()

	for offset := 0; offset < n; offset++ {
		id := fmt.Sprintf("notes/n%02d", offset)

		rt.Nodes.Upsert(index.NodeRow{
			ID:             id,
			Type:           "note",
			Path:           id + ".md",
			Title:          fmt.Sprintf("Note %02d", offset),
			PropertiesJSON: "{}",
			LastChecksum:   "x",
		})

		if upsertErr := rt.Embeddings.Upsert(index.EmbeddingRow{
			NodeID:      id,
			ChunkIdx:    0,
			Model:       "stub",
			ContentHash: fmt.Sprintf("h%d", offset),
			Vector:      vectorFor(offset),
			Dim:         3,
			Body:        fmt.Sprintf("body %02d", offset),
		}); upsertErr != nil {
			test.Fatalf("Upsert n%02d: %v", offset, upsertErr)
		}
	}
}

func TestTool_Query_SemanticDefaultTakeIs10(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	rt.Embedder = snippetStubEmbedder{}

	seedSemanticVault(test, rt, 15, func(offset int) []float32 {
		return []float32{1, 0, 0}
	})

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_query", map[string]any{
		"filter":   "type=note",
		"semantic": "anything",
	})

	if callErr != nil {
		test.Fatalf("tusk_query: %v", callErr)
	}

	results, _ := body["results"].([]any)

	if len(results) != 10 {
		test.Errorf("len(results) = %d, want 10 (default take)", len(results))
	}
}

func TestTool_Query_SemanticHonorsExplicitTake(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	rt.Embedder = snippetStubEmbedder{}

	seedSemanticVault(test, rt, 15, func(offset int) []float32 {
		return []float32{1, 0, 0}
	})

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_query", map[string]any{
		"filter":   "type=note",
		"semantic": "anything",
		"take":     3,
	})

	if callErr != nil {
		test.Fatalf("tusk_query: %v", callErr)
	}

	results, _ := body["results"].([]any)

	if len(results) != 3 {
		test.Errorf("len(results) = %d, want 3", len(results))
	}
}

func TestTool_Query_SemanticAppliesMinScoreDefault(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	rt.Embedder = snippetStubEmbedder{}

	// Mix of scores against query vector [1, 0, 0]:
	//   i%4==0 → [1, 0, 0]   score 1.000
	//   i%4==1 → [3, 4, 0]   score 0.600
	//   i%4==2 → [1, 2, 0]   score ~0.447 (below 0.5)
	//   i%4==3 → [1, 9, 0]   score ~0.110 (below 0.5)
	vectors := [][]float32{
		{1, 0, 0},
		{3, 4, 0},
		{1, 2, 0},
		{1, 9, 0},
	}

	seedSemanticVault(test, rt, 8, func(offset int) []float32 {
		return vectors[offset%4]
	})

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_query", map[string]any{
		"filter":   "type=note",
		"semantic": "anything",
	})

	if callErr != nil {
		test.Fatalf("tusk_query: %v", callErr)
	}

	results, _ := body["results"].([]any)

	if len(results) != 4 {
		test.Fatalf("len(results) = %d, want 4 (only scores >= 0.5)", len(results))
	}

	for _, raw := range results {
		row, _ := raw.(map[string]any)
		score, _ := row["score"].(float64)

		if score < 0.5 {
			test.Errorf("row score %v below default min_score 0.5: %v", score, row)
		}
	}
}

func TestTool_Query_SemanticHonorsExplicitMinScore(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	rt.Embedder = snippetStubEmbedder{}

	vectors := [][]float32{
		{1, 0, 0},
		{3, 4, 0},
		{1, 2, 0},
		{1, 9, 0},
	}

	seedSemanticVault(test, rt, 8, func(offset int) []float32 {
		return vectors[offset%4]
	})

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_query", map[string]any{
		"filter":    "type=note",
		"semantic":  "anything",
		"min_score": 0.1,
	})

	if callErr != nil {
		test.Fatalf("tusk_query: %v", callErr)
	}

	results, _ := body["results"].([]any)

	if len(results) != 8 {
		test.Errorf("len(results) = %d, want 8 (min_score=0.1 includes all)", len(results))
	}
}

func TestTool_Query_SemanticIncludesTitle(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	rt.Embedder = snippetStubEmbedder{}

	rt.Nodes.Upsert(index.NodeRow{
		ID:             "notes/titled",
		Type:           "note",
		Path:           "notes/titled.md",
		Title:          "A Titled Note",
		PropertiesJSON: "{}",
		LastChecksum:   "x",
	})

	if upsertErr := rt.Embeddings.Upsert(index.EmbeddingRow{
		NodeID:      "notes/titled",
		ChunkIdx:    0,
		Model:       "stub",
		ContentHash: "h1",
		Vector:      []float32{1, 0, 0},
		Dim:         3,
		Body:        "chunk body",
	}); upsertErr != nil {
		test.Fatalf("Upsert: %v", upsertErr)
	}

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_query", map[string]any{
		"filter":   "type=note",
		"semantic": "anything",
	})

	if callErr != nil {
		test.Fatalf("tusk_query: %v", callErr)
	}

	results, _ := body["results"].([]any)

	if len(results) == 0 {
		test.Fatalf("results empty")
	}

	first, _ := results[0].(map[string]any)
	title, _ := first["title"].(string)

	if title != "A Titled Note" {
		test.Errorf("first.title = %q, want %q", title, "A Titled Note")
	}
}

func TestTool_Query_SemanticReportsFilteredBelowMinScoreWhenEmpty(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	rt.Embedder = snippetStubEmbedder{}

	// All vectors score below 0.5.
	seedSemanticVault(test, rt, 3, func(offset int) []float32 {
		return []float32{1, 9, 0}
	})

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_query", map[string]any{
		"filter":   "type=note",
		"semantic": "anything",
	})

	if callErr != nil {
		test.Fatalf("tusk_query: %v", callErr)
	}

	results, _ := body["results"].([]any)

	if len(results) != 0 {
		test.Errorf("len(results) = %d, want 0 (all pruned)", len(results))
	}

	pruned, ok := body["filtered_below_min_score"].(float64)

	if !ok {
		test.Fatalf("missing filtered_below_min_score in body: %v", body)
	}

	if int(pruned) != 3 {
		test.Errorf("filtered_below_min_score = %v, want 3", pruned)
	}
}

func TestTool_Doctor_WithEmbedStats(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	// Mark workspace as having embeddings configured so doctor runs the stats path.
	rt.Manifest.Embeddings.Provider = "ollama"

	rt.Nodes.Upsert(index.NodeRow{ID: "notes/a", Type: "note", Path: "notes/a.md", PropertiesJSON: "{}", LastChecksum: "x"})

	if upsertErr := rt.Embeddings.Upsert(index.EmbeddingRow{
		NodeID: "notes/a", ChunkIdx: 0, Model: "stub", ContentHash: "h",
		Vector: []float32{0.1}, Dim: 1, Body: "short body",
	}); upsertErr != nil {
		test.Fatalf("Upsert: %v", upsertErr)
	}

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_doctor", map[string]any{})

	if callErr != nil {
		test.Fatalf("tusk_doctor: %v", callErr)
	}

	stats, ok := body["embed_stats"].(map[string]any)

	if !ok {
		test.Fatalf("embed_stats missing or wrong type: %v", body)
	}

	if stats["total_nodes"].(float64) != 1 || stats["total_chunks"].(float64) != 1 {
		test.Errorf("stats = %+v", stats)
	}
}

// TestTool_NodeGet_IncludeFilter covers the new `include` argument on
// tusk_node_get: only requested fields appear in the JSON envelope.
func TestTool_NodeGet_IncludeFilter(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	if _, createErr := rt.NodeService.Create(node.CreateInput{
		RelPath: "notes/hi.md",
		Type:    "note",
		Title:   "Hi",
		Body:    []byte("hello world\n"),
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_node_get", map[string]any{
		"id":      "notes/hi",
		"include": []any{"body"},
	})

	if callErr != nil {
		test.Fatalf("tusk_node_get: %v", callErr)
	}

	if body["body"] == nil || !strings.Contains(body["body"].(string), "hello world") {
		test.Errorf("expected body to include 'hello world', got %v", body["body"])
	}

	if _, present := body["edges"]; present {
		test.Errorf("expected edges to be omitted, got %v", body["edges"])
	}

	if _, present := body["properties"]; present {
		test.Errorf("expected properties to be omitted, got %v", body["properties"])
	}
}

// TestTool_NodeList_IncludeBody asserts the new MCP `include` arg expands
// each row with body content read from disk.
func TestTool_NodeList_IncludeBody(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	if _, createErr := rt.NodeService.Create(node.CreateInput{
		RelPath: "notes/a.md",
		Type:    "note",
		Title:   "A",
		Body:    []byte("alpha body\n"),
	}); createErr != nil {
		test.Fatalf("Create A: %v", createErr)
	}

	if _, createErr := rt.NodeService.Create(node.CreateInput{
		RelPath: "notes/b.md",
		Type:    "note",
		Title:   "B",
		Body:    []byte("beta body\n"),
	}); createErr != nil {
		test.Fatalf("Create B: %v", createErr)
	}

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_node_list", map[string]any{
		"type":    "note",
		"include": []any{"body"},
	})

	if callErr != nil {
		test.Fatalf("tusk_node_list: %v", callErr)
	}

	results, _ := body["results"].([]any)

	if len(results) != 2 {
		test.Fatalf("len(results) = %d, want 2", len(results))
	}

	for _, item := range results {
		entry := item.(map[string]any)

		if entry["body"] == nil {
			test.Errorf("row %s missing body: %+v", entry["id"], entry)
		}
	}
}

// TestTool_NodeList_FormatCompact asserts format=compact returns the
// rendered text inside a single text content block (not JSON).
func TestTool_NodeList_FormatCompact(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	if _, createErr := rt.NodeService.Create(node.CreateInput{
		RelPath: "notes/a.md",
		Type:    "note",
		Title:   "Alpha",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	srv := mcp.NewServer(rt)

	request := mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Name:      "tusk_node_list",
			Arguments: map[string]any{"format": "compact"},
		},
	}

	result, callErr := srv.HandleToolCall(context.Background(), request)

	if callErr != nil {
		test.Fatalf("call: %v", callErr)
	}

	if result.IsError {
		test.Fatalf("result is error: %v", fmtError(result))
	}

	if len(result.Content) != 1 {
		test.Fatalf("expected single content block, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(mcpgo.TextContent)

	if !ok {
		test.Fatalf("expected TextContent, got %T", result.Content[0])
	}

	if !strings.Contains(textContent.Text, "notes/a") || !strings.Contains(textContent.Text, "Alpha") {
		test.Errorf("compact text missing expected fields:\n%s", textContent.Text)
	}

	// The compact body must NOT be valid JSON — that would mean we're
	// double-encoding. Sanity-check by trying to parse and asserting the
	// result is the raw text, not an object.
	var parsed any

	if jsonErr := json.Unmarshal([]byte(textContent.Text), &parsed); jsonErr == nil {
		// If it happened to be valid JSON, it should at least not be a map
		// (the JSON branch returns {"results":[...]}).
		if _, isMap := parsed.(map[string]any); isMap {
			test.Errorf("compact branch returned a JSON object, expected raw text:\n%s", textContent.Text)
		}
	}
}

// TestTool_NodeGet_CompactRespectsInclude verifies the MCP tusk_node_get
// compact path filters Body / Edges / Properties per the include set rather
// than always handing the full node to the renderer.
func TestTool_NodeGet_CompactRespectsInclude(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	if _, createErr := rt.NodeService.Create(node.CreateInput{
		RelPath:    "notes/hi.md",
		Type:       "note",
		Title:      "Hi",
		Body:       []byte("hello world\n"),
		Properties: map[string]any{"priority": 1},
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	srv := mcp.NewServer(rt)

	request := mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Name: "tusk_node_get",
			Arguments: map[string]any{
				"id":      "notes/hi",
				"include": []any{"body"},
				"format":  "compact",
			},
		},
	}

	result, callErr := srv.HandleToolCall(context.Background(), request)

	if callErr != nil {
		test.Fatalf("call: %v", callErr)
	}

	if result.IsError {
		test.Fatalf("result is error: %v", fmtError(result))
	}

	textContent, ok := result.Content[0].(mcpgo.TextContent)

	if !ok {
		test.Fatalf("expected TextContent, got %T", result.Content[0])
	}

	text := textContent.Text

	if !strings.Contains(text, "hello world") {
		test.Errorf("expected body to be rendered:\n%s", text)
	}

	if strings.Contains(text, "priority=1") {
		test.Errorf("expected properties to be filtered out:\n%s", text)
	}
}

// TestTool_EdgeList_FormatCompact mirrors the node_list compact test for
// tusk_edge_list.
func TestTool_EdgeList_FormatCompact(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	// Seed the source node so the FK on edges.source_id is satisfied.
	rt.Nodes.Upsert(index.NodeRow{ID: "a", Type: "note", Path: "a.md", Title: "A", PropertiesJSON: "{}", LastChecksum: "x"})

	rt.Edges.UpsertAll("a", "a.md", []index.EdgeRow{
		{Type: "links", SourceID: "a", TargetID: "b", SourcePath: "a.md"},
	})

	srv := mcp.NewServer(rt)

	request := mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Name:      "tusk_edge_list",
			Arguments: map[string]any{"format": "compact"},
		},
	}

	result, callErr := srv.HandleToolCall(context.Background(), request)

	if callErr != nil {
		test.Fatalf("call: %v", callErr)
	}

	if result.IsError {
		test.Fatalf("result is error: %v", fmtError(result))
	}

	textContent, ok := result.Content[0].(mcpgo.TextContent)

	if !ok {
		test.Fatalf("expected TextContent, got %T", result.Content[0])
	}

	if !strings.Contains(textContent.Text, "links") || !strings.Contains(textContent.Text, "a") {
		test.Errorf("compact edge text missing fields:\n%s", textContent.Text)
	}
}

// TestTool_Query_SubUnitsIncludeUnitsAttachesMatchedUnits verifies the
// structural include=units path returns each file's sub-unit list inline
// as matched_units. Regression test for Task 5 (Phase 2).
func TestTool_Query_SubUnitsIncludeUnitsAttachesMatchedUnits(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	// File row plus two sub-units owned by it.
	if err := rt.Nodes.Upsert(index.NodeRow{
		ID: "notes/with-units", Type: "note", Path: "notes/with-units.md",
		Title: "With units", PropertiesJSON: "{}", LastChecksum: "x",
	}); err != nil {
		test.Fatalf("file upsert: %v", err)
	}

	subRows := []struct {
		id, typ, props string
		ordinal        int
		payload        string
	}{
		{id: "notes/with-units#a", typ: "section", props: `{"heading-level":2}`, ordinal: 0, payload: "Top section"},
		{id: "notes/with-units#b", typ: "paragraph", props: "{}", ordinal: 1, payload: "Body paragraph one"},
	}

	for _, sub := range subRows {
		if err := rt.Nodes.Upsert(index.NodeRow{
			ID: sub.id, Type: sub.typ, Path: "notes/with-units.md",
			PropertiesJSON: sub.props, LastChecksum: "x",
			ParentID:     sql.NullString{String: "notes/with-units", Valid: true},
			Ordinal:      sql.NullInt64{Int64: int64(sub.ordinal), Valid: true},
			EmbedPayload: sql.NullString{String: sub.payload, Valid: true},
		}); err != nil {
			test.Fatalf("sub upsert %s: %v", sub.id, err)
		}
	}

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_query", map[string]any{
		"filter":  "type=note",
		"include": []any{"units"},
	})

	if callErr != nil {
		test.Fatalf("tusk_query: %v", callErr)
	}

	results, _ := body["results"].([]any)

	if len(results) != 1 {
		test.Fatalf("results = %d, want 1", len(results))
	}

	first := results[0].(map[string]any)
	matched, ok := first["matched_units"].([]any)

	if !ok {
		test.Fatalf("matched_units missing or not array: %v", first)
	}

	if len(matched) != 2 {
		test.Errorf("matched_units count = %d, want 2", len(matched))
	}

	section := matched[0].(map[string]any)

	if section["type"] != "section" {
		test.Errorf("first matched type = %v, want section", section["type"])
	}

	if level, _ := section["heading_level"].(float64); level != 2 {
		test.Errorf("heading_level = %v, want 2", section["heading_level"])
	}

	if _, hasScore := section["score"]; hasScore {
		test.Errorf("structural matched_units must not include score: %v", section)
	}
}

// TestTool_Query_DirectSubUnitFilterReturnsRowsWithParentID confirms a
// direct sub-unit filter (e.g. `type=section`) returns sub-units as
// top-level result rows with parent_id populated.
func TestTool_Query_DirectSubUnitFilterReturnsRowsWithParentID(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	if err := rt.Nodes.Upsert(index.NodeRow{
		ID: "notes/owner", Type: "note", Path: "notes/owner.md",
		Title: "Owner", PropertiesJSON: "{}", LastChecksum: "x",
	}); err != nil {
		test.Fatalf("file upsert: %v", err)
	}

	if err := rt.Nodes.Upsert(index.NodeRow{
		ID: "notes/owner#sec", Type: "section",
		Path: "notes/owner.md", PropertiesJSON: `{"heading-level":2}`,
		LastChecksum: "x",
		ParentID:     sql.NullString{String: "notes/owner", Valid: true},
		Ordinal:      sql.NullInt64{Int64: 0, Valid: true},
		EmbedPayload: sql.NullString{String: "head text", Valid: true},
	}); err != nil {
		test.Fatalf("section upsert: %v", err)
	}

	srv := mcp.NewServer(rt)

	body, callErr := callTool(test, srv, "tusk_query", map[string]any{
		"filter": "type=section",
	})

	if callErr != nil {
		test.Fatalf("tusk_query: %v", callErr)
	}

	results, _ := body["results"].([]any)

	if len(results) != 1 {
		test.Fatalf("results = %d, want 1", len(results))
	}

	first := results[0].(map[string]any)

	if first["type"] != "section" {
		test.Errorf("type = %v, want section", first["type"])
	}

	if first["parent_id"] != "notes/owner" {
		test.Errorf("parent_id = %v, want notes/owner", first["parent_id"])
	}

	if _, hasMatched := first["matched_units"]; hasMatched {
		test.Errorf("direct sub-unit result must not carry matched_units: %v", first)
	}
}
