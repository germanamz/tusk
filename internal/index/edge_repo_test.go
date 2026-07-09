package index_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/typeref"
)

func strptr(value string) *string {
	return &value
}

func seedFourNodes(test *testing.T, store *index.Index) {
	test.Helper()
	seedNodes(test, store, "projects/a", "projects/b", "notes/a", "notes/a#sec")
}

// insertEdge writes a single edge row. source is *string so callers
// can pass nil to mean SQL NULL (the user namespace).
func insertEdge(test *testing.T, store *index.Index, edgeType, sourceID, targetID, sourcePath, kind string, source *string) {
	test.Helper()

	var sourceArg any
	if source == nil {
		sourceArg = nil
	} else {
		sourceArg = *source
	}

	if _, execErr := store.DB().Exec(`
		INSERT INTO edges (type, source_id, target_id, source_path, kind, source)
		VALUES (?, ?, ?, ?, ?, ?)
	`, edgeType, sourceID, targetID, sourcePath, kind, sourceArg); execErr != nil {
		test.Fatalf("insertEdge: %v", execErr)
	}
}

// newTestEdgeRepo opens a fresh index, seeds the provided node ids
// (required since the P2 migration added a foreign key from
// edges.source_id to nodes.id), and returns an EdgeRepo against it.
func newTestEdgeRepo(test *testing.T, nodeIDs ...string) *index.EdgeRepo {
	test.Helper()

	store := openTestIndex(test)

	if len(nodeIDs) > 0 {
		seedNodes(test, store, nodeIDs...)
	}

	return index.NewEdgeRepo(store)
}

func TestEdgeRepo_UpsertAllAndListBySource(test *testing.T) {
	repo := newTestEdgeRepo(test, "tickets/foo")

	edges := []index.EdgeRow{
		{Type: "parent", SourceID: "tickets/foo", TargetID: "tickets/epic", SourcePath: "tickets/foo.md", Kind: "direct"},
		{Type: "blocks", SourceID: "tickets/foo", TargetID: "tickets/bar", SourcePath: "tickets/foo.md", Kind: "direct"},
	}

	if upsertErr := repo.UpsertAll("tickets/foo", "tickets/foo.md", edges); upsertErr != nil {
		test.Fatalf("UpsertAll: %v", upsertErr)
	}

	listed, listErr := repo.ListBySource("tickets/foo")

	if listErr != nil {
		test.Fatalf("ListBySource: %v", listErr)
	}

	if len(listed) != 2 {
		test.Errorf("len = %d, want 2", len(listed))
	}

	triples := map[string]bool{}

	for _, row := range listed {
		triples[row.Type+"|"+row.SourceID+"|"+row.TargetID] = true
	}

	if !triples["parent|tickets/foo|tickets/epic"] || !triples["blocks|tickets/foo|tickets/bar"] {
		test.Errorf("missing expected triples in listed = %+v", listed)
	}
}

func TestEdgeRepo_UpsertAllReplacesExistingEdgesForSource(test *testing.T) {
	repo := newTestEdgeRepo(test, "x")

	first := []index.EdgeRow{
		{Type: "parent", SourceID: "x", TargetID: "y", SourcePath: "x.md", Kind: "direct"},
		{Type: "blocks", SourceID: "x", TargetID: "z", SourcePath: "x.md", Kind: "direct"},
	}

	repo.UpsertAll("x", "x.md", first)

	second := []index.EdgeRow{
		{Type: "parent", SourceID: "x", TargetID: "y2", SourcePath: "x.md", Kind: "direct"},
	}

	if upsertErr := repo.UpsertAll("x", "x.md", second); upsertErr != nil {
		test.Fatalf("second UpsertAll: %v", upsertErr)
	}

	listed, _ := repo.ListBySource("x")

	if len(listed) != 1 {
		test.Errorf("len = %d, want 1 after replace", len(listed))
	}

	if listed[0].TargetID != "y2" {
		test.Errorf("Target = %q, want y2", listed[0].TargetID)
	}
}

