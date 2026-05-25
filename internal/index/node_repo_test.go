package index_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func openTestIndex(test *testing.T) *index.Index {
	test.Helper()

	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	return store
}

// seedNodes inserts a minimal placeholder node row for each id so that
// downstream tests inserting edges or embeddings satisfy the foreign
// keys added in the P2 migration. The placeholder shape is the smallest
// row NodeRepo.Upsert accepts; tests that need richer fields should
// upsert again with the desired values (Upsert is replace-by-id).
func seedNodes(test *testing.T, store *index.Index, ids ...string) {
	test.Helper()

	repo := index.NewNodeRepo(store)

	for _, id := range ids {
		row := index.NodeRow{
			ID:             id,
			Type:           "note",
			Path:           id + ".md",
			Title:          id,
			PropertiesJSON: "{}",
			LastChecksum:   "x",
		}

		if upsertErr := repo.Upsert(row); upsertErr != nil {
			test.Fatalf("seedNodes: upsert %s: %v", id, upsertErr)
		}
	}
}

func TestNodeRepo_UpsertAndGet(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	row := index.NodeRow{
		ID:             "notes/auth-rfc",
		Type:           "note",
		Path:           "notes/auth-rfc.md",
		Title:          "Auth RFC",
		PropertiesJSON: `{"title":"Auth RFC"}`,
		LastMtime:      100,
		LastSize:       42,
		LastChecksum:   "abc123",
	}

	if upsertErr := repo.Upsert(row); upsertErr != nil {
		test.Fatalf("Upsert: %v", upsertErr)
	}

	loaded, loadErr := repo.Get("notes/auth-rfc")

	if loadErr != nil {
		test.Fatalf("Get: %v", loadErr)
	}

	if loaded.Type != "note" || loaded.Title != "Auth RFC" {
		test.Errorf("got = %+v", loaded)
	}
}

func TestNodeRepo_UpsertReplacesExistingRow(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	first := index.NodeRow{
		ID: "x", Type: "ticket", Path: "x.md", Title: "first",
		PropertiesJSON: `{}`, LastMtime: 1, LastSize: 1, LastChecksum: "a",
	}

	second := index.NodeRow{
		ID: "x", Type: "ticket", Path: "x.md", Title: "second",
		PropertiesJSON: `{}`, LastMtime: 2, LastSize: 2, LastChecksum: "b",
	}

	if upsertErr := repo.Upsert(first); upsertErr != nil {
		test.Fatalf("first upsert: %v", upsertErr)
	}

	if upsertErr := repo.Upsert(second); upsertErr != nil {
		test.Fatalf("second upsert: %v", upsertErr)
	}

	loaded, loadErr := repo.Get("x")

	if loadErr != nil {
		test.Fatalf("Get: %v", loadErr)
	}

	if loaded.Title != "second" {
		test.Errorf("Title = %q, want %q", loaded.Title, "second")
	}
}

func TestNodeRepo_GetReturnsErrNotFoundWhenMissing(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	_, getErr := repo.Get("missing")

	if getErr != index.ErrNodeNotFound {
		test.Errorf("err = %v, want ErrNodeNotFound", getErr)
	}
}

func TestNodeRepo_ListAll(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	rows := []index.NodeRow{
		{ID: "a", Type: "ticket", Path: "a.md", Title: "A", PropertiesJSON: `{}`, LastMtime: 1, LastSize: 1, LastChecksum: "1"},
		{ID: "b", Type: "note", Path: "b.md", Title: "B", PropertiesJSON: `{}`, LastMtime: 2, LastSize: 2, LastChecksum: "2"},
	}

	for _, row := range rows {
		if upsertErr := repo.Upsert(row); upsertErr != nil {
			test.Fatalf("upsert: %v", upsertErr)
		}
	}

	listed, listErr := repo.List(index.ListFilter{})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(listed) != 2 {
		test.Fatalf("len = %d, want 2", len(listed))
	}
}

func TestNodeRepo_ListByType(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	repo.Upsert(index.NodeRow{ID: "a", Type: "ticket", Path: "a.md", Title: "A", PropertiesJSON: `{}`, LastMtime: 1, LastSize: 1, LastChecksum: "1"})
	repo.Upsert(index.NodeRow{ID: "b", Type: "note", Path: "b.md", Title: "B", PropertiesJSON: `{}`, LastMtime: 2, LastSize: 2, LastChecksum: "2"})

	listed, listErr := repo.List(index.ListFilter{Type: "ticket"})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(listed) != 1 || listed[0].ID != "a" {
		test.Errorf("listed = %+v", listed)
	}
}

