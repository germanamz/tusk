package mcp_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/mcp"
)

// openEdgeViewsServer boots a runtime over the #848 repro: src links target
// from its intro paragraph and from a paragraph under "## Section", so the
// file and four of its sub-units store a references edge to target.
func openEdgeViewsServer(test *testing.T) *mcp.Server {
	test.Helper()

	root := test.TempDir()
	files := map[string]string{
		"tusk.toml": `[workspace]
name = "x"

[node-types.note]
description = "n"

[edge-types.references]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
wikilinks = true
`,
		"target.md": "---\ntype: note\n---\n\n# Target\n",
		"src.md":    "---\ntype: note\n---\n\n# Source\n\nIntro links [[target]].\n\n## Section\n\nOne paragraph that links [[target]].\n",
	}

	for name, content := range files {
		if writeErr := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); writeErr != nil {
			test.Fatalf("write %s: %v", name, writeErr)
		}
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	test.Cleanup(func() { rt.Close() })

	srv := mcp.NewServer(rt)

	if _, reindexErr := callTool(test, srv, "tusk_reindex", map[string]any{"no_embed": true}); reindexErr != nil {
		test.Fatalf("tusk_reindex: %v", reindexErr)
	}

	return srv
}

// TestTool_EdgeListFoldsSubUnits pins tusk_edge_list's #848 view: one row from
// the referring file with its passages as units, and sub_units: true for the
// stored rows.
func TestTool_EdgeListFoldsSubUnits(test *testing.T) {
	srv := openEdgeViewsServer(test)

	body, callErr := callTool(test, srv, "tusk_edge_list", map[string]any{"to": "target"})

	if callErr != nil {
		test.Fatalf("tusk_edge_list: %v", callErr)
	}

	results, _ := body["results"].([]any)

	if len(results) != 1 {
		test.Fatalf("results = %v, want one row from src", results)
	}

	row := results[0].(map[string]any)
	units := anyStrings(row["units"])

	if row["source_id"] != "src" || !slices.Equal(units, []string{"src#S1P1", "src#S1.1P1"}) {
		test.Errorf("row = %v, want source_id src and units [src#S1P1 src#S1.1P1]", row)
	}

	stored, storedErr := callTool(test, srv, "tusk_edge_list", map[string]any{"to": "target", "sub_units": true})

	if storedErr != nil {
		test.Fatalf("tusk_edge_list sub_units: %v", storedErr)
	}

	if count := stored["count"]; count != float64(5) {
		test.Errorf("sub_units: count = %v, want 5 (src and four sub-units)", count)
	}

	if _, typeErr := callTool(test, srv, "tusk_edge_list", map[string]any{"to": "target", "sub_units": "yes"}); typeErr == nil {
		test.Errorf("sub_units: \"yes\" was accepted, want a type error")
	}
}

// TestTool_NodeGetEdgeFilters pins tusk_node_get's edge_type, direction and
// sub_units arguments, each of which implies include edges.
func TestTool_NodeGetEdgeFilters(test *testing.T) {
	srv := openEdgeViewsServer(test)

	body, callErr := callTool(test, srv, "tusk_node_get", map[string]any{"id": "target", "edge_type": "references", "direction": "in"})

	if callErr != nil {
		test.Fatalf("tusk_node_get: %v", callErr)
	}

	edges, _ := body["edges"].([]any)

	if len(edges) != 1 {
		test.Fatalf("edges = %v, want one incoming references edge", edges)
	}

	edge := edges[0].(map[string]any)

	if edge["target_id"] != "src" || edge["direction"] != "in" || !slices.Equal(anyStrings(edge["units"]), []string{"src#S1P1", "src#S1.1P1"}) {
		test.Errorf("edge = %v, want in from src with units", edge)
	}

	if _, present := body["body"]; present {
		test.Errorf("edge filters returned the body too: %v", body)
	}

	stored, storedErr := callTool(test, srv, "tusk_node_get", map[string]any{"id": "target", "sub_units": true})

	if storedErr != nil {
		test.Fatalf("tusk_node_get sub_units: %v", storedErr)
	}

	if storedEdges, _ := stored["edges"].([]any); len(storedEdges) != 6 {
		test.Errorf("sub_units: %d edges, want 6 (five references in, one contains out): %v", len(storedEdges), storedEdges)
	}

	for _, args := range []map[string]any{
		{"id": "target", "direction": "sideways"},
		{"id": "target", "edge_type": 3},
	} {
		if _, argErr := callTool(test, srv, "tusk_node_get", args); argErr == nil {
			test.Errorf("tusk_node_get %v succeeded, want an error", args)
		} else if !strings.Contains(argErr.Error(), "direction") && !strings.Contains(argErr.Error(), "edge_type") {
			test.Errorf("tusk_node_get %v: error %v does not name the argument", args, argErr)
		}
	}
}

func anyStrings(raw any) []string {
	values, _ := raw.([]any)
	out := make([]string, 0, len(values))

	for _, value := range values {
		text, _ := value.(string)
		out = append(out, text)
	}

	return out
}
