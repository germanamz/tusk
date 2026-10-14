package index_test

import (
	"database/sql"
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/wikilink"
)

// seedSections upserts section rows under fileID, each [address, heading,
// parent address] with "" for a top-level section, keyed the way the sub-unit
// sync keys them.
func seedSections(test *testing.T, store *index.Index, fileID string, sections ...[3]string) {
	test.Helper()

	rows := make([]index.NodeRow, 0, len(sections))

	for position, section := range sections {
		parent := fileID

		if section[2] != "" {
			parent = fileID + "#" + section[2]
		}

		rows = append(rows, index.NodeRow{
			ID: fileID + "#" + section[0], Type: "section", Path: fileID + ".md", Title: section[1],
			PropertiesJSON: "{}", LastChecksum: "x",
			ParentID: sql.NullString{String: parent, Valid: true},
			Ordinal:  sql.NullInt64{Int64: int64(position), Valid: true},
			NameKey:  sql.NullString{String: wikilink.HeadingKey(section[1]), Valid: true},
		})
	}

	if upsertErr := index.NewNodeRepo(store).BulkUpsert(rows, "markdown"); upsertErr != nil {
		test.Fatalf("seed sections of %s: %v", fileID, upsertErr)
	}
}

func resolveLinksOnce(test *testing.T, store *index.Index) int {
	test.Helper()

	names, namesErr := index.NewNodeRepo(store).LinkNames(wikilink.CaseInsensitive)

	if namesErr != nil {
		test.Fatalf("LinkNames: %v", namesErr)
	}

	changed, resolveErr := index.NewEdgeRepo(store).ResolveLinks(names)

	if resolveErr != nil {
		test.Fatalf("ResolveLinks: %v", resolveErr)
	}

	return changed
}

// A heading fragment resolves to its section in every link form: by name,
// by path, as a chain, and with no path part from the linking file's own
// sub-unit (#808).
func TestResolveLinks_HeadingFragmentsFindTheirSection(test *testing.T) {
	store := openTestIndex(test)
	seedFiles(test, store, "inbox/today.md", "notes/auth-rfc.md")
	seedSections(test, store, "notes/auth-rfc", [3]string{"S1", "Overview", ""}, [3]string{"S1.1", "Tokens", "S1"})
	seedSections(test, store, "inbox/today", [3]string{"S1", "Agenda", ""})

	edges := index.NewEdgeRepo(store)

	if upsertErr := edges.UpsertAll("inbox/today", "inbox/today.md", []index.EdgeRow{
		linkEdge("inbox/today", "auth-rfc#overview", "auth-rfc#overview"),
		linkEdge("inbox/today", "notes/auth-rfc#Overview#Tokens", "notes/auth-rfc#Overview#Tokens"),
	}); upsertErr != nil {
		test.Fatalf("UpsertAll: %v", upsertErr)
	}

	if upsertErr := edges.UpsertAll("inbox/today#S1", "inbox/today.md", []index.EdgeRow{
		{Type: "references", SourceID: "inbox/today#S1", TargetID: "#Agenda", SourcePath: "inbox/today.md", Kind: "direct", Link: "#Agenda"},
	}); upsertErr != nil {
		test.Fatalf("UpsertAll: %v", upsertErr)
	}

	if changed := resolveLinksOnce(test, store); changed != 3 {
		test.Errorf("ResolveLinks changed %d rows, want 3", changed)
	}

	want := []index.EdgeRow{
		linkEdge("inbox/today", "notes/auth-rfc#S1", "auth-rfc#overview"),
		linkEdge("inbox/today", "notes/auth-rfc#S1.1", "notes/auth-rfc#Overview#Tokens"),
		{Type: "references", SourceID: "inbox/today#S1", TargetID: "inbox/today#S1", SourcePath: "inbox/today.md", Kind: "direct", Link: "#Agenda"},
	}

	if got := edgeTargets(test, store); !reflect.DeepEqual(got, want) {
		test.Errorf("edges =\n%+v\nwant\n%+v", got, want)
	}

	if changed := resolveLinksOnce(test, store); changed != 0 {
		test.Errorf("a second pass changed %d rows, want none", changed)
	}
}

