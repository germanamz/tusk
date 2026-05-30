package mcp_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/mcp"
)

const (
	mcpAliasManifest = `[workspace]
name = "test"

[alias.snap]
command = "status"
description = "Quick snapshot"
`

	mcpContextManifest = `[workspace]
name = "test"
sub-units = false

[context]
pinned = ["notes/alpha"]
`
)

// TestGoldenMCP_Run pins the tusk_run dispatch envelope.
func TestGoldenMCP_Run(test *testing.T) {
	runGoldenMCPCases(test, []goldenMCPCase{
		{
			name:        "run dispatches an alias",
			manifest:    mcpAliasManifest,
			tool:        "tusk_run",
			args:        map[string]any{"alias": "snap"},
			wantIsError: false,
			wantJSONObj: true,
			wantText:    `{"alias":"snap","command":"status","kind":"status","result":{"edge_count":0,"embed_queue_depth":0,"last_reindex_at":"<TS>","nodes_by_type":{},"reindex_queue_depth":0}}`,
		},
	})
}

// TestGoldenMCP_Context pins tusk_context: the empty (no [context]) envelope and
// a pinned-node digest.
func TestGoldenMCP_Context(test *testing.T) {
	runGoldenMCPCases(test, []goldenMCPCase{
		{
			name:        "context with no [context] block is empty",
			tool:        "tusk_context",
			args:        map[string]any{},
			wantIsError: false,
			wantJSONObj: true,
			wantText:    `{}`,
		},
		{
			name:        "context composes a pinned node",
			manifest:    mcpContextManifest,
			setup:       func(test *testing.T, rt *mcp.Runtime) { createRealNode(test, rt, "notes/alpha.md", "note", "A") },
			tool:        "tusk_context",
			args:        map[string]any{},
			wantIsError: false,
			wantJSONObj: true,
			wantText:    `{"pinned":[{"id":"notes/alpha","type":"note","path":"notes/alpha.md","title":"A","body":"---\ntype: note\ntitle: A\n---\n\n\n"}]}`,
		},
	})
}
