package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

// TestFreshCheckoutKeepsEdges replays the scenario from issue #406: 8 nodes +
// 7 edges authored via `tusk node create` / `tusk edge add`. Delete the DB.
// Reindex. The 7 edges must survive — proving edges are durably stored in
// source frontmatter rather than the now-removed __cli__ DB-only path.
func TestFreshCheckoutKeepsEdges(test *testing.T) {
	manifestBody := `
[workspace]
name = "test"

[node-types.wbs-node]
properties = [
    { name = "order", type = "int" },
]

[edge-types.wbs-parent]
from        = ["wbs-node"]
to          = ["wbs-node"]
cardinality = "many-to-one"
hierarchy   = "wbs"
`

	dir := initWorkspaceWithManifest(test, manifestBody)

	// 8 nodes: 1 root + 7 children.
	createNode(test, dir, "wbs/root.md", "wbs-node", "Root", "")

	for childIndex := 1; childIndex <= 7; childIndex++ {
		createNode(test, dir, fmt.Sprintf("wbs/c%d.md", childIndex), "wbs-node", fmt.Sprintf("C%d", childIndex), "")
	}

	// 7 edges: each child → root.
	for childIndex := 1; childIndex <= 7; childIndex++ {
		output, addErr := runCLI("edge", "add", "--type", "wbs-parent", "--source", fmt.Sprintf("wbs/c%d", childIndex), "--target", "wbs/root")

		if addErr != nil {
			test.Fatalf("edge add c%d: %v\noutput: %s", childIndex, addErr, output)
		}
	}

	// Verify the index has 7 wbs-parent edges before destroying it.
	dbPath := filepath.Join(dir, ".tusk", "index.db")
	assertEdgeCount(test, dbPath, "wbs-parent", 7)

	// Nuke the DB.
	if removeErr := os.RemoveAll(dbPath); removeErr != nil {
		test.Fatalf("rm db: %v", removeErr)
	}

	// Reindex.
	if output, reindexErr := runCLI("reindex"); reindexErr != nil {
		test.Fatalf("reindex: %v\noutput: %s", reindexErr, output)
	}

	// Edges must be back.
	assertEdgeCount(test, dbPath, "wbs-parent", 7)
}

func assertEdgeCount(test *testing.T, dbPath, edgeType string, expected int) {
	test.Helper()

	store, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("open db: %v", openErr)
	}

	defer store.Close()

	rows, listErr := index.NewEdgeRepo(store).ListByType(edgeType)

	if listErr != nil {
		test.Fatalf("list %s: %v", edgeType, listErr)
	}

	if len(rows) != expected {
		test.Errorf("expected %d %s edges, got %d", expected, edgeType, len(rows))
	}
}