// A heading link that misses lands on its note, unless its source links the
// note already: then it waits at its fragment rather than doubling the edge,
// and is neither deleted nor moved again while the twin stays. When the twin
// goes, it lands on the note in the same pass.
func TestResolveLinks_AMissedHeadingParksBesideItsTwin(test *testing.T) {
	store := openTestIndex(test)
	seedFiles(test, store, "inbox/today.md", "notes/auth-rfc.md")
	seedSections(test, store, "notes/auth-rfc", [3]string{"S1", "Overview", ""})

	edges := index.NewEdgeRepo(store)

	if upsertErr := edges.UpsertAll("inbox/today", "inbox/today.md", []index.EdgeRow{
		linkEdge("inbox/today", "auth-rfc#Renamed", "auth-rfc#Renamed"),
		linkEdge("inbox/today", "auth-rfc", "auth-rfc"),
		linkEdge("inbox/today", "notes/auth-rfc", ""),
	}); upsertErr != nil {
		test.Fatalf("UpsertAll: %v", upsertErr)
	}

	resolveLinksOnce(test, store)

	// The path-written row wins over the name twin, which is deleted; the
	// missed heading parks rather than being deleted with it.
	want := []index.EdgeRow{
		linkEdge("inbox/today", "notes/auth-rfc", ""),
		linkEdge("inbox/today", "notes/auth-rfc#Renamed", "auth-rfc#Renamed"),
	}

	if got := edgeTargets(test, store); !reflect.DeepEqual(got, want) {
		test.Errorf("edges =\n%+v\nwant\n%+v", got, want)
	}

	if changed := resolveLinksOnce(test, store); changed != 0 {
		test.Errorf("a second pass changed %d rows, want the parked row left alone", changed)
	}

	// The heading comes back under its new name.
	seedSections(test, store, "notes/auth-rfc", [3]string{"S1", "Overview", ""}, [3]string{"S2", "Renamed", ""})
	resolveLinksOnce(test, store)

	want = []index.EdgeRow{
		linkEdge("inbox/today", "notes/auth-rfc", ""),
		linkEdge("inbox/today", "notes/auth-rfc#S2", "auth-rfc#Renamed"),
	}

	if got := edgeTargets(test, store); !reflect.DeepEqual(got, want) {
		test.Errorf("after the heading returns, edges =\n%+v\nwant\n%+v", got, want)
	}
}

func TestResolveLinks_AMissedHeadingLandsOnTheNoteWhenItsTwinGoes(test *testing.T) {
	store := openTestIndex(test)
	seedFiles(test, store, "inbox/today.md", "notes/auth-rfc.md")

	edges := index.NewEdgeRepo(store)

	if upsertErr := edges.UpsertAll("inbox/today", "inbox/today.md", []index.EdgeRow{
		linkEdge("inbox/today", "auth-rfc", "auth-rfc"),
		linkEdge("inbox/today", "notes/auth-rfc#Renamed", "notes/auth-rfc#Renamed"),
	}); upsertErr != nil {
		test.Fatalf("UpsertAll: %v", upsertErr)
	}

	resolveLinksOnce(test, store)

	if got := edgeTargets(test, store); got[1].TargetID != "notes/auth-rfc#Renamed" {
		test.Fatalf("edges = %+v, want the heading link parked beside [[auth-rfc]]", got)
	}

	// A second auth-rfc makes the plain name ambiguous: it leaves the note,
	// and the parked heading link takes the note over in the same pass.
	seedFiles(test, store, "archive/auth-rfc.md")
	resolveLinksOnce(test, store)

	want := []index.EdgeRow{
		linkEdge("inbox/today", "auth-rfc", "auth-rfc"),
		linkEdge("inbox/today", "notes/auth-rfc", "notes/auth-rfc#Renamed"),
	}

	if got := edgeTargets(test, store); !reflect.DeepEqual(got, want) {
		test.Errorf("edges =\n%+v\nwant\n%+v", got, want)
	}
}

