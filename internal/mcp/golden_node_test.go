package mcp_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/mcp"
)

// TestGoldenMCP_Node pins the byte-stable behavior of the tusk_node_* tools.
// The two cases here are the highest-risk MCP trap — the two error encodings —
// because callTool collapses both into a Go error and so cannot tell them
// apart. The wire contract distinguishes a plain-text toolError (IsError +
// non-JSON text) from a structured toolJSONError (IsError + a JSON object).
func TestGoldenMCP_Node(test *testing.T) {
	const ticketManifest = `[workspace]
name = "test"

[node-types.ticket]
properties = [
    { name = "summary", type = "string", required = true },
]
`

	runGoldenMCPCases(test, []goldenMCPCase{
		{
			name:        "node_get missing id is a plain-text error",
			tool:        "tusk_node_get",
			args:        map[string]any{},
			wantIsError: true,
			wantJSONObj: false,
			wantText:    `missing or non-string argument "id"`,
		},
		{
			name:        "node_create property rejection is a structured JSON error",
			manifest:    ticketManifest,
			tool:        "tusk_node_create",
			args:        map[string]any{"type": "ticket", "path": "tickets/foo.md"},
			wantIsError: true,
			wantJSONObj: true,
			wantText:    goldenNodeTypesRejection,
		},
	})
}

// goldenNodeTypesRejection is the exact JSON body tusk_node_create returns when
// a required property is missing. json.Marshal sorts map keys, so this literal
// is deterministic regardless of map iteration order.
const goldenNodeTypesRejection = `{"error":"node-types-rejection","errors":[{"kind":"required-missing","property":"summary","reason":"is required (declared in [node-types.ticket])","type":"string"}],"node_id":"tickets/foo","node_type":"ticket","op":"create"}`

// TestGoldenMCP_NodeLifecycle pins the create/get/list/modify/move/delete tool
// envelopes — the structured-JSON counterparts to the CLI's human-readable lines.
func TestGoldenMCP_NodeLifecycle(test *testing.T) {
	runGoldenMCPCases(test, []goldenMCPCase{
		{
			name:        "node_create writes a node",
			tool:        "tusk_node_create",
			args:        map[string]any{"type": "note", "path": "notes/x.md", "title": "X"},
			wantIsError: false,
			wantJSONObj: true,
			wantText:    `{"id":"notes/x","path":"notes/x.md","title":"X","type":"note"}`,
		},
		{
			name:        "node_get returns the node envelope",
			setup:       func(test *testing.T, rt *mcp.Runtime) { createRealNode(test, rt, "notes/g.md", "note", "G") },
			tool:        "tusk_node_get",
			args:        map[string]any{"id": "notes/g"},
			wantIsError: false,
			wantJSONObj: true,
			// No include filter → full envelope (body empty, edges null, props).
			wantText: `{"body":"","edges":null,"id":"notes/g","path":"notes/g.md","properties":{"title":"G","type":"note"},"title":"G","type":"note"}`,
		},
		{
			name: "node_list sorts by id ascending",
			setup: func(test *testing.T, rt *mcp.Runtime) {
				seedNode(test, rt, "notes/c", "note")
				seedNode(test, rt, "notes/a", "note")
				seedNode(test, rt, "notes/b", "note")
			},
			tool:        "tusk_node_list",
			args:        map[string]any{"type": "note"},
			wantIsError: false,
			wantJSONObj: true,
			// Seeded c, a, b — returned a, b, c (MCP forces Sort=+id).
			wantText: `{"count":3,"results":[{"id":"notes/a","path":"notes/a.md","title":"","type":"note"},{"id":"notes/b","path":"notes/b.md","title":"","type":"note"},{"id":"notes/c","path":"notes/c.md","title":"","type":"note"}]}`,
		},
		{
			name:        "node_modify sets a property",
			setup:       func(test *testing.T, rt *mcp.Runtime) { createRealNode(test, rt, "notes/m.md", "note", "M") },
			tool:        "tusk_node_modify",
			args:        map[string]any{"id": "notes/m", "set": map[string]any{"tag": "urgent"}},
			wantIsError: false,
			wantJSONObj: true,
			wantText:    `{"id":"notes/m","path":"notes/m.md","properties":{"tag":"urgent","title":"M","type":"note"},"title":"M","type":"note"}`,
		},
		{
			name:        "node_move renames the node",
			setup:       func(test *testing.T, rt *mcp.Runtime) { createRealNode(test, rt, "notes/old.md", "note", "O") },
			tool:        "tusk_node_move",
			args:        map[string]any{"id": "notes/old", "new_path": "notes/new.md"},
			wantIsError: false,
			wantJSONObj: true,
			wantText:    `{"affected_files":null,"new_id":"notes/new","new_path":"notes/new.md","old_id":"notes/old","old_path":"notes/old.md"}`,
		},
		{
			name:        "node_delete removes the node",
			setup:       func(test *testing.T, rt *mcp.Runtime) { createRealNode(test, rt, "notes/d.md", "note", "D") },
			tool:        "tusk_node_delete",
			args:        map[string]any{"id": "notes/d"},
			wantIsError: false,
			wantJSONObj: true,
			wantText:    `{"deleted_id":"notes/d"}`,
		},
	})
}
