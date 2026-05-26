package subunit_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/subunit"
)

// openSyncTestIndex spins up an on-disk sqlite index in test.TempDir().
// On-disk (rather than ":memory:") because the migrations apply
// `PRAGMA foreign_keys` per-connection and the sub-unit sync relies on
// FK cascades observed across separate connections in the same pool.
func openSyncTestIndex(test *testing.T) *index.Index {
	test.Helper()

	dir := test.TempDir()
	store, openErr := index.Open(filepath.Join(dir, "index.db"))

	if openErr != nil {
		test.Fatalf("index.Open: %v", openErr)
	}

	test.Cleanup(func() {
		_ = store.Close()
	})

	return store
}

// referencesManifest is the small manifest used by every sync test —
// declares a single wikilink-enabled edge ("references") plus the
// built-in sub-document pack.
func referencesManifest(test *testing.T) *manifest.Manifest {
	test.Helper()

	loaded := &manifest.Manifest{
		EdgeTypes: manifest.EdgeTypes{
			"references": manifest.EdgeType{
				From:        []string{"*"},
				To:          []string{"*"},
				Cardinality: manifest.CardinalityManyToMany,
				Wikilinks:   true,
			},
		},
	}

	return loaded
}

// seedFileRow inserts a placeholder file-level node row so sub-unit
// rows can satisfy the parent_id foreign-key analog and the rewriting
// machinery has a real parent to reference.
func seedFileRow(test *testing.T, repo *index.NodeRepo, id, path string) index.NodeRow {
	test.Helper()

	row := index.NodeRow{
		ID:             id,
		Type:           "note",
		Path:           path,
		Title:          id,
		PropertiesJSON: `{}`,
		LastMtime:      1,
		LastSize:       1,
		LastChecksum:   "h",
	}

	if upsertErr := repo.Upsert(row); upsertErr != nil {
		test.Fatalf("seed file row: %v", upsertErr)
	}

	return row
}

func newSync(store *index.Index, loaded *manifest.Manifest) (*subunit.Sync, *index.NodeRepo, *index.EdgeRepo, *index.EmbedQueueRepo) {
	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)
	queue := index.NewEmbedQueueRepo(store)

	return &subunit.Sync{
		Repo:     nodes,
		EdgeRepo: edges,
		EmbedQ:   queue,
		Manifest: loaded,
	}, nodes, edges, queue
}

func TestSync_FirstCallInsertsAllUnits(test *testing.T) {
	store := openSyncTestIndex(test)
	loaded := referencesManifest(test)
	sync, nodes, _, _ := newSync(store, loaded)

	parent := seedFileRow(test, nodes, "notes/intro", "notes/intro.md")

	units, parseErr := subunit.Parse([]byte("# Title\n\nFirst paragraph.\n\nSecond paragraph.\n"))

	if parseErr != nil {
		test.Fatalf("Parse: %v", parseErr)
	}

	if len(units) == 0 {
		test.Fatalf("Parse returned no units")
	}

	result, applyErr := sync.ApplyFile(context.Background(), parent, units)

	if applyErr != nil {
		test.Fatalf("ApplyFile: %v", applyErr)
	}

	if result.Inserted != len(units) {
		test.Errorf("Inserted = %d, want %d", result.Inserted, len(units))
	}

	if result.Deleted != 0 || result.Reordered != 0 {
		test.Errorf("unexpected churn: %+v", result)
	}

	listed, listErr := nodes.ListSubUnitsForFile(parent.ID)

	if listErr != nil {
		test.Fatalf("ListByParent: %v", listErr)
	}

	if len(listed) != len(units) {
		test.Errorf("ListByParent len = %d, want %d", len(listed), len(units))
	}
}

func TestSync_SecondCallIdenticalIsNoop(test *testing.T) {
	store := openSyncTestIndex(test)
	loaded := referencesManifest(test)
	sync, nodes, _, _ := newSync(store, loaded)

	parent := seedFileRow(test, nodes, "notes/idem", "notes/idem.md")

	units, _ := subunit.Parse([]byte("# A\n\np1\n\np2\n"))

	if _, applyErr := sync.ApplyFile(context.Background(), parent, units); applyErr != nil {
		test.Fatalf("first ApplyFile: %v", applyErr)
	}

	second, applyErr := sync.ApplyFile(context.Background(), parent, units)

	if applyErr != nil {
		test.Fatalf("second ApplyFile: %v", applyErr)
	}

	if second.Inserted != 0 || second.Deleted != 0 || second.Reordered != 0 {
		test.Errorf("second pass not a no-op: %+v", second)
	}
}