func TestNodeRepo_ListByParent(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	// File row (parent).
	parent := index.NodeRow{
		ID: "notes/auth-rfc", Type: "note", Path: "notes/auth-rfc.md", Title: "Auth RFC",
		PropertiesJSON: `{}`, LastMtime: 1, LastSize: 1, LastChecksum: "h",
	}

	if upsertErr := repo.Upsert(parent); upsertErr != nil {
		test.Fatalf("Upsert parent: %v", upsertErr)
	}

	// Three sub-units out of order so the ORDER BY ordinal ASC assertion bites.
	subUnits := []index.NodeRow{
		{
			ID: "notes/auth-rfc#bbb", Type: "paragraph", Path: parent.Path,
			Title: "second", PropertiesJSON: `{}`, LastChecksum: "h",
			ParentID:     sql.NullString{String: parent.ID, Valid: true},
			Ordinal:      sql.NullInt64{Int64: 1, Valid: true},
			EmbedPayload: sql.NullString{String: "second body", Valid: true},
		},
		{
			ID: "notes/auth-rfc#aaa", Type: "paragraph", Path: parent.Path,
			Title: "first", PropertiesJSON: `{}`, LastChecksum: "h",
			ParentID: sql.NullString{String: parent.ID, Valid: true},
			Ordinal:  sql.NullInt64{Int64: 0, Valid: true},
		},
		{
			ID: "notes/auth-rfc#ccc", Type: "section", Path: parent.Path,
			Title: "third", PropertiesJSON: `{"heading-level":2}`, LastChecksum: "h",
			ParentID: sql.NullString{String: parent.ID, Valid: true},
			Ordinal:  sql.NullInt64{Int64: 2, Valid: true},
		},
	}

	for _, row := range subUnits {
		if upsertErr := repo.Upsert(row); upsertErr != nil {
			test.Fatalf("Upsert sub-unit %s: %v", row.ID, upsertErr)
		}
	}

	listed, listErr := repo.ListByParent(parent.ID)

	if listErr != nil {
		test.Fatalf("ListByParent: %v", listErr)
	}

	if len(listed) != 3 {
		test.Fatalf("len = %d, want 3", len(listed))
	}

	wantOrder := []string{"notes/auth-rfc#aaa", "notes/auth-rfc#bbb", "notes/auth-rfc#ccc"}

	for idx, row := range listed {
		if row.ID != wantOrder[idx] {
			test.Errorf("row %d id = %q, want %q", idx, row.ID, wantOrder[idx])
		}

		if !row.ParentID.Valid || row.ParentID.String != parent.ID {
			test.Errorf("row %d parent_id = %+v, want %q", idx, row.ParentID, parent.ID)
		}
	}

	// Sanity: a parent with no sub-units returns an empty slice.
	empty, emptyErr := repo.ListByParent("nonexistent")

	if emptyErr != nil {
		test.Fatalf("ListByParent empty: %v", emptyErr)
	}

	if len(empty) != 0 {
		test.Errorf("len(empty) = %d, want 0", len(empty))
	}
}

func TestNodeRepo_BulkUpsert(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	rows := []index.NodeRow{
		{
			ID: "doc-one", Type: "note", Path: "doc-one.md", Title: "One",
			PropertiesJSON: `{}`, LastChecksum: "h",
		},
		{
			ID: "doc-two", Type: "note", Path: "doc-two.md", Title: "Two",
			PropertiesJSON: `{}`, LastChecksum: "h",
		},
	}

	if bulkErr := repo.BulkUpsert(rows); bulkErr != nil {
		test.Fatalf("BulkUpsert: %v", bulkErr)
	}

	for _, row := range rows {
		loaded, getErr := repo.Get(row.ID)

		if getErr != nil {
			test.Fatalf("Get %s: %v", row.ID, getErr)
		}

		if loaded.Title != row.Title {
			test.Errorf("title for %s = %q, want %q", row.ID, loaded.Title, row.Title)
		}
	}

	// Re-upsert replaces fields.
	rows[0].Title = "One Updated"

	if bulkErr := repo.BulkUpsert(rows); bulkErr != nil {
		test.Fatalf("second BulkUpsert: %v", bulkErr)
	}

	updated, _ := repo.Get("doc-one")

	if updated.Title != "One Updated" {
		test.Errorf("updated title = %q, want %q", updated.Title, "One Updated")
	}

	// Empty slice is a no-op.
	if bulkErr := repo.BulkUpsert(nil); bulkErr != nil {
		test.Errorf("BulkUpsert(nil): %v", bulkErr)
	}
}

