package index_test

import (
	"database/sql"
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/wikilink"
)

// seedBlocks upserts paragraph rows under fileID in document order, each
// [address, block id] with "" for a paragraph no marker names, keyed the way
// the sub-unit sync keys them.
func seedBlocks(test *testing.T, store *index.Index, fileID string, blocks ...[2]string) {
	test.Helper()

	rows := make([]index.NodeRow, 0, len(blocks))

	for position, block := range blocks {
		row := index.NodeRow{
			ID: fileID + "#" + block[0], Type: "paragraph", Path: fileID + ".md", Title: block[0],
			PropertiesJSON: "{}", LastChecksum: "x",
			ParentID: sql.NullString{String: fileID, Valid: true},
			Ordinal:  sql.NullInt64{Int64: int64(position), Valid: true},
		}

		if block[1] != "" {
			row.NameKey = sql.NullString{String: wikilink.BlockKey(block[1]), Valid: true}
		}

		rows = append(rows, row)
	}

	if upsertErr := index.NewNodeRepo(store).BulkUpsert(rows, "markdown"); upsertErr != nil {
		test.Fatalf("seed blocks of %s: %v", fileID, upsertErr)
	}
}

// A block reference resolves to the unit its ^id names, in any case and to
// the first of several units sharing an id, follows the id when it moves to
// another unit, and lands on the note while no unit has it (#819).
func TestResolveLinks_BlockReferencesFindTheirUnit(test *testing.T) {
	store := openTestIndex(test)
	seedFiles(test, store, "inbox/today.md", "notes/auth-rfc.md")
	seedBlocks(test, store, "notes/auth-rfc", [2]string{"P1", "quote-1"}, [2]string{"P2", "Dup"}, [2]string{"P3", "dup"})

	edges := index.NewEdgeRepo(store)

	if upsertErr := edges.UpsertAll("inbox/today", "inbox/today.md", []index.EdgeRow{
		linkEdge("inbox/today", "auth-rfc#^Quote-1", "auth-rfc#^Quote-1"),
		linkEdge("inbox/today", "notes/auth-rfc#^dup", "notes/auth-rfc#^dup"),
		linkEdge("inbox/today", "auth-rfc#^later", "auth-rfc#^later"),
	}); upsertErr != nil {
		test.Fatalf("UpsertAll: %v", upsertErr)
	}

	if changed := resolveLinksOnce(test, store); changed != 3 {
		test.Errorf("ResolveLinks changed %d rows, want 3", changed)
	}

	want := []index.EdgeRow{
		linkEdge("inbox/today", "notes/auth-rfc", "auth-rfc#^later"),
		linkEdge("inbox/today", "notes/auth-rfc#P1", "auth-rfc#^Quote-1"),
		linkEdge("inbox/today", "notes/auth-rfc#P2", "notes/auth-rfc#^dup"),
	}

	if got := edgeTargets(test, store); !reflect.DeepEqual(got, want) {
		test.Errorf("edges =\n%+v\nwant\n%+v", got, want)
	}

	if changed := resolveLinksOnce(test, store); changed != 0 {
		test.Errorf("a second pass changed %d rows, want none", changed)
	}

	// The quote's id moves to a later paragraph, and ^later appears.
	seedBlocks(test, store, "notes/auth-rfc",
		[2]string{"P1", ""}, [2]string{"P2", "Dup"}, [2]string{"P3", "dup"}, [2]string{"P4", "quote-1"}, [2]string{"P5", "later"})
	resolveLinksOnce(test, store)

	want = []index.EdgeRow{
		linkEdge("inbox/today", "notes/auth-rfc#P2", "notes/auth-rfc#^dup"),
		linkEdge("inbox/today", "notes/auth-rfc#P4", "auth-rfc#^Quote-1"),
		linkEdge("inbox/today", "notes/auth-rfc#P5", "auth-rfc#^later"),
	}

	if got := edgeTargets(test, store); !reflect.DeepEqual(got, want) {
		test.Errorf("after the ids move, edges =\n%+v\nwant\n%+v", got, want)
	}
}

// The index-backed lookup a single note's refs resolve through finds blocks
// too, and never takes a heading for one.
func TestFileNames_ResolvesBlockReferences(test *testing.T) {
	store := openTestIndex(test)
	seedFiles(test, store, "notes/auth-rfc.md")
	seedSections(test, store, "notes/auth-rfc", [3]string{"S1", "Overview", ""})
	seedBlocks(test, store, "notes/auth-rfc", [2]string{"S1P1", "Overview"})

	files := index.NewNodeRepo(store).FileNames(wikilink.CaseInsensitive)
	source := wikilink.Source{ID: "notes/auth-rfc", Path: "notes/auth-rfc.md"}

	for target, want := range map[string]wikilink.Target{
		"auth-rfc#^overview": {ID: "notes/auth-rfc#S1P1"},
		"#^OVERVIEW":         {ID: "notes/auth-rfc#S1P1"},
		"auth-rfc#Overview":  {ID: "notes/auth-rfc#S1"},
		"auth-rfc#^S1":       {ID: "notes/auth-rfc", MissingFragment: true},
	} {
		got, resolveErr := wikilink.ResolveTarget(target, source, files)

		if resolveErr != nil {
			test.Fatalf("ResolveTarget(%q): %v", target, resolveErr)
		}

		if !reflect.DeepEqual(got, want) {
			test.Errorf("ResolveTarget(%q) = %+v, want %+v", target, got, want)
		}
	}
}