func TestSync_SecondCallRemovesUnitDeletesRow(test *testing.T) {
	store := openSyncTestIndex(test)
	loaded := referencesManifest(test)
	sync, nodes, _, _ := newSync(store, loaded)

	parent := seedFileRow(test, nodes, "notes/del", "notes/del.md")

	// Use a heading-free body so removing a leaf doesn't also
	// invalidate an enclosing section's hash (sections include
	// descendant text in their content hash, so removing a child
	// also evicts the section row — that case is covered by other
	// tests).
	first, _ := subunit.Parse([]byte("first paragraph\n\nsecond paragraph\n"))

	if _, applyErr := sync.ApplyFile(context.Background(), parent, first); applyErr != nil {
		test.Fatalf("first ApplyFile: %v", applyErr)
	}

	firstRows, _ := nodes.ListSubUnitsForFile(parent.ID)
	firstCount := len(firstRows)

	second, _ := subunit.Parse([]byte("first paragraph\n"))

	result, applyErr := sync.ApplyFile(context.Background(), parent, second)

	if applyErr != nil {
		test.Fatalf("second ApplyFile: %v", applyErr)
	}

	if result.Deleted != 1 {
		test.Errorf("Deleted = %d, want 1", result.Deleted)
	}

	if result.Inserted != 0 {
		test.Errorf("Inserted = %d, want 0", result.Inserted)
	}

	postRows, _ := nodes.ListSubUnitsForFile(parent.ID)

	if len(postRows) != firstCount-1 {
		test.Errorf("post len = %d, want %d", len(postRows), firstCount-1)
	}
}

func TestSync_SecondCallChangedUnitReplacesRow(test *testing.T) {
	store := openSyncTestIndex(test)
	loaded := referencesManifest(test)
	sync, nodes, _, _ := newSync(store, loaded)

	parent := seedFileRow(test, nodes, "notes/edit", "notes/edit.md")

	// Heading-free body so the diff observes a clean paragraph
	// swap; with a heading present, the section hash would also
	// change and the result would be Inserted=2 Deleted=2.
	first, _ := subunit.Parse([]byte("hello world\n"))

	if _, applyErr := sync.ApplyFile(context.Background(), parent, first); applyErr != nil {
		test.Fatalf("first ApplyFile: %v", applyErr)
	}

	firstRows, _ := nodes.ListSubUnitsForFile(parent.ID)

	second, _ := subunit.Parse([]byte("hello edited\n"))

	result, applyErr := sync.ApplyFile(context.Background(), parent, second)

	if applyErr != nil {
		test.Fatalf("second ApplyFile: %v", applyErr)
	}

	if result.Inserted != 1 || result.Deleted != 1 {
		test.Errorf("expected one swap, got %+v", result)
	}

	postRows, _ := nodes.ListSubUnitsForFile(parent.ID)

	if len(postRows) != len(firstRows) {
		test.Errorf("post len = %d, want %d", len(postRows), len(firstRows))
	}

	// The edited paragraph row must have a new id (its hash changed).
	firstIDs := map[string]struct{}{}

	for _, row := range firstRows {
		firstIDs[row.ID] = struct{}{}
	}

	foundNew := false

	for _, row := range postRows {
		if _, kept := firstIDs[row.ID]; !kept {
			foundNew = true
			break
		}
	}

	if !foundNew {
		test.Errorf("no new row id after content edit: pre=%v post=%v", firstRows, postRows)
	}
}

