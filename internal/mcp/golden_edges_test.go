package mcp_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/mcp"
)

const edgeManifest = `[workspace]
name = "test"

[edge-types.blocks]
from = ["ticket"]
to = ["ticket"]
cardinality = "many-to-many"
acyclic = true
`

// TestGoldenMCP_Edges pins the edge tools' snake_case envelopes and the
// plain-text cycle rejection (contrast: CLI edge add errors, MCP returns IsError).
func TestGoldenMCP_Edges(test *testing.T) {
	runGoldenMCPCases(test, []goldenMCPCase{
		{
			name:        "edge_add creates an edge",
			manifest:    edgeManifest,
			setup:       mcpEdgeFixture,
			tool:        "tusk_edge_add",
			args:        map[string]any{"type": "blocks", "source_id": "tickets/foo", "target_id": "tickets/bar"},
			wantIsError: false,
			wantJSONObj: true,
			wantText:    `{"source_id":"tickets/foo","target_id":"tickets/bar","type":"blocks"}`,
		},
		{
			name:     "edge_list returns edges",
			manifest: edgeManifest,
			setup: func(test *testing.T, rt *mcp.Runtime) {
				mcpEdgeFixture(test, rt)
				addRealEdge(test, rt, "blocks", "tickets/foo", "tickets/bar")
			},
			tool:        "tusk_edge_list",
			args:        map[string]any{"type": "blocks"},
			wantIsError: false,
			wantJSONObj: true,
			wantText:    `{"count":1,"results":[{"source_id":"tickets/foo","source_path":"tickets/foo.md","target_id":"tickets/bar","type":"blocks"}]}`,
		},
		{
			name:     "edge_remove deletes an edge",
			manifest: edgeManifest,
			setup: func(test *testing.T, rt *mcp.Runtime) {
				mcpEdgeFixture(test, rt)
				addRealEdge(test, rt, "blocks", "tickets/foo", "tickets/bar")
			},
			tool:        "tusk_edge_remove",
			args:        map[string]any{"type": "blocks", "source_id": "tickets/foo", "target_id": "tickets/bar"},
			wantIsError: false,
			wantJSONObj: true,
			wantText:    `{"source_id":"tickets/foo","target_id":"tickets/bar","type":"blocks"}`,
		},
		{
			name:     "edge_add rejects a cycle (plain-text error)",
			manifest: edgeManifest,
			setup: func(test *testing.T, rt *mcp.Runtime) {
				mcpEdgeFixture(test, rt)
				addRealEdge(test, rt, "blocks", "tickets/foo", "tickets/bar")
			},
			tool:        "tusk_edge_add",
			args:        map[string]any{"type": "blocks", "source_id": "tickets/bar", "target_id": "tickets/foo"},
			wantIsError: true,
			wantJSONObj: false,
			wantText:    "node: edge would create a cycle: tickets/bar → … → tickets/foo → tickets/bar",
		},
	})
}

// mcpEdgeFixture creates the two ticket nodes edges reference.
func mcpEdgeFixture(test *testing.T, rt *mcp.Runtime) {
	createRealNode(test, rt, "tickets/foo.md", "ticket", "Foo")
	createRealNode(test, rt, "tickets/bar.md", "ticket", "Bar")
}