// A move re-points a path-written heading link's text along with its target,
// so ResolveLinks finds the heading in the moved note; a name-form link's text
// is the rewriter's, and a root note's link reads like a name.
func TestEdgeRepo_RetargetEdges_MovesPathFormLinkText(test *testing.T) {
	store := openTestIndex(test)
	seedFiles(test, store, "inbox/today.md", "notes/new.md", "archive/root.md")

	edges := index.NewEdgeRepo(store)

	if upsertErr := edges.UpsertAll("inbox/today", "inbox/today.md", []index.EdgeRow{
		linkEdge("inbox/today", "notes/old#S1", "notes/old#Setup"),
		linkEdge("inbox/today", "notes/old", "old#Missing"),
		linkEdge("inbox/today", "root#S1", "root#Setup"),
	}); upsertErr != nil {
		test.Fatalf("UpsertAll: %v", upsertErr)
	}

	if retargetErr := edges.RetargetEdges("notes/old", "notes/new"); retargetErr != nil {
		test.Fatalf("RetargetEdges: %v", retargetErr)
	}

	if retargetErr := edges.RetargetEdges("root", "archive/root"); retargetErr != nil {
		test.Fatalf("RetargetEdges: %v", retargetErr)
	}

	want := []index.EdgeRow{
		linkEdge("inbox/today", "archive/root#S1", "root#Setup"),
		linkEdge("inbox/today", "notes/new", "old#Missing"),
		linkEdge("inbox/today", "notes/new#S1", "notes/new#Setup"),
	}

	if got := edgeTargets(test, store); !reflect.DeepEqual(got, want) {
		test.Errorf("edges =\n%+v\nwant\n%+v", got, want)
	}
}

// A name rewritten to a full id stays looked up while it carries a fragment.
func TestEdgeRepo_RenameLink_KeepsAFragmentLookedUp(test *testing.T) {
	store := openTestIndex(test)
	seedFiles(test, store, "inbox/today.md")

	edges := index.NewEdgeRepo(store)

	if upsertErr := edges.UpsertAll("inbox/today", "inbox/today.md", []index.EdgeRow{
		linkEdge("inbox/today", "notes/old", "old"),
		linkEdge("inbox/today", "notes/old#S1", "old#Setup"),
	}); upsertErr != nil {
		test.Fatalf("UpsertAll: %v", upsertErr)
	}

	if renameErr := edges.RenameLink("inbox/today.md", "old", "archive/old"); renameErr != nil {
		test.Fatalf("RenameLink: %v", renameErr)
	}

	want := []index.EdgeRow{
		linkEdge("inbox/today", "archive/old", ""),
		linkEdge("inbox/today", "archive/old#Setup", "archive/old#Setup"),
	}

	if got := edgeTargets(test, store); !reflect.DeepEqual(got, want) {
		test.Errorf("edges =\n%+v\nwant\n%+v", got, want)
	}
}

// A bare trailing `#` names the note itself, so [[auth-rfc#]] beside
// [[auth-rfc]] is a plain twin: one row survives, and a later pass changes
// nothing rather than parking it at a dangling auth-rfc#.
func TestResolveLinks_ABareHashIsNoFragment(test *testing.T) {
	store := openTestIndex(test)
	seedFiles(test, store, "inbox/today.md", "notes/auth-rfc.md")

	edges := index.NewEdgeRepo(store)

	if upsertErr := edges.UpsertAll("inbox/today", "inbox/today.md", []index.EdgeRow{
		linkEdge("inbox/today", "auth-rfc", "auth-rfc"),
		linkEdge("inbox/today", "auth-rfc#", "auth-rfc#"),
	}); upsertErr != nil {
		test.Fatalf("UpsertAll: %v", upsertErr)
	}

	resolveLinksOnce(test, store)

	if got := edgeTargets(test, store); len(got) != 1 || got[0].TargetID != "notes/auth-rfc" {
		test.Errorf("edges = %+v, want one edge to notes/auth-rfc", got)
	}

	if changed := resolveLinksOnce(test, store); changed != 0 {
		test.Errorf("a second pass changed %d rows, want none", changed)
	}
}