func TestSync_ReorderedUnitsBumpReordered(test *testing.T) {
	store := openSyncTestIndex(test)
	loaded := referencesManifest(test)
	sync, nodes, _, _ := newSync(store, loaded)

	parent := seedFileRow(test, nodes, "notes/reorder", "notes/reorder.md")

	first, _ := subunit.Parse([]byte("alpha paragraph\n\nbravo paragraph\n"))

	if _, applyErr := sync.ApplyFile(context.Background(), parent, first); applyErr != nil {
		test.Fatalf("first ApplyFile: %v", applyErr)
	}

	// Swap the order — same content, different ordinals.
	second, _ := subunit.Parse([]byte("bravo paragraph\n\nalpha paragraph\n"))

	result, applyErr := sync.ApplyFile(context.Background(), parent, second)

	if applyErr != nil {
		test.Fatalf("second ApplyFile: %v", applyErr)
	}

	if result.Inserted != 0 || result.Deleted != 0 {
		test.Errorf("unexpected insert/delete: %+v", result)
	}

	if result.Reordered == 0 {
		test.Errorf("Reordered = 0, want >0")
	}

	// Read the rows back and confirm the new ordinals actually landed in
	// the DB. This guards against a silent BulkUpsert ordinal-column
	// failure that would still bump Reordered (driven from the in-memory
	// diff) without persisting the change.
	persisted, listErr := nodes.ListSubUnitsForFile(parent.ID)

	if listErr != nil {
		test.Fatalf("ListSubUnitsForFile: %v", listErr)
	}

	persistedByHash := make(map[string]int64, len(persisted))

	for _, row := range persisted {
		hash := row.ID[len(parent.ID)+1:]
		if !row.Ordinal.Valid {
			test.Errorf("row %s has NULL ordinal", row.ID)
			continue
		}

		persistedByHash[hash] = row.Ordinal.Int64
	}

	for _, unit := range second {
		got, ok := persistedByHash[unit.Hash]

		if !ok {
			test.Errorf("hash %s missing from persisted rows", unit.Hash)
			continue
		}

		if got != int64(unit.Ordinal) {
			test.Errorf("hash %s: persisted ordinal = %d, want %d", unit.Hash, got, unit.Ordinal)
		}
	}
}

func TestSync_WikilinksEmitOutboundEdges(test *testing.T) {
	store := openSyncTestIndex(test)
	loaded := referencesManifest(test)
	sync, nodes, edges, _ := newSync(store, loaded)

	parent := seedFileRow(test, nodes, "notes/src", "notes/src.md")
	// Seed the target file so the edge row points at a real node id.
	seedFileRow(test, nodes, "notes/target", "notes/target.md")

	units, _ := subunit.Parse([]byte("see [[notes/target]] for more\n"))

	if _, applyErr := sync.ApplyFile(context.Background(), parent, units); applyErr != nil {
		test.Fatalf("ApplyFile: %v", applyErr)
	}

	subRows, _ := nodes.ListSubUnitsForFile(parent.ID)

	if len(subRows) == 0 {
		test.Fatalf("no sub-unit rows")
	}

	var found bool

	for _, sub := range subRows {
		listed, _ := edges.ListBySource(sub.ID)

		for _, edge := range listed {
			if edge.Type == "references" && edge.TargetID == "notes/target" {
				found = true
			}
		}
	}

	if !found {
		test.Errorf("expected references edge from a sub-unit to notes/target")
	}
}

func TestSync_ContainsEdgePerInsertedUnit(test *testing.T) {
	store := openSyncTestIndex(test)
	loaded := referencesManifest(test)
	sync, nodes, edges, _ := newSync(store, loaded)

	parent := seedFileRow(test, nodes, "notes/contains", "notes/contains.md")

	units, _ := subunit.Parse([]byte("# H\n\nfirst\n\nsecond\n"))

	if _, applyErr := sync.ApplyFile(context.Background(), parent, units); applyErr != nil {
		test.Fatalf("ApplyFile: %v", applyErr)
	}

	listed, _ := edges.ListBySource(parent.ID)

	var containsCount int

	for _, edge := range listed {
		if edge.Type == "contains" {
			containsCount++
		}
	}

	if containsCount != len(units) {
		test.Errorf("contains edges = %d, want %d", containsCount, len(units))
	}
}

func TestSync_FrontmatterEdgesSurviveContainsRewrite(test *testing.T) {
	store := openSyncTestIndex(test)
	loaded := referencesManifest(test)
	sync, nodes, edges, _ := newSync(store, loaded)

	parent := seedFileRow(test, nodes, "notes/keepfm", "notes/keepfm.md")
	seedFileRow(test, nodes, "notes/other", "notes/other.md")

	// Seed a file-level frontmatter-style edge under the same
	// (source_id, source_path) pair as the file row. This stands in
	// for what the file-level reindex pass writes via EdgeRepo.UpsertAll.
	frontEdge := []index.EdgeRow{
		{Type: "references", SourceID: parent.ID, TargetID: "notes/other", SourcePath: parent.Path},
	}

	if upsertErr := edges.UpsertAll(parent.ID, parent.Path, frontEdge); upsertErr != nil {
		test.Fatalf("seed frontmatter edge: %v", upsertErr)
	}

	units, _ := subunit.Parse([]byte("# H\n\nfirst para\n"))

	if _, applyErr := sync.ApplyFile(context.Background(), parent, units); applyErr != nil {
		test.Fatalf("ApplyFile: %v", applyErr)
	}

	listed, _ := edges.ListBySource(parent.ID)

	var sawFrontmatter, sawContains bool

	for _, edge := range listed {
		switch edge.Type {
		case "references":
			if edge.TargetID == "notes/other" {
				sawFrontmatter = true
			}
		case "contains":
			sawContains = true
		}
	}

	if !sawFrontmatter {
		test.Errorf("frontmatter-style references edge was wiped by contains rewrite")
	}

	if !sawContains {
		test.Errorf("contains edge missing after sync")
	}
}

