package subunit_test

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/subunit"
)

func TestSync_SourceZeroValueWritesMarkdownContainsEdges(test *testing.T) {
	store := openSyncTestIndex(test)
	loaded := referencesManifest(test)
	sync, nodes, edges, _ := newSync(store, loaded)

	parent := seedFileRow(test, nodes, "notes/intro", "notes/intro.md")

	units, parseErr := subunit.Parse([]byte("# Title\n\nFirst paragraph.\n"))

	if parseErr != nil {
		test.Fatalf("Parse: %v", parseErr)
	}

	if _, applyErr := sync.ApplyFile(context.Background(), parent, units); applyErr != nil {
		test.Fatalf("ApplyFile: %v", applyErr)
	}

	contains, listErr := edges.ListBySource(parent.ID)

	if listErr != nil {
		test.Fatalf("ListBySource: %v", listErr)
	}

	if len(contains) == 0 {
		test.Fatalf("no contains edges written")
	}

	for _, edge := range contains {
		if edge.Type != "contains" {
			continue
		}

		if !edge.Source.Valid || edge.Source.String != "markdown" {
			test.Errorf("contains edge source = %+v, want {markdown true}", edge.Source)
		}
	}
}

func TestSync_SourceHTMLWritesHTMLContainsEdges(test *testing.T) {
	store := openSyncTestIndex(test)
	loaded := referencesManifest(test)
	sync, nodes, edges, _ := newSync(store, loaded)

	sync.Source = "html"

	parent := seedFileRow(test, nodes, "notes/page.html", "notes/page.html")

	units, parseErr := subunit.Parse([]byte("# Title\n\nFirst paragraph.\n"))

	if parseErr != nil {
		test.Fatalf("Parse: %v", parseErr)
	}

	if _, applyErr := sync.ApplyFile(context.Background(), parent, units); applyErr != nil {
		test.Fatalf("ApplyFile: %v", applyErr)
	}

	contains, listErr := edges.ListBySource(parent.ID)

	if listErr != nil {
		test.Fatalf("ListBySource: %v", listErr)
	}

	if len(contains) == 0 {
		test.Fatalf("no contains edges written")
	}

	for _, edge := range contains {
		if edge.Type != "contains" {
			continue
		}

		if !edge.Source.Valid || edge.Source.String != "html" {
			test.Errorf("contains edge source = %+v, want {html true}", edge.Source)
		}
	}
}

func TestSync_SourceHTMLStampsHTMLOnSubUnitNodeRows(test *testing.T) {
	store := openSyncTestIndex(test)
	loaded := referencesManifest(test)
	sync, nodes, _, _ := newSync(store, loaded)

	sync.Source = "html"

	parent := seedFileRow(test, nodes, "notes/page.html", "notes/page.html")

	units, parseErr := subunit.Parse([]byte("# Title\n\nFirst paragraph.\n"))

	if parseErr != nil {
		test.Fatalf("Parse: %v", parseErr)
	}

	if _, applyErr := sync.ApplyFile(context.Background(), parent, units); applyErr != nil {
		test.Fatalf("ApplyFile: %v", applyErr)
	}

	rows, queryErr := store.DB().Query(`SELECT id, source FROM nodes WHERE id GLOB ?`, parent.ID+"#*")

	if queryErr != nil {
		test.Fatalf("query sub-unit rows: %v", queryErr)
	}

	defer rows.Close()

	seen := 0

	for rows.Next() {
		var (
			id     string
			source *string
		)

		if scanErr := rows.Scan(&id, &source); scanErr != nil {
			test.Fatalf("scan: %v", scanErr)
		}

		seen++

		if source == nil || *source != "html" {
			test.Errorf("row %s: source = %v, want \"html\"", id, source)
		}
	}

	if seen == 0 {
		test.Fatalf("no sub-unit rows written")
	}
}
