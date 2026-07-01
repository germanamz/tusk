package node_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

// Regression: ReindexSource re-persists the node row, so it must canonicalize a
// declared date the way a full reindex does — writing the date-only string, not
// json.Marshal's RFC3339 timestamp for the time.Time the YAML parser produces
// from an unquoted on-disk date. Otherwise an idempotent edge op flips the
// indexed value to "...T00:00:00Z" and breaks exact-match / range date filters.
func TestReindexSource_CanonicalizesUnquotedDateInNodeRow(test *testing.T) {
	dir := test.TempDir()
	idx, _ := index.Open(filepath.Join(dir, ".tusk", "index.db"))
	defer idx.Close()

	repo := index.NewNodeRepo(idx)
	edgeRepo := index.NewEdgeRepo(idx)

	nodeTypes := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "due", Type: "date"}}},
	}
	edgeTypes := manifest.EdgeTypes{}

	// Author the date UNQUOTED on disk — the YAML parser decodes it to a time.Time.
	if err := os.MkdirAll(filepath.Join(dir, "tickets"), 0o755); err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tickets/t.md"),
		[]byte("---\ntype: ticket\ntitle: T\ndue: 2026-06-11\n---\nbody\n"), 0o644); err != nil {
		test.Fatal(err)
	}

	if err := node.ReindexSource(dir, repo, edgeRepo, node.NewIndexRefLookup(repo), edgeTypes, nodeTypes, "tickets/t"); err != nil {
		test.Fatalf("ReindexSource: %v", err)
	}

	row, getErr := repo.Get("tickets/t")
	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if strings.Contains(row.PropertiesJSON, "2026-06-11T00:00:00Z") {
		test.Errorf("node row date not canonicalized to date-only:\n%s", row.PropertiesJSON)
	}
	if !strings.Contains(row.PropertiesJSON, `"due":"2026-06-11"`) {
		test.Errorf("expected date-only due in node row, got:\n%s", row.PropertiesJSON)
	}
}
