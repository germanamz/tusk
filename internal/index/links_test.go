package index_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/wikilink"
)

// seedFiles upserts a file row for each workspace-relative markdown path.
func seedFiles(test *testing.T, store *index.Index, paths ...string) {
	test.Helper()

	repo := index.NewNodeRepo(store)

	for _, relPath := range paths {
		row := index.NodeRow{
			ID:             index.NodeIDForPath(relPath),
			Type:           "note",
			Path:           relPath,
			PropertiesJSON: "{}",
			LastChecksum:   "x",
		}

		if upsertErr := repo.Upsert(row); upsertErr != nil {
			test.Fatalf("upsert %s: %v", relPath, upsertErr)
		}
	}
}

func TestFileNames_ResolveName(test *testing.T) {
	store := openTestIndex(test)
	seedFiles(test, store, "notes/Auth RFC.md", "a/dup.md", "b/dup.md", "root.md", "deep/root.md")

	files := index.NewNodeRepo(store).FileNames(wikilink.CaseInsensitive)

	for _, tc := range []struct {
		name       string
		wantID     string
		candidates []string
	}{
		{"auth rfc", "notes/Auth RFC", nil},
		{"Auth RFC", "notes/Auth RFC", nil},
		{"dup", "", []string{"a/dup", "b/dup"}},
		{"root", "root", nil},
		{"missing", "", nil},
	} {
		gotID, gotCandidates, resolveErr := files.ResolveName(tc.name)

		if resolveErr != nil {
			test.Fatalf("ResolveName(%q): %v", tc.name, resolveErr)
		}

		if gotID != tc.wantID || !reflect.DeepEqual(gotCandidates, tc.candidates) {
			test.Errorf("ResolveName(%q) = %q, %v; want %q, %v", tc.name, gotID, gotCandidates, tc.wantID, tc.candidates)
		}
	}

	sensitive := index.NewNodeRepo(store).FileNames(wikilink.CaseSensitive)

	if id, _, _ := sensitive.ResolveName("auth rfc"); id != "" {
		test.Errorf("case-sensitive lookup matched another case: %q", id)
	}
}

// edgeTargets lists source → target for every non-structural edge, with the
// link text when the row carries one.
func edgeTargets(test *testing.T, store *index.Index) []index.EdgeRow {
	test.Helper()

	rows, listErr := index.NewEdgeRepo(store).ListAll()

	if listErr != nil {
		test.Fatalf("ListAll: %v", listErr)
	}

	return rows
}

func linkEdge(source, target, link string) index.EdgeRow {
	return index.EdgeRow{
		Type:       "references",
		SourceID:   source,
		TargetID:   target,
		SourcePath: source + ".md",
		Kind:       "direct",
		Link:       link,
	}
}

func TestResolveLinks_PointsNameLinksAtTheirFile(test *testing.T) {
	store := openTestIndex(test)
	seedFiles(test, store, "inbox/today.md", "notes/auth-rfc.md", "a/dup.md", "b/dup.md")

	edges := index.NewEdgeRepo(store)

	if upsertErr := edges.UpsertAll("inbox/today", "inbox/today.md", []index.EdgeRow{
		linkEdge("inbox/today", "auth-rfc", "auth-rfc"),
		linkEdge("inbox/today", "AUTH-RFC#S1", "AUTH-RFC#S1"),
		linkEdge("inbox/today", "dup", "dup"),
		linkEdge("inbox/today", "missing", "missing"),
	}); upsertErr != nil {
		test.Fatalf("UpsertAll: %v", upsertErr)
	}

	if resolveErr := index.ResolveLinks(index.NewNodeRepo(store), edges, wikilink.CaseInsensitive); resolveErr != nil {
		test.Fatalf("ResolveLinks: %v", resolveErr)
	}

	want := []index.EdgeRow{
		linkEdge("inbox/today", "dup", "dup"),
		linkEdge("inbox/today", "missing", "missing"),
		linkEdge("inbox/today", "notes/auth-rfc", "auth-rfc"),
		linkEdge("inbox/today", "notes/auth-rfc#S1", "AUTH-RFC#S1"),
	}

	if got := edgeTargets(test, store); !reflect.DeepEqual(got, want) {
		test.Errorf("edges =\n%+v\nwant\n%+v", got, want)
	}

	// The file goes away: the link falls back to its own text and dangles.
	if deleteErr := index.NewNodeRepo(store).DeleteByPath("notes/auth-rfc.md"); deleteErr != nil {
		test.Fatalf("DeleteByPath: %v", deleteErr)
	}

	if resolveErr := index.ResolveLinks(index.NewNodeRepo(store), edges, wikilink.CaseInsensitive); resolveErr != nil {
		test.Fatalf("ResolveLinks: %v", resolveErr)
	}

	want = []index.EdgeRow{
		linkEdge("inbox/today", "AUTH-RFC#S1", "AUTH-RFC#S1"),
		linkEdge("inbox/today", "auth-rfc", "auth-rfc"),
		linkEdge("inbox/today", "dup", "dup"),
		linkEdge("inbox/today", "missing", "missing"),
	}

	if got := edgeTargets(test, store); !reflect.DeepEqual(got, want) {
		test.Errorf("after delete, edges =\n%+v\nwant\n%+v", got, want)
	}
}