// TestEdgeRepo_UpsertAllManyReplacesPerSource pins B4: UpsertAllMany applies
// several per-source replacements in one transaction with the same
// delete-then-insert contract as UpsertAll (so stale edges are removed, not
// orphaned), and leaves sources absent from the batch untouched.
func TestEdgeRepo_UpsertAllManyReplacesPerSource(test *testing.T) {
	repo := newTestEdgeRepo(test, "a", "b")

	first := []index.EdgeBatch{
		{SourceID: "a", SourcePath: "a.md", Edges: []index.EdgeRow{
			{Type: "blocks", SourceID: "a", TargetID: "y", SourcePath: "a.md", Kind: "direct"},
			{Type: "blocks", SourceID: "a", TargetID: "z", SourcePath: "a.md", Kind: "direct"},
		}},
		{SourceID: "b", SourcePath: "b.md", Edges: []index.EdgeRow{
			{Type: "blocks", SourceID: "b", TargetID: "y", SourcePath: "b.md", Kind: "direct"},
		}},
	}

	if upsertErr := repo.UpsertAllMany(first); upsertErr != nil {
		test.Fatalf("UpsertAllMany: %v", upsertErr)
	}

	aEdges, _ := repo.ListBySource("a")
	bEdges, _ := repo.ListBySource("b")

	if len(aEdges) != 2 || len(bEdges) != 1 {
		test.Fatalf("after first batch: a=%d (want 2), b=%d (want 1)", len(aEdges), len(bEdges))
	}

	// Replace a's edge set; omit b this round.
	second := []index.EdgeBatch{
		{SourceID: "a", SourcePath: "a.md", Edges: []index.EdgeRow{
			{Type: "blocks", SourceID: "a", TargetID: "w", SourcePath: "a.md", Kind: "direct"},
		}},
	}

	if upsertErr := repo.UpsertAllMany(second); upsertErr != nil {
		test.Fatalf("second UpsertAllMany: %v", upsertErr)
	}

	aEdges, _ = repo.ListBySource("a")

	if len(aEdges) != 1 || aEdges[0].TargetID != "w" {
		test.Errorf("a edges after replace = %+v, want exactly [a->w] (delete+insert, no orphans)", aEdges)
	}

	bEdges, _ = repo.ListBySource("b")

	if len(bEdges) != 1 || bEdges[0].TargetID != "y" {
		test.Errorf("b edges (absent from second batch) should be untouched = %+v", bEdges)
	}
}

func TestEdgeRepo_UpsertAllManyEmptyIsNoop(test *testing.T) {
	repo := newTestEdgeRepo(test)

	if upsertErr := repo.UpsertAllMany(nil); upsertErr != nil {
		test.Fatalf("UpsertAllMany(nil): %v", upsertErr)
	}
}

func TestEdgeRepo_ListByTarget(test *testing.T) {
	repo := newTestEdgeRepo(test, "a", "b")

	repo.UpsertAll("a", "a.md", []index.EdgeRow{
		{Type: "blocks", SourceID: "a", TargetID: "z", SourcePath: "a.md", Kind: "direct"},
	})

	repo.UpsertAll("b", "b.md", []index.EdgeRow{
		{Type: "blocks", SourceID: "b", TargetID: "z", SourcePath: "b.md", Kind: "direct"},
	})

	listed, listErr := repo.ListByTarget("z")

	if listErr != nil {
		test.Fatalf("ListByTarget: %v", listErr)
	}

	if len(listed) != 2 {
		test.Errorf("len = %d, want 2", len(listed))
	}
}

func TestEdgeRepo_ListByType(test *testing.T) {
	repo := newTestEdgeRepo(test, "a")

	repo.UpsertAll("a", "a.md", []index.EdgeRow{
		{Type: "blocks", SourceID: "a", TargetID: "x", SourcePath: "a.md", Kind: "direct"},
		{Type: "parent", SourceID: "a", TargetID: "y", SourcePath: "a.md", Kind: "direct"},
	})

	listed, listErr := repo.ListByType("blocks")

	if listErr != nil {
		test.Fatalf("ListByType: %v", listErr)
	}

	if len(listed) != 1 || listed[0].TargetID != "x" {
		test.Errorf("listed = %+v", listed)
	}
}

func TestNeighborsByEdgeRefs_ScopeAnyMatchesUnion(test *testing.T) {
	test.Parallel()

	store := openTestIndex(test)
	seedFourNodes(test, store)

	insertEdge(test, store, "contains", "projects/a", "projects/b", "projects/a.md", "direct", nil)
	insertEdge(test, store, "contains", "notes/a", "notes/a#sec", "notes/a.md", "structural", strptr("markdown"))

	repo := index.NewEdgeRepo(store)
	rows, err := repo.NeighborsByEdgeRefs(
		[]typeref.EdgeRef{{Scope: typeref.ScopeAny, Type: "contains"}},
		[]string{"projects/a", "notes/a"},
	)
	if err != nil {
		test.Fatalf("NeighborsByEdgeRefs: %v", err)
	}
	if len(rows) != 2 {
		test.Errorf("ScopeAny returned %d rows, want 2", len(rows))
	}
}

func TestNeighborsByEdgeRefs_ScopeUserMatchesNullSource(test *testing.T) {
	test.Parallel()

	store := openTestIndex(test)
	seedFourNodes(test, store)
	insertEdge(test, store, "contains", "projects/a", "projects/b", "projects/a.md", "direct", nil)
	insertEdge(test, store, "contains", "notes/a", "notes/a#sec", "notes/a.md", "structural", strptr("markdown"))

	repo := index.NewEdgeRepo(store)
	rows, err := repo.NeighborsByEdgeRefs(
		[]typeref.EdgeRef{{Scope: typeref.ScopeUser, Type: "contains"}},
		[]string{"projects/a", "notes/a"},
	)
	if err != nil {
		test.Fatalf("NeighborsByEdgeRefs: %v", err)
	}
	if len(rows) != 1 {
		test.Errorf("ScopeUser returned %d rows, want 1 (only the user-namespace edge)", len(rows))
	}
	if rows[0].SourceID != "projects/a" {
		test.Errorf("ScopeUser returned wrong edge: %+v", rows[0])
	}
}

