package doctor_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
)

func TestRun_PathMissingWarnsOnAnchoredRefsThatAreGone(test *testing.T) {
	root := test.TempDir()

	if mkErr := os.MkdirAll(filepath.Join(root, "server/ledger/core"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	for _, file := range []string{"server/ledger/core/service.go", "server/ledger/readme.md"} {
		if writeErr := os.WriteFile(filepath.Join(root, file), []byte("x"), 0o644); writeErr != nil {
			test.Fatalf("write: %v", writeErr)
		}
	}

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open: %v", openErr)
	}

	defer store.Close()

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)

	if upsertErr := nodes.Upsert(index.NodeRow{ID: "technical/ledger", Type: "technical", Path: "technical/ledger.md", PropertiesJSON: `{}`}); upsertErr != nil {
		test.Fatalf("upsert: %v", upsertErr)
	}

	section := index.NodeRow{
		ID: "technical/ledger#S1", Type: "section", Path: "technical/ledger.md", Title: "Ledger", PropertiesJSON: `{}`,
		ParentID:  sql.NullString{String: "technical/ledger", Valid: true},
		StartLine: sql.NullInt64{Int64: 5, Valid: true},
		EndLine:   sql.NullInt64{Int64: 40, Valid: true},
	}

	if upsertErr := nodes.BulkUpsert([]index.NodeRow{section}, "markdown"); upsertErr != nil {
		test.Fatalf("section: %v", upsertErr)
	}

	refs := []index.PathRefRow{
		{Type: "describes", Target: "server/ledger/core/service.go", Line: 7},  // present
		{Type: "describes", Target: "server/ledger/old.go", Line: 9},           // gone
		{Type: "describes", Target: "server/ledger/old.go", Line: 12},          // gone, same finding
		{Type: "describes", Target: "server/Ledger/core/service.go", Line: 14}, // wrong case
		{Type: "describes", Target: "application/json", Line: 16},              // never a path
		{Type: "describes", Target: "go.sum", Line: 18},                        // single segment, absent
		{Type: "describes", Target: "server/ledger/readme", Line: 20},          // a markdown node id, as an HTML href names it
	}

	if replaceErr := edges.ReplacePathRefs("technical/ledger", refs); replaceErr != nil {
		test.Fatalf("refs: %v", replaceErr)
	}

	report, runErr := doctor.Run(doctor.Config{Nodes: nodes, Edges: edges, DB: store.DB(), Root: root})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	var found []doctor.Issue

	for _, issue := range report.Issues {
		if issue.Kind == doctor.IssuePathMissing {
			found = append(found, issue)
		}
	}

	if len(found) != 2 {
		test.Fatalf("path-missing issues = %+v, want 2 (old.go, wrong-case path)", found)
	}

	first := found[0]

	if first.NodeID != "technical/ledger" || first.Severity != doctor.SeverityWarning {
		test.Errorf("issue = %+v, want a warning on technical/ledger", first)
	}

	if want := `describes names "server/ledger/old.go" (lines 9, 12), which is not on disk`; first.Message != want {
		test.Errorf("message = %q, want %q", first.Message, want)
	}

	if !slices.Equal(first.Locations, []string{"technical/ledger#S1"}) {
		test.Errorf("locations = %v, want the section", first.Locations)
	}

	if found[1].Message != `describes names "server/Ledger/core/service.go" (line 14), which is not on disk` {
		test.Errorf("case-mismatch message = %q", found[1].Message)
	}

	if counts := report.Counts(); counts.Errors != 0 {
		test.Errorf("path-missing must not count as an error: %+v", counts)
	}
}