func TestSync_LeavesEnqueuedSectionsSkipped(test *testing.T) {
	store := openSyncTestIndex(test)
	loaded := referencesManifest(test)
	sync, nodes, _, queue := newSync(store, loaded)

	parent := seedFileRow(test, nodes, "notes/enqueue", "notes/enqueue.md")

	units, _ := subunit.Parse([]byte("# Heading\n\nbody paragraph\n"))

	if _, applyErr := sync.ApplyFile(context.Background(), parent, units); applyErr != nil {
		test.Fatalf("ApplyFile: %v", applyErr)
	}

	ids, _ := queue.ListNodeIDs()

	queued := map[string]struct{}{}

	for _, id := range ids {
		queued[id] = struct{}{}
	}

	for _, unit := range units {
		subunitID := parent.ID + "#" + unit.Hash
		_, present := queued[subunitID]

		switch unit.Kind {
		case subunit.KindSection:
			if present {
				test.Errorf("section %q was enqueued; expected skip", subunitID)
			}
		default:
			if !present {
				test.Errorf("leaf %q (%s) was not enqueued", subunitID, unit.Kind)
			}
		}
	}
}

func TestSync_WritesKindAndSourceForSubUnits(test *testing.T) {
	store := openSyncTestIndex(test)
	loaded := referencesManifest(test)
	sync, nodes, _, _ := newSync(store, loaded)

	parent := seedFileRow(test, nodes, "notes/kind", "notes/kind.md")

	units, parseErr := subunit.Parse([]byte("# Heading\n\nfirst paragraph\n\nsecond paragraph\n"))

	if parseErr != nil {
		test.Fatalf("Parse: %v", parseErr)
	}

	if _, applyErr := sync.ApplyFile(context.Background(), parent, units); applyErr != nil {
		test.Fatalf("ApplyFile: %v", applyErr)
	}

	rows, queryErr := store.DB().Query(`SELECT id, kind, source FROM nodes WHERE parent_id IS NOT NULL`)

	if queryErr != nil {
		test.Fatalf("query subunits: %v", queryErr)
	}

	defer rows.Close()

	var seen int

	for rows.Next() {
		var (
			id     string
			kind   *string
			source *string
		)

		if scanErr := rows.Scan(&id, &kind, &source); scanErr != nil {
			test.Fatalf("scan: %v", scanErr)
		}

		seen++

		if kind == nil || *kind != "subunit" {
			test.Errorf("row %s: kind = %v, want \"subunit\"", id, kind)
		}

		if source == nil || *source != "markdown" {
			test.Errorf("row %s: source = %v, want \"markdown\"", id, source)
		}
	}

	if iterErr := rows.Err(); iterErr != nil {
		test.Fatalf("iter: %v", iterErr)
	}

	if seen == 0 {
		test.Fatal("expected at least one sub-unit row")
	}
}

func TestSync_EmptyUnitsDeletesAllRowsAndContains(test *testing.T) {
	store := openSyncTestIndex(test)
	loaded := referencesManifest(test)
	sync, nodes, edges, _ := newSync(store, loaded)

	parent := seedFileRow(test, nodes, "notes/clear", "notes/clear.md")

	units, _ := subunit.Parse([]byte("# H\n\na\n\nb\n"))

	if _, applyErr := sync.ApplyFile(context.Background(), parent, units); applyErr != nil {
		test.Fatalf("first ApplyFile: %v", applyErr)
	}

	result, applyErr := sync.ApplyFile(context.Background(), parent, nil)

	if applyErr != nil {
		test.Fatalf("clear ApplyFile: %v", applyErr)
	}

	if result.Deleted == 0 {
		test.Errorf("Deleted = 0, want >0")
	}

	postRows, _ := nodes.ListSubUnitsForFile(parent.ID)

	if len(postRows) != 0 {
		test.Errorf("post rows = %d, want 0", len(postRows))
	}

	listed, _ := edges.ListBySource(parent.ID)

	for _, edge := range listed {
		if edge.Type == "contains" {
			test.Errorf("contains edge survived empty-units sync: %+v", edge)
		}
	}
}
