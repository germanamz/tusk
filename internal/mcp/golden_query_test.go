package mcp_test

import (
	"os"
	"path/filepath"
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
			name: "names-path lists the pages naming a path and where",
			manifest: `[workspace]
name = "test"

[node-types.technical]

[edge-types.describes]
from  = ["technical"]
paths = true
`,
			setup:       seedPathRefVault,
			tool:        "tusk_query",
			args:        map[string]any{"filter": "names-path=server/ledger/core/service.go", "include": []any{"paths"}, "sort": "+id"},
			wantIsError: false,
			wantJSONObj: true,
			wantText: `{"count":2,"results":[` +
				`{"id":"technical/arch","path":"technical/arch.md","paths":[{"path":"server/ledger","edge_type":"describes","exists":true,"mentions":[{"line":8,"section":"technical/arch#S1","heading":"Modules"}]}],"title":"Architecture","type":"technical"},` +
				`{"id":"technical/ledger","path":"technical/ledger.md","paths":[{"path":"server/ledger/core/service.go","edge_type":"describes","exists":true,"mentions":[{"line":10,"section":"technical/ledger#S1.1","heading":"Core service"}]}],"title":"Ledger","type":"technical"}]}`,
		},
		{
			name:        "names-path needs an edge type with paths = true",
			tool:        "tusk_query",
			args:        map[string]any{"filter": "names-path=server/a.go"},
			wantIsError: true,
			wantJSONObj: false,
			wantText:    `filter validate: names-path needs an edge type with paths = true at column 1 (declare one in tusk.toml, e.g. [edge-types.describes] with from and paths = true)`,
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

// seedPathRefVault writes two technical pages that name a source file, the
// file itself, and reindexes so the pages' path refs and sections exist.
func seedPathRefVault(test *testing.T, rt *mcp.Runtime) {
	test.Helper()

	files := map[string]string{
		"server/ledger/core/service.go": "package core\n",
		"technical/ledger.md":           "---\ntype: technical\ntitle: Ledger\n---\n\n# Ledger\n\n## Core service\n\nLives in `server/ledger/core/service.go`, wired in `server/wire.go`.\n",
		"technical/arch.md":             "---\ntype: technical\ntitle: Architecture\n---\n\n# Modules\n\nThe ledger is `server/ledger/`.\n",
	}

	for relPath, content := range files {
		absPath := filepath.Join(rt.Root, relPath)

		if mkErr := os.MkdirAll(filepath.Dir(absPath), 0o755); mkErr != nil {
			test.Fatalf("mkdir: %v", mkErr)
		}

		if writeErr := os.WriteFile(absPath, []byte(content), 0o644); writeErr != nil {
			test.Fatalf("write %s: %v", relPath, writeErr)
		}
	}

	if text, isError := rawToolResult(test, mcp.NewServer(rt), "tusk_reindex", map[string]any{}); isError {
		test.Fatalf("reindex: %s", text)
	}
}