func TestNodeRepo_BulkDelete(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	rows := []index.NodeRow{
		{ID: "a", Type: "note", Path: "a.md", Title: "A", PropertiesJSON: `{}`, LastChecksum: "h"},
		{ID: "b", Type: "note", Path: "b.md", Title: "B", PropertiesJSON: `{}`, LastChecksum: "h"},
		{ID: "c", Type: "note", Path: "c.md", Title: "C", PropertiesJSON: `{}`, LastChecksum: "h"},
	}

	if bulkErr := repo.BulkUpsert(rows); bulkErr != nil {
		test.Fatalf("seed BulkUpsert: %v", bulkErr)
	}

	if deleteErr := repo.BulkDelete([]string{"a", "c"}); deleteErr != nil {
		test.Fatalf("BulkDelete: %v", deleteErr)
	}

	if _, getErr := repo.Get("a"); getErr != index.ErrNodeNotFound {
		test.Errorf("a still present: %v", getErr)
	}

	if _, getErr := repo.Get("c"); getErr != index.ErrNodeNotFound {
		test.Errorf("c still present: %v", getErr)
	}

	if _, getErr := repo.Get("b"); getErr != nil {
		test.Errorf("b unexpectedly removed: %v", getErr)
	}

	// Empty slice is a no-op.
	if deleteErr := repo.BulkDelete(nil); deleteErr != nil {
		test.Errorf("BulkDelete(nil): %v", deleteErr)
	}
}

func TestNodeRepo_BulkDeleteCascadesEdgesAndEmbeddings(test *testing.T) {
	store := openTestIndex(test)
	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)
	embeddings := index.NewEmbeddingRepo(store)

	// Seed a parent file + one sub-unit.
	if upsertErr := nodes.Upsert(index.NodeRow{
		ID: "f", Type: "note", Path: "f.md", Title: "F", PropertiesJSON: `{}`, LastChecksum: "h",
	}); upsertErr != nil {
		test.Fatalf("Upsert parent: %v", upsertErr)
	}

	subID := "f#abc"

	if upsertErr := nodes.Upsert(index.NodeRow{
		ID: subID, Type: "paragraph", Path: "f.md", Title: "p", PropertiesJSON: `{}`, LastChecksum: "h",
		ParentID: sql.NullString{String: "f", Valid: true},
		Ordinal:  sql.NullInt64{Int64: 0, Valid: true},
	}); upsertErr != nil {
		test.Fatalf("Upsert sub-unit: %v", upsertErr)
	}

	// Seed an outbound edge and an embedding row on the sub-unit.
	if upsertErr := edges.UpsertAll(subID, "f.md", []index.EdgeRow{
		{Type: "references", SourceID: subID, TargetID: "f", SourcePath: "f.md"},
	}); upsertErr != nil {
		test.Fatalf("UpsertAll: %v", upsertErr)
	}

	if upsertErr := embeddings.Upsert(index.EmbeddingRow{
		NodeID: subID, Model: "test", ContentHash: "h", Vector: []float32{0}, Dim: 1,
	}); upsertErr != nil {
		test.Fatalf("embeddings.Upsert: %v", upsertErr)
	}

	// Bulk delete the sub-unit — cascades must remove its edge and embedding.
	if deleteErr := nodes.BulkDelete([]string{subID}); deleteErr != nil {
		test.Fatalf("BulkDelete: %v", deleteErr)
	}

	remainingEdges, _ := edges.ListBySource(subID)

	if len(remainingEdges) != 0 {
		test.Errorf("edges not cascaded: %+v", remainingEdges)
	}

	remainingEmbeds, _ := embeddings.GetByNodeID(subID)

	if len(remainingEmbeds) != 0 {
		test.Errorf("embeddings not cascaded: %+v", remainingEmbeds)
	}
}

func TestNodeRepo_DeleteByPath(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	repo.Upsert(index.NodeRow{ID: "x", Type: "note", Path: "x.md", Title: "X", PropertiesJSON: `{}`, LastMtime: 1, LastSize: 1, LastChecksum: "1"})

	if deleteErr := repo.DeleteByPath("x.md"); deleteErr != nil {
		test.Fatalf("DeleteByPath: %v", deleteErr)
	}

	_, getErr := repo.Get("x")

	if getErr != index.ErrNodeNotFound {
		test.Errorf("err after delete = %v, want ErrNodeNotFound", getErr)
	}
}
