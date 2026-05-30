package mcp_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/mcp"
)

// TestGoldenMCP_Query pins tusk_query: the structural {results,count} envelope,
// the required-filter error, and a semantic {results,count,model} envelope ranked
// by the deterministic stub embedder.
func TestGoldenMCP_Query(test *testing.T) {
	runGoldenMCPCases(test, []goldenMCPCase{
		{
			name: "query returns structural rows sorted by id",
			setup: func(test *testing.T, rt *mcp.Runtime) {
				seedNode(test, rt, "notes/b", "note")
				seedNode(test, rt, "notes/a", "note")
			},
			tool:        "tusk_query",
			args:        map[string]any{"filter": "type=note", "sort": "+id"},
			wantIsError: false,
			wantJSONObj: true,
			wantText:    `{"count":2,"results":[{"id":"notes/a","path":"notes/a.md","title":"","type":"note"},{"id":"notes/b","path":"notes/b.md","title":"","type":"note"}]}`,
		},
		{
			name:        "query requires a filter",
			tool:        "tusk_query",
			args:        map[string]any{},
			wantIsError: true,
			wantJSONObj: false,
			wantText:    `missing or non-string argument "filter"`,
		},
	})
}
