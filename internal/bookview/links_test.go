package bookview

import (
	"database/sql"
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

// subUnit builds a sub-unit NodeRow under parentID, the shape subunit sync
// writes: the id is "<fileID>#<address>" and Path is the file's, because a
// section has no file of its own.
//
// parentID is the ENCLOSING row, and it is the file id only for units at the
// document root. A unit nested inside a section carries that section's id
// instead — subunit sync's parentRowID returns the file id only when the unit's
// ParentAddress is empty, and internal/subunit/ast.go states the rule outright:
// "Sub-units of sub-units (paragraphs under a section, an H3 section under an H2)
// reference the closest enclosing section."
//
// So "c#S1" is a legitimate parentID here, and it is the common one: prose lives
// under headings. Fixtures that only ever pass a file id model the document-root
// case and nothing else. That gap is what hid the nested-rollup bug these tests
// now cover — the doubles agreed with the code instead of with the indexer.
func subUnit(id, parentID, nodeType, title, path string) index.NodeRow {
	return index.NodeRow{
		ID:       id,
		Type:     nodeType,
		Title:    title,
		Path:     path,
		ParentID: sql.NullString{String: parentID, Valid: true},
	}
}

// containsEdge builds the structural contains edge subunit sync materializes
// from a file to one of its sections. Kind is "structural" and Source is
// "markdown", which the schema's CHECK requires of every structural row.
func containsEdge(parentID, childID, path string) index.EdgeRow {
	return index.EdgeRow{
		Type:       "contains",
		SourceID:   parentID,
		TargetID:   childID,
		SourcePath: path,
		Kind:       "structural",
		Source:     sql.NullString{String: "markdown", Valid: true},
	}
}

// linksFor drives linksOf directly. The rails are the projection under test, so
// these tests read them at the source rather than through handleNode's JSON —
// node_test.go already pins the wire shape.
func linksFor(test *testing.T, srv *Server, nodeID string) nodeLinks {
	test.Helper()

	links, linksErr := srv.linksOf(nodeID)

	if linksErr != nil {
		test.Fatalf("linksOf(%q): %v", nodeID, linksErr)
	}

	return links
}

// TestLinksRollUpSubUnitSource pins the whole point of the rollup: a link
// authored in a section of c is a link from note c. The edge row is
// "c#S1 → a", so the file-level rule (drop any neighbor whose far end is a
// sub-unit) drops it and a's Backlinks rail silently reports nothing — the
// failure mode that reads as "working". It must surface, and as c, not c#S1:
// c#S1 is not what a reader means by "what links here", and the rail entry has
// to be navigable.
func TestLinksRollUpSubUnitSource(test *testing.T) {
	nodes := fakeNodes{
		file: []index.NodeRow{
			{ID: "a", Type: "note", Title: "A", Path: "a.md"},
			{ID: "c", Type: "spec", Title: "C", Path: "c.md"},
		},
		sub: []index.NodeRow{subUnit("c#S1", "c", "spec", "Section 1", "c.md")},
	}

	edges := fakeEdges{all: []index.EdgeRow{
		containsEdge("c", "c#S1", "c.md"),
		{Type: "references", SourceID: "c#S1", TargetID: "a", SourcePath: "c.md", Kind: "direct"},
	}}

	links := linksFor(test, New(Deps{Nodes: nodes, Edges: edges}), "a")

	want := []LinkRef{{ID: "c", Title: "C", Type: "spec", EdgeType: "references"}}

	if !reflect.DeepEqual(links.in, want) {
		test.Fatalf("links.in=%+v want %+v — the sub-unit source must roll up to its file", links.in, want)
	}

	if len(links.out) != 0 {
		test.Fatalf("links.out=%+v want none", links.out)
	}
}

// TestLinksRollUpNestedSubUnitSource is the ruling's motivating scenario, and
// the case the first cut of the rollup got wrong: the link is authored in a
// PARAGRAPH under a section, not in the section itself. That paragraph's
// parent_id is "c#S1" — another sub-unit — so resolving one parent hop lands on
// the section and stops there, surfacing "c#S1": not the file the ruling
// requires, not navigable, and not de-duplicable against c's other sections.
//
// This is the common shape, not an exotic one. Prose lives under headings, so
// most real backlinks arrive through a nested unit.
func TestLinksRollUpNestedSubUnitSource(test *testing.T) {
	nodes := fakeNodes{
		file: []index.NodeRow{
			{ID: "a", Type: "note", Title: "A", Path: "a.md"},
			{ID: "c", Type: "spec", Title: "C", Path: "c.md"},
		},
		sub: []index.NodeRow{
			subUnit("c#S1", "c", "spec", "Section 1", "c.md"),
			// The paragraph's parent is the SECTION, not the file.
			subUnit("c#S1P1", "c#S1", "spec", "", "c.md"),
		},
	}

	edges := fakeEdges{all: []index.EdgeRow{
		containsEdge("c", "c#S1", "c.md"),
		containsEdge("c#S1", "c#S1P1", "c.md"),
		{Type: "references", SourceID: "c#S1P1", TargetID: "a", SourcePath: "c.md", Kind: "direct"},
	}}

	links := linksFor(test, New(Deps{Nodes: nodes, Edges: edges}), "a")

	want := []LinkRef{{ID: "c", Title: "C", Type: "spec", EdgeType: "references"}}

	if !reflect.DeepEqual(links.in, want) {
		test.Fatalf("links.in=%+v want %+v — a nested sub-unit must roll up to its FILE, not to its enclosing section", links.in, want)
	}
}

// TestLinksRollUpDeeplyNestedSubUnit pins that the rollup has no depth limit. A
// paragraph under an H3 under an H2 is three rows from its file, so any
// fixed-depth resolution (one hop, or two) surfaces an intermediate section for
// some real document. Projecting the id rather than walking parent_id is what
// makes depth irrelevant.
func TestLinksRollUpDeeplyNestedSubUnit(test *testing.T) {
	nodes := fakeNodes{
		file: []index.NodeRow{
			{ID: "a", Type: "note", Title: "A", Path: "a.md"},
			{ID: "c", Type: "spec", Title: "C", Path: "c.md"},
		},
		// The full chain, as subunit sync would write it: each row's parent is
		// the closest enclosing section, and only the outermost points at c.
		sub: []index.NodeRow{
			subUnit("c#S1", "c", "spec", "Section 1", "c.md"),
			subUnit("c#S1S2", "c#S1", "spec", "Subsection 2", "c.md"),
			subUnit("c#S1S2P3", "c#S1S2", "spec", "", "c.md"),
		},
	}

	edges := fakeEdges{all: []index.EdgeRow{
		{Type: "references", SourceID: "c#S1S2P3", TargetID: "a", SourcePath: "c.md", Kind: "direct"},
	}}

	links := linksFor(test, New(Deps{Nodes: nodes, Edges: edges}), "a")

	want := []LinkRef{{ID: "c", Title: "C", Type: "spec", EdgeType: "references"}}

	if !reflect.DeepEqual(links.in, want) {
		test.Fatalf("links.in=%+v want %+v — rollup must reach the file from any nesting depth", links.in, want)
	}
}

// TestLinksDeduplicateNestedSubUnitsAcrossSections pins de-duplication where it
// actually has to work. The existing dedup test links from the sections
// themselves, whose parents are all c, so a one-hop rollup collapses them
// correctly by accident. Here the links come from paragraphs in two DIFFERENT
// sections: one hop lands them on c#S1 and c#S2 — two distinct refs, no
// collision, two rail entries. Only a rollup that reaches the file gives the one
// c entry the ruling requires.
func TestLinksDeduplicateNestedSubUnitsAcrossSections(test *testing.T) {
	nodes := fakeNodes{
		file: []index.NodeRow{
			{ID: "a", Type: "note", Title: "A", Path: "a.md"},
			{ID: "c", Type: "spec", Title: "C", Path: "c.md"},
		},
		sub: []index.NodeRow{
			subUnit("c#S1", "c", "spec", "Section 1", "c.md"),
			subUnit("c#S1P1", "c#S1", "spec", "", "c.md"),
			subUnit("c#S2", "c", "spec", "Section 2", "c.md"),
			subUnit("c#S2P1", "c#S2", "spec", "", "c.md"),
		},
	}

	edges := fakeEdges{all: []index.EdgeRow{
		{Type: "references", SourceID: "c#S1P1", TargetID: "a", SourcePath: "c.md", Kind: "direct"},
		{Type: "references", SourceID: "c#S2P1", TargetID: "a", SourcePath: "c.md", Kind: "direct"},
	}}

	links := linksFor(test, New(Deps{Nodes: nodes, Edges: edges}), "a")

	want := []LinkRef{{ID: "c", Title: "C", Type: "spec", EdgeType: "references"}}

	if !reflect.DeepEqual(links.in, want) {
		test.Fatalf("links.in=%+v want exactly one entry, %+v — paragraphs in two sections are still one link from c", links.in, want)
	}
}

// TestLinksSkipRolledUpNestedSelfReference pins the self-reference skip against
// the nested shape, which is the one that defeats it when rollup stops short. The
// focus's own paragraph a#S1P1 links back to a; rolled up to the file it equals
// the focus and the guard drops it. Resolved one hop it becomes a#S1 — not equal
// to the focus, so the guard misses and a's own section appears in a's own rail,
// which the ruling puts explicitly out of scope.
func TestLinksSkipRolledUpNestedSelfReference(test *testing.T) {
	nodes := fakeNodes{
		file: []index.NodeRow{
			{ID: "a", Type: "note", Title: "A", Path: "a.md"},
			{ID: "b", Type: "note", Title: "B", Path: "b.md"},
		},
		sub: []index.NodeRow{
			subUnit("a#S1", "a", "note", "Section 1", "a.md"),
			subUnit("a#S1P1", "a#S1", "note", "", "a.md"),
		},
	}

	edges := fakeEdges{all: []index.EdgeRow{
		// A paragraph inside a section of a references a itself.
		{Type: "references", SourceID: "a#S1P1", TargetID: "a", SourcePath: "a.md", Kind: "direct"},
		// A real outbound link, to prove the skip is targeted.
		{Type: "references", SourceID: "a", TargetID: "b", SourcePath: "a.md", Kind: "derived"},
	}}

	links := linksFor(test, New(Deps{Nodes: nodes, Edges: edges}), "a")

	if len(links.in) != 0 {
		test.Fatalf("links.in=%+v want a's own nested sub-unit not rolled up into a self-backlink", links.in)
	}

	want := []LinkRef{{ID: "b", Title: "B", Type: "note", EdgeType: "references"}}

	if !reflect.DeepEqual(links.out, want) {
		test.Fatalf("links.out=%+v want %+v", links.out, want)
	}
}

// TestLinksDeduplicateRolledUpSource pins de-duplication. Rolling up makes
// collisions reachable that the file-level rule made impossible: three sections
// of c linking to a are three distinct edge rows but one link from c. Emitting
// c three times would put three byte-identical entries in the rail, which is
// noise a reader cannot act on.
func TestLinksDeduplicateRolledUpSource(test *testing.T) {
	nodes := fakeNodes{
		file: []index.NodeRow{
			{ID: "a", Type: "note", Title: "A", Path: "a.md"},
			{ID: "c", Type: "spec", Title: "C", Path: "c.md"},
		},
		sub: []index.NodeRow{
			subUnit("c#S1", "c", "spec", "Section 1", "c.md"),
			subUnit("c#S2", "c", "spec", "Section 2", "c.md"),
			subUnit("c#S3", "c", "spec", "Section 3", "c.md"),
		},
	}

	edges := fakeEdges{all: []index.EdgeRow{
		{Type: "references", SourceID: "c#S1", TargetID: "a", SourcePath: "c.md", Kind: "direct"},
		{Type: "references", SourceID: "c#S2", TargetID: "a", SourcePath: "c.md", Kind: "direct"},
		{Type: "references", SourceID: "c#S3", TargetID: "a", SourcePath: "c.md", Kind: "direct"},
	}}

	links := linksFor(test, New(Deps{Nodes: nodes, Edges: edges}), "a")

	want := []LinkRef{{ID: "c", Title: "C", Type: "spec", EdgeType: "references"}}

	if !reflect.DeepEqual(links.in, want) {
		test.Fatalf("links.in=%+v want exactly one entry, %+v", links.in, want)
	}
}

// TestLinksKeepDistinctEdgeTypesFromSameFile guards the de-duplication against
// over-collapsing. Two sections of c reaching a by different edge types are two
// different statements about a, and the rail shows the edge type — collapsing
// them on the far-end id alone would lose one. De-duplication keys on the whole
// LinkRef, so only entries that are identical on the wire merge.
func TestLinksKeepDistinctEdgeTypesFromSameFile(test *testing.T) {
	nodes := fakeNodes{
		file: []index.NodeRow{
			{ID: "a", Type: "note", Title: "A", Path: "a.md"},
			{ID: "c", Type: "spec", Title: "C", Path: "c.md"},
		},
		sub: []index.NodeRow{
			subUnit("c#S1", "c", "spec", "Section 1", "c.md"),
			subUnit("c#S2", "c", "spec", "Section 2", "c.md"),
		},
	}

	edges := fakeEdges{all: []index.EdgeRow{
		{Type: "references", SourceID: "c#S1", TargetID: "a", SourcePath: "c.md", Kind: "direct"},
		{Type: "depends-on", SourceID: "c#S2", TargetID: "a", SourcePath: "c.md", Kind: "direct"},
	}}

	links := linksFor(test, New(Deps{Nodes: nodes, Edges: edges}), "a")

	// (source_id, type, target_id): "c#S1" sorts before "c#S2".
	want := []LinkRef{
		{ID: "c", Title: "C", Type: "spec", EdgeType: "references"},
		{ID: "c", Title: "C", Type: "spec", EdgeType: "depends-on"},
	}

	if !reflect.DeepEqual(links.in, want) {
		test.Fatalf("links.in=%+v want both edge types kept, %+v", links.in, want)
	}
}

// TestLinksRollUpSubUnitTarget pins the other direction: an out-link to
// [[b#S1]] has the identical far-end drop, so it rolls up to b. Without this a
// note that deep-links a section of another note shows an empty Links rail.
func TestLinksRollUpSubUnitTarget(test *testing.T) {
	nodes := fakeNodes{
		file: []index.NodeRow{
			{ID: "a", Type: "note", Title: "A", Path: "a.md"},
			{ID: "b", Type: "note", Title: "B", Path: "b.md"},
		},
		sub: []index.NodeRow{subUnit("b#S1", "b", "note", "Section 1", "b.md")},
	}

	edges := fakeEdges{all: []index.EdgeRow{
		containsEdge("b", "b#S1", "b.md"),
		{Type: "references", SourceID: "a", TargetID: "b#S1", SourcePath: "a.md", Kind: "derived"},
	}}

	links := linksFor(test, New(Deps{Nodes: nodes, Edges: edges}), "a")

	want := []LinkRef{{ID: "b", Title: "B", Type: "note", EdgeType: "references"}}

	if !reflect.DeepEqual(links.out, want) {
		test.Fatalf("links.out=%+v want the sub-unit target rolled up to its file, %+v", links.out, want)
	}
}

// TestLinksFileLevelUnchanged pins the no-regression case: a vault with no
// sub-unit indexing must project exactly as it did before the rollup landed,
// including ListAll's global (source_id, type, target_id) order across the two
// rails — neither ListBySource's (type, target_id) nor ListByTarget's
// (type, source_id) reproduces it.
func TestLinksFileLevelUnchanged(test *testing.T) {
	nodes := fakeNodes{file: []index.NodeRow{
		{ID: "a", Type: "note", Title: "A", Path: "a.md"},
		{ID: "b", Type: "note", Title: "B", Path: "b.md"},
		{ID: "c", Type: "spec", Title: "C", Path: "c.md"},
		{ID: "d", Type: "note", Title: "D", Path: "d.md"},
		{ID: "e", Type: "note", Title: "E", Path: "e.md"},
	}}

	edges := fakeEdges{all: []index.EdgeRow{
		{Type: "references", SourceID: "a", TargetID: "d", SourcePath: "a.md", Kind: "derived"},
		{Type: "references", SourceID: "a", TargetID: "b", SourcePath: "a.md", Kind: "derived"},
		{Type: "depends-on", SourceID: "a", TargetID: "d", SourcePath: "a.md", Kind: "direct"},
		{Type: "references", SourceID: "c", TargetID: "a", SourcePath: "c.md", Kind: "direct"},
		{Type: "depends-on", SourceID: "e", TargetID: "a", SourcePath: "e.md", Kind: "direct"},
	}}

	links := linksFor(test, New(Deps{Nodes: nodes, Edges: edges}), "a")

	// Sorted by (source_id, type, target_id): depends-on→d, then references→b,
	// then references→d. All share source_id "a", so type breaks the tie first.
	wantOut := []LinkRef{
		{ID: "d", Title: "D", Type: "note", EdgeType: "depends-on"},
		{ID: "b", Title: "B", Type: "note", EdgeType: "references"},
		{ID: "d", Title: "D", Type: "note", EdgeType: "references"},
	}

	if !reflect.DeepEqual(links.out, wantOut) {
		test.Fatalf("links.out=%+v want %+v", links.out, wantOut)
	}

	// The in rail is where the global order is actually observable: every out
	// edge shares source_id "a", so ListBySource's own (type, target_id) order
	// already matches. These two are deliberately chosen so the two orders
	// disagree — ListByTarget's (type, source_id) yields e then c, while
	// ListAll's (source_id, type, target_id) yields c then e.
	wantIn := []LinkRef{
		{ID: "c", Title: "C", Type: "spec", EdgeType: "references"},
		{ID: "e", Title: "E", Type: "note", EdgeType: "depends-on"},
	}

	if !reflect.DeepEqual(links.in, wantIn) {
		test.Fatalf("links.in=%+v want ListAll's global order, %+v", links.in, wantIn)
	}
}

// TestLinksExcludeStructuralEdges pins the view policy from both ends: a
// structural "contains" edge is index plumbing, not a link a reader follows, and
// containment is what the Contents pane expresses.
//
// The rollup makes this filter load-bearing where it was previously incidental.
// Before, a file focus's own contains edge pointed at a sub-unit and the
// file-level drop removed it for free. Now that sub-unit rolls back up to the
// focus itself, so the Kind filter is the only thing standing between the reader
// and a note that links to itself once per section.
func TestLinksExcludeStructuralEdges(test *testing.T) {
	nodes := fakeNodes{
		file: []index.NodeRow{
			{ID: "a", Type: "note", Title: "A", Path: "a.md"},
			{ID: "b", Type: "note", Title: "B", Path: "b.md"},
		},
		sub: []index.NodeRow{subUnit("a#section", "a", "note", "Section", "a.md")},
	}

	edges := fakeEdges{all: []index.EdgeRow{
		containsEdge("a", "a#section", "a.md"),
		{Type: "references", SourceID: "a#section", TargetID: "b", SourcePath: "a.md", Kind: "derived"},
	}}

	srv := New(Deps{Nodes: nodes, Edges: edges})

	// The file focus: its contains edge now resolves to a file-level far end
	// (itself), so only the Kind filter keeps it out.
	fileLinks := linksFor(test, srv, "a")

	if len(fileLinks.out) != 0 || len(fileLinks.in) != 0 {
		test.Fatalf("file links out=%+v in=%+v, want the structural contains excluded", fileLinks.out, fileLinks.in)
	}

	// The sub-unit focus: the parent's contains edge arrives as an incoming
	// link from a real file, and the Kind filter is again the only exclusion.
	unitLinks := linksFor(test, srv, "a#section")

	if len(unitLinks.in) != 0 {
		test.Fatalf("sub-unit links.in=%+v, want the structural contains excluded", unitLinks.in)
	}

	wantOut := []LinkRef{{ID: "b", Title: "B", Type: "note", EdgeType: "references"}}

	if !reflect.DeepEqual(unitLinks.out, wantOut) {
		test.Fatalf("sub-unit links.out=%+v want the derived reference kept, %+v", unitLinks.out, wantOut)
	}
}

// TestLinksKeepUserAuthoredContains pins the distinction the structural filter
// rests on: it filters Kind, not Type. A user who writes `contains: [[b]]` in
// frontmatter authored a link, and it lands as Kind "direct" — filtering on
// Type == "contains" would silently swallow it as though it were plumbing.
func TestLinksKeepUserAuthoredContains(test *testing.T) {
	nodes := fakeNodes{file: []index.NodeRow{
		{ID: "a", Type: "note", Title: "A", Path: "a.md"},
		{ID: "b", Type: "note", Title: "B", Path: "b.md"},
	}}

	edges := fakeEdges{all: []index.EdgeRow{
		{Type: "contains", SourceID: "a", TargetID: "b", SourcePath: "a.md", Kind: "direct"},
	}}

	links := linksFor(test, New(Deps{Nodes: nodes, Edges: edges}), "a")

	want := []LinkRef{{ID: "b", Title: "B", Type: "note", EdgeType: "contains"}}

	if !reflect.DeepEqual(links.out, want) {
		test.Fatalf("links.out=%+v want the user-authored contains kept, %+v", links.out, want)
	}
}

// TestLinksSkipSubUnitWithMissingParent pins the dangling-parent choice: a
// sub-unit whose file row is gone from the index has nothing navigable behind
// it, so it is skipped. Emitting it would put a zero-value entry in the rail —
// a blank title linking to an id that 404s — which is worse than the omission.
//
// The rails detect this by looking up the file id the sub-unit id names ("c" for
// "c#S1") and finding no row, rather than by chasing a parent pointer. The
// observable contract is the same and this test still pins it; what changed is
// that the check now costs nothing beyond the lookup the rollup already makes.
func TestLinksSkipSubUnitWithMissingParent(test *testing.T) {
	nodes := fakeNodes{
		file: []index.NodeRow{{ID: "a", Type: "note", Title: "A", Path: "a.md"}},
		// c#S1's parent file row "c" is absent, as it would be mid-reindex
		// after c was deleted but its sub-unit rows not yet reaped.
		sub: []index.NodeRow{subUnit("c#S1", "c", "spec", "Section 1", "c.md")},
	}

	edges := fakeEdges{all: []index.EdgeRow{
		{Type: "references", SourceID: "c#S1", TargetID: "a", SourcePath: "c.md", Kind: "direct"},
	}}

	links := linksFor(test, New(Deps{Nodes: nodes, Edges: edges}), "a")

	if len(links.in) != 0 {
		test.Fatalf("links.in=%+v want the orphaned sub-unit skipped, not emitted as a zero-value entry", links.in)
	}
}

// TestLinksRollUpSourceWithoutSubUnitRow pins the authority question nothing
// else in this file covers: the rollup verifies the far end's FILE row, never
// the far SUB-UNIT row. A stale "c#S1 → a" whose c#S1 row was reaped but whose
// file c survives still emits c.
//
// That is deliberate, and the inverse of TestLinksSkipSubUnitWithMissingParent:
// there the file was gone, leaving nothing navigable behind the link. Here the
// edge row is the authority for "a link was authored", and c is navigable and
// carries a title, so the entry is not the dead, zero-value one that test
// guards against. It is also what keeps the rails correct where sub-unit rows
// do not exist to verify at all — a workspace with sub-unit indexing off.
//
// The state is unreachable in a consistent index (subunit sync rewrites rows
// and derived edges in one pass), which is precisely why it needs pinning:
// nothing else here would catch a change to it.
func TestLinksRollUpSourceWithoutSubUnitRow(test *testing.T) {
	nodes := fakeNodes{
		file: []index.NodeRow{
			{ID: "a", Type: "note", Title: "A", Path: "a.md"},
			{ID: "c", Type: "spec", Title: "C", Path: "c.md"},
		},
		// No c#S1 row: reaped, or never written because the workspace does not
		// index sub-units.
	}

	edges := fakeEdges{all: []index.EdgeRow{
		{Type: "references", SourceID: "c#S1", TargetID: "a", SourcePath: "c.md", Kind: "direct"},
	}}

	links := linksFor(test, New(Deps{Nodes: nodes, Edges: edges}), "a")

	want := []LinkRef{{ID: "c", Title: "C", Type: "spec", EdgeType: "references"}}

	if !reflect.DeepEqual(links.in, want) {
		test.Fatalf("links.in=%+v want %+v — the rollup verifies the far FILE row, not the far sub-unit row", links.in, want)
	}
}

// TestLinksSkipDanglingFarEnd re-pins a contract the traversal inherited: an
// edge whose far end has no row at all is skipped. ListByIDs omits missing ids
// silently rather than erroring, so nothing but this check stands between a
// stale edge and an empty rail entry.
func TestLinksSkipDanglingFarEnd(test *testing.T) {
	nodes := fakeNodes{file: []index.NodeRow{{ID: "a", Type: "note", Title: "A", Path: "a.md"}}}

	edges := fakeEdges{all: []index.EdgeRow{
		{Type: "references", SourceID: "a", TargetID: "gone", SourcePath: "a.md", Kind: "derived"},
		{Type: "references", SourceID: "vanished", TargetID: "a", SourcePath: "x.md", Kind: "derived"},
	}}

	links := linksFor(test, New(Deps{Nodes: nodes, Edges: edges}), "a")

	if len(links.out) != 0 || len(links.in) != 0 {
		test.Fatalf("out=%+v in=%+v, want both dangling ends skipped", links.out, links.in)
	}
}

// TestLinksSelfLoopEmittedOnceAsOut re-pins a contract the traversal inherited:
// a genuine file-level self-loop comes back from both ListBySource and
// ListByTarget, and is emitted exactly once, as "out". The rollup's own
// self-reference skip must not touch it — nothing was rolled up, so the edge is
// a real link the author wrote.
func TestLinksSelfLoopEmittedOnceAsOut(test *testing.T) {
	nodes := fakeNodes{file: []index.NodeRow{{ID: "a", Type: "note", Title: "A", Path: "a.md"}}}

	edges := fakeEdges{all: []index.EdgeRow{
		{Type: "references", SourceID: "a", TargetID: "a", SourcePath: "a.md", Kind: "derived"},
	}}

	links := linksFor(test, New(Deps{Nodes: nodes, Edges: edges}), "a")

	want := []LinkRef{{ID: "a", Title: "A", Type: "note", EdgeType: "references"}}

	if !reflect.DeepEqual(links.out, want) {
		test.Fatalf("links.out=%+v want the self-loop once as out, %+v", links.out, want)
	}

	if len(links.in) != 0 {
		test.Fatalf("links.in=%+v want the self-loop counted only as out", links.in)
	}
}

// TestLinksSkipRolledUpSelfReference pins the case rollup creates from nothing:
// a section of a linking back to a. The far end a#S1 rolls up onto the focus
// itself, so emitting it would invent a self-link the file-level rails never
// had — and on the "in" side it would contradict the rule the test above pins,
// that a self-loop appears only as "out". The focus node's own sub-units are a
// separate affordance; this projection is about the far end.
func TestLinksSkipRolledUpSelfReference(test *testing.T) {
	nodes := fakeNodes{
		file: []index.NodeRow{
			{ID: "a", Type: "note", Title: "A", Path: "a.md"},
			{ID: "b", Type: "note", Title: "B", Path: "b.md"},
		},
		sub: []index.NodeRow{subUnit("a#S1", "a", "note", "Section 1", "a.md")},
	}

	edges := fakeEdges{all: []index.EdgeRow{
		// A section of a references a itself.
		{Type: "references", SourceID: "a#S1", TargetID: "a", SourcePath: "a.md", Kind: "direct"},
		// A real outbound link, to prove the skip is targeted.
		{Type: "references", SourceID: "a", TargetID: "b", SourcePath: "a.md", Kind: "derived"},
	}}

	links := linksFor(test, New(Deps{Nodes: nodes, Edges: edges}), "a")

	if len(links.in) != 0 {
		test.Fatalf("links.in=%+v want a's own sub-unit not rolled up into a self-backlink", links.in)
	}

	want := []LinkRef{{ID: "b", Title: "B", Type: "note", EdgeType: "references"}}

	if !reflect.DeepEqual(links.out, want) {
		test.Fatalf("links.out=%+v want %+v", links.out, want)
	}
}

// TestLinksIsolatedNodeReturnsEmptyRails pins the nil-slice trap at the
// projection level, where it originates: both rails must be non-nil so the
// payload marshals to [] rather than null. TestNodeLinksMarshalEmptyArray pins
// the same thing at the wire bytes; this one localizes a break to linksOf.
func TestLinksIsolatedNodeReturnsEmptyRails(test *testing.T) {
	nodes := fakeNodes{file: []index.NodeRow{{ID: "a", Type: "note", Title: "A", Path: "a.md"}}}

	links := linksFor(test, New(Deps{Nodes: nodes, Edges: fakeEdges{}}), "a")

	if links.out == nil || links.in == nil {
		test.Fatalf("out=%v in=%v, want non-nil empty rails", links.out, links.in)
	}
}