// A note linked both by path and by name from the same source keeps one edge:
// the path-written one, which no later rename of the vault can move.
func TestResolveLinks_DropsANameLinkThatDuplicatesAPathLink(test *testing.T) {
	store := openTestIndex(test)
	seedFiles(test, store, "inbox/today.md", "notes/auth-rfc.md")

	edges := index.NewEdgeRepo(store)

	if upsertErr := edges.UpsertAll("inbox/today", "inbox/today.md", []index.EdgeRow{
		linkEdge("inbox/today", "auth-rfc", "auth-rfc"),
		linkEdge("inbox/today", "Auth-RFC", "Auth-RFC"),
		linkEdge("inbox/today", "notes/auth-rfc", ""),
	}); upsertErr != nil {
		test.Fatalf("UpsertAll: %v", upsertErr)
	}

	if resolveErr := index.ResolveLinks(index.NewNodeRepo(store), edges, wikilink.CaseInsensitive); resolveErr != nil {
		test.Fatalf("ResolveLinks: %v", resolveErr)
	}

	want := []index.EdgeRow{linkEdge("inbox/today", "notes/auth-rfc", "")}

	if got := edgeTargets(test, store); !reflect.DeepEqual(got, want) {
		test.Errorf("edges =\n%+v\nwant\n%+v", got, want)
	}
}

// Two name-form links that resolve to the same file keep one row between them.
func TestResolveLinks_DedupesNameLinksToTheSameFile(test *testing.T) {
	store := openTestIndex(test)
	seedFiles(test, store, "inbox/today.md", "notes/auth-rfc.md")

	edges := index.NewEdgeRepo(store)

	if upsertErr := edges.UpsertAll("inbox/today", "inbox/today.md", []index.EdgeRow{
		linkEdge("inbox/today", "auth-rfc", "auth-rfc"),
		linkEdge("inbox/today", "Auth-RFC", "Auth-RFC"),
	}); upsertErr != nil {
		test.Fatalf("UpsertAll: %v", upsertErr)
	}

	if resolveErr := index.ResolveLinks(index.NewNodeRepo(store), edges, wikilink.CaseInsensitive); resolveErr != nil {
		test.Fatalf("ResolveLinks: %v", resolveErr)
	}

	if got := edgeTargets(test, store); len(got) != 1 || got[0].TargetID != "notes/auth-rfc" {
		test.Errorf("edges = %+v, want one edge to notes/auth-rfc", got)
	}
}

// TestOpen_AddsLinkColumnsToLegacyIndex: an index written before name-form
// links gains nodes.name_key (backfilled from each file's path) and edges.link
// in place on open, with its rows intact.
func TestOpen_AddsLinkColumnsToLegacyIndex(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	first, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	seedFiles(test, first, "notes/Auth RFC.md")

	if _, execErr := first.DB().Exec(`
		DROP INDEX nodes_name_key_idx;
		DROP INDEX edges_link_idx;
		ALTER TABLE nodes DROP COLUMN name_key;
		ALTER TABLE edges DROP COLUMN link;
	`); execErr != nil {
		test.Fatalf("drop link columns: %v", execErr)
	}

	first.Close()

	for attempt := range 2 {
		reopened, reopenErr := index.Open(dbPath)

		if reopenErr != nil {
			test.Fatalf("reopen %d: %v", attempt, reopenErr)
		}

		id, _, resolveErr := index.NewNodeRepo(reopened).FileNames(wikilink.CaseInsensitive).ResolveName("auth rfc")

		if resolveErr != nil || id != "notes/Auth RFC" {
			test.Errorf("reopen %d: ResolveName = %q, %v; want the backfilled file", attempt, id, resolveErr)
		}

		reopened.Close()
	}
}