func TestNeighborsByEdgeRefs_ScopeSourceMatchesOnePack(test *testing.T) {
	test.Parallel()

	store := openTestIndex(test)
	seedFourNodes(test, store)
	insertEdge(test, store, "contains", "projects/a", "projects/b", "projects/a.md", "direct", nil)
	insertEdge(test, store, "contains", "notes/a", "notes/a#sec", "notes/a.md", "structural", strptr("markdown"))

	repo := index.NewEdgeRepo(store)
	rows, err := repo.NeighborsByEdgeRefs(
		[]typeref.EdgeRef{{Scope: typeref.ScopeSource, Source: "markdown", Type: "contains"}},
		[]string{"projects/a", "notes/a"},
	)
	if err != nil {
		test.Fatalf("NeighborsByEdgeRefs: %v", err)
	}
	if len(rows) != 1 {
		test.Errorf("ScopeSource returned %d rows, want 1 (only the markdown edge)", len(rows))
	}
}

func TestEdgeRepo_DeleteBySource(test *testing.T) {
	repo := newTestEdgeRepo(test, "doomed")

	repo.UpsertAll("doomed", "doomed.md", []index.EdgeRow{
		{Type: "parent", SourceID: "doomed", TargetID: "x", SourcePath: "doomed.md", Kind: "direct"},
	})

	if deleteErr := repo.DeleteBySource("doomed"); deleteErr != nil {
		test.Fatalf("DeleteBySource: %v", deleteErr)
	}

	listed, _ := repo.ListBySource("doomed")

	if len(listed) != 0 {
		test.Errorf("len = %d, want 0", len(listed))
	}
}

// RetargetEdges and ListByTargetOrSubUnits must treat the sub-unit prefix in
// characters SQLite understands: node ids are user paths and routinely carry
// multi-byte characters (accents, CJK). Regression test: byte lengths were
// bound into SQLite substr(), which counts UTF-8 characters, so every
// non-ASCII id silently escaped both the selection and the retarget.
func TestEdgeRepo_RetargetEdges_MultibyteIDs(test *testing.T) {
	store := openTestIndex(test)
	seedNodes(test, store, "notas/reunión", "notas/otro")
	repo := index.NewEdgeRepo(store)

	insertEdge(test, store, "references", "notas/otro", "notas/reunión", "notas/otro.md", "direct", nil)
	insertEdge(test, store, "references", "notas/otro", "notas/reunión#S1", "notas/otro.md", "direct", nil)

	listed, listErr := repo.ListByTargetOrSubUnits("notas/reunión")

	if listErr != nil {
		test.Fatalf("ListByTargetOrSubUnits: %v", listErr)
	}

	if len(listed) != 2 {
		test.Fatalf("ListByTargetOrSubUnits = %d rows, want 2 (sub-unit-targeted row missed)", len(listed))
	}

	if retargetErr := repo.RetargetEdges("notas/reunión", "notas/plan"); retargetErr != nil {
		test.Fatalf("RetargetEdges: %v", retargetErr)
	}

	if stale, _ := repo.ListByTarget("notas/reunión#S1"); len(stale) != 0 {
		test.Errorf("sub-unit-targeted edge still points at old id: %+v", stale)
	}

	moved, _ := repo.ListByTarget("notas/plan#S1")

	if len(moved) != 1 {
		test.Errorf("edge not retargeted to notas/plan#S1: %+v", moved)
	}
}

// When the referrer already carries an edge to BOTH the old and the new id,
// retargeting must not leave two identical rows behind: SQLite's UNIQUE
// treats NULL `source` values as distinct, so OR REPLACE alone never fires
// for direct/derived edges.
func TestEdgeRepo_RetargetEdges_DedupesNullSourceDuplicates(test *testing.T) {
	store := openTestIndex(test)
	seedNodes(test, store, "notes/old", "notes/new", "notes/ref")
	repo := index.NewEdgeRepo(store)

	insertEdge(test, store, "references", "notes/ref", "notes/old", "notes/ref.md", "direct", nil)
	insertEdge(test, store, "references", "notes/ref", "notes/new", "notes/ref.md", "direct", nil)

	if retargetErr := repo.RetargetEdges("notes/old", "notes/new"); retargetErr != nil {
		test.Fatalf("RetargetEdges: %v", retargetErr)
	}

	listed, _ := repo.ListByTarget("notes/new")

	if len(listed) != 1 {
		test.Errorf("expected a single deduped edge, got %+v", listed)
	}
}
