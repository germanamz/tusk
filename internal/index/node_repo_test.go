package index_test

import (
	"database/sql"
	"path/filepath"
	"strings"
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

func TestNodeRepo_UpsertSetsFileKindAndNullSource(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	row := index.NodeRow{
		ID:             "notes/hello",
		Type:           "note",
		Path:           "notes/hello.md",
		Title:          "Hello",
		PropertiesJSON: "{}",
		LastMtime:      1,
		LastSize:       1,
		LastChecksum:   "abc",
	}

	if upsertErr := repo.Upsert(row); upsertErr != nil {
		test.Fatalf("Upsert: %v", upsertErr)
	}

	var (
		kind   sql.NullString
		source sql.NullString
	)

	scanErr := store.DB().QueryRow(
		`SELECT kind, source FROM nodes WHERE id = ?`, row.ID,
	).Scan(&kind, &source)

	if scanErr != nil {
		test.Fatalf("scan: %v", scanErr)
	}

	if !kind.Valid || kind.String != "file" {
		test.Errorf("kind = %+v, want \"file\"", kind)
	}

	if source.Valid {
		test.Errorf("source = %+v, want NULL", source)
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

	if bulkErr := repo.BulkUpsert(subUnits); bulkErr != nil {
		test.Fatalf("BulkUpsert sub-units: %v", bulkErr)
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

	parent := index.NodeRow{
		ID: "doc", Type: "note", Path: "doc.md", Title: "Doc",
		PropertiesJSON: `{}`, LastChecksum: "h",
	}

	if upsertErr := repo.Upsert(parent); upsertErr != nil {
		test.Fatalf("Upsert parent: %v", upsertErr)
	}

	rows := []index.NodeRow{
		{
			ID: "doc#one", Type: "paragraph", Path: parent.Path, Title: "One",
			PropertiesJSON: `{}`, LastChecksum: "h",
			ParentID:     sql.NullString{String: parent.ID, Valid: true},
			Ordinal:      sql.NullInt64{Int64: 0, Valid: true},
			EmbedPayload: sql.NullString{String: "one body", Valid: true},
		},
		{
			ID: "doc#two", Type: "paragraph", Path: parent.Path, Title: "Two",
			PropertiesJSON: `{}`, LastChecksum: "h",
			ParentID:     sql.NullString{String: parent.ID, Valid: true},
			Ordinal:      sql.NullInt64{Int64: 1, Valid: true},
			EmbedPayload: sql.NullString{String: "two body", Valid: true},
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

	updated, _ := repo.Get("doc#one")

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

	for _, row := range rows {
		if upsertErr := repo.Upsert(row); upsertErr != nil {
			test.Fatalf("seed Upsert %s: %v", row.ID, upsertErr)
		}
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

	if bulkErr := nodes.BulkUpsert([]index.NodeRow{{
		ID: subID, Type: "paragraph", Path: "f.md", Title: "p", PropertiesJSON: `{}`, LastChecksum: "h",
		ParentID: sql.NullString{String: "f", Valid: true},
		Ordinal:  sql.NullInt64{Int64: 0, Valid: true},
	}}); bulkErr != nil {
		test.Fatalf("BulkUpsert sub-unit: %v", bulkErr)
	}

	// Seed an outbound edge and an embedding row on the sub-unit.
	if upsertErr := edges.UpsertAll(subID, "f.md", []index.EdgeRow{
		{Type: "references", SourceID: subID, TargetID: "f", SourcePath: "f.md", Kind: "direct"},
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

// TestNodeRepo_ListSubUnitsForFile_UnderscoreNoLeakage exercises the GLOB
// pattern fix: file paths containing `_` (extremely common in workspaces)
// must not silently match siblings via SQL LIKE's underscore-as-wildcard
// semantics. The regression seeds two files whose ids only differ in a
// single character where one has `_` and the other has a literal space;
// under LIKE both queries would alias the other file's sub-units.
func TestNodeRepo_ListSubUnitsForFile_UnderscoreNoLeakage(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	parents := []index.NodeRow{
		{ID: "notes/foo_a", Type: "note", Path: "notes/foo_a.md", Title: "foo_a", PropertiesJSON: `{}`, LastChecksum: "h"},
		{ID: "notes/foo b", Type: "note", Path: "notes/foo b.md", Title: "foo b", PropertiesJSON: `{}`, LastChecksum: "h"},
	}

	for _, parent := range parents {
		if upsertErr := repo.Upsert(parent); upsertErr != nil {
			test.Fatalf("Upsert parent %s: %v", parent.ID, upsertErr)
		}
	}

	subUnits := []index.NodeRow{
		{
			ID: "notes/foo_a#aaa", Type: "paragraph", Path: "notes/foo_a.md",
			Title: "fooA-a", PropertiesJSON: `{}`, LastChecksum: "h",
			ParentID: sql.NullString{String: "notes/foo_a", Valid: true},
			Ordinal:  sql.NullInt64{Int64: 0, Valid: true},
		},
		{
			ID: "notes/foo_a#bbb", Type: "paragraph", Path: "notes/foo_a.md",
			Title: "fooA-b", PropertiesJSON: `{}`, LastChecksum: "h",
			ParentID: sql.NullString{String: "notes/foo_a", Valid: true},
			Ordinal:  sql.NullInt64{Int64: 1, Valid: true},
		},
		{
			ID: "notes/foo b#xxx", Type: "paragraph", Path: "notes/foo b.md",
			Title: "fooB-x", PropertiesJSON: `{}`, LastChecksum: "h",
			ParentID: sql.NullString{String: "notes/foo b", Valid: true},
			Ordinal:  sql.NullInt64{Int64: 0, Valid: true},
		},
	}

	if bulkErr := repo.BulkUpsert(subUnits); bulkErr != nil {
		test.Fatalf("BulkUpsert sub-units: %v", bulkErr)
	}

	fooA, listErr := repo.ListSubUnitsForFile("notes/foo_a")

	if listErr != nil {
		test.Fatalf("ListSubUnitsForFile foo_a: %v", listErr)
	}

	if len(fooA) != 2 {
		test.Fatalf("foo_a sub-units = %d, want 2 (no leakage from foo b)", len(fooA))
	}

	for _, row := range fooA {
		if row.ParentID.String != "notes/foo_a" {
			test.Errorf("foo_a sub-unit %s parent = %q, want notes/foo_a", row.ID, row.ParentID.String)
		}
	}

	fooB, listErr := repo.ListSubUnitsForFile("notes/foo b")

	if listErr != nil {
		test.Fatalf("ListSubUnitsForFile foo b: %v", listErr)
	}

	if len(fooB) != 1 {
		test.Fatalf("foo b sub-units = %d, want 1", len(fooB))
	}

	if fooB[0].ID != "notes/foo b#xxx" {
		test.Errorf("foo b sub-unit id = %q, want notes/foo b#xxx", fooB[0].ID)
	}
}

// TestNodeRepo_ListSubUnitsForFiles batches multiple file ids in a single
// query and must produce the same union ListSubUnitsForFile would over each
// id, with no LIKE-style underscore aliasing.
func TestNodeRepo_ListSubUnitsForFiles(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	parents := []index.NodeRow{
		{ID: "notes/foo_a", Type: "note", Path: "notes/foo_a.md", Title: "foo_a", PropertiesJSON: `{}`, LastChecksum: "h"},
		{ID: "notes/foo b", Type: "note", Path: "notes/foo b.md", Title: "foo b", PropertiesJSON: `{}`, LastChecksum: "h"},
		{ID: "notes/other", Type: "note", Path: "notes/other.md", Title: "other", PropertiesJSON: `{}`, LastChecksum: "h"},
	}

	for _, parent := range parents {
		if upsertErr := repo.Upsert(parent); upsertErr != nil {
			test.Fatalf("Upsert parent %s: %v", parent.ID, upsertErr)
		}
	}

	subUnits := []index.NodeRow{
		{
			ID: "notes/foo_a#aaa", Type: "paragraph", Path: "notes/foo_a.md",
			PropertiesJSON: `{}`, LastChecksum: "h",
			ParentID: sql.NullString{String: "notes/foo_a", Valid: true},
			Ordinal:  sql.NullInt64{Int64: 0, Valid: true},
		},
		{
			ID: "notes/foo b#xxx", Type: "paragraph", Path: "notes/foo b.md",
			PropertiesJSON: `{}`, LastChecksum: "h",
			ParentID: sql.NullString{String: "notes/foo b", Valid: true},
			Ordinal:  sql.NullInt64{Int64: 0, Valid: true},
		},
		{
			ID: "notes/other#ooo", Type: "paragraph", Path: "notes/other.md",
			PropertiesJSON: `{}`, LastChecksum: "h",
			ParentID: sql.NullString{String: "notes/other", Valid: true},
			Ordinal:  sql.NullInt64{Int64: 0, Valid: true},
		},
	}

	if bulkErr := repo.BulkUpsert(subUnits); bulkErr != nil {
		test.Fatalf("BulkUpsert sub-units: %v", bulkErr)
	}

	// Asking for only foo_a must not include foo b's sub-unit (LIKE would).
	loaded, listErr := repo.ListSubUnitsForFiles([]string{"notes/foo_a"})

	if listErr != nil {
		test.Fatalf("ListSubUnitsForFiles foo_a: %v", listErr)
	}

	if len(loaded) != 1 {
		test.Fatalf("foo_a batched sub-units = %d, want 1 (no leakage)", len(loaded))
	}

	if loaded[0].ID != "notes/foo_a#aaa" {
		test.Errorf("foo_a batched id = %q, want notes/foo_a#aaa", loaded[0].ID)
	}

	// Batched request covering two files returns the union and excludes
	// the unmentioned `other` file.
	loaded, listErr = repo.ListSubUnitsForFiles([]string{"notes/foo_a", "notes/foo b"})

	if listErr != nil {
		test.Fatalf("ListSubUnitsForFiles foo_a+foo b: %v", listErr)
	}

	if len(loaded) != 2 {
		test.Fatalf("batched sub-units = %d, want 2", len(loaded))
	}

	gotIDs := map[string]bool{}

	for _, row := range loaded {
		gotIDs[row.ID] = true
	}

	for _, want := range []string{"notes/foo_a#aaa", "notes/foo b#xxx"} {
		if !gotIDs[want] {
			test.Errorf("missing id %q in batched result %v", want, gotIDs)
		}
	}

	if gotIDs["notes/other#ooo"] {
		test.Errorf("batched result leaked unrelated file: %v", gotIDs)
	}

	// Empty input is a no-op; never executes a query.
	empty, listErr := repo.ListSubUnitsForFiles(nil)

	if listErr != nil {
		test.Fatalf("ListSubUnitsForFiles nil: %v", listErr)
	}

	if len(empty) != 0 {
		test.Errorf("nil input returned %d rows, want 0", len(empty))
	}
}

// TestSQLite_OctetLengthAvailable confirms modernc.org/sqlite supports
// `octet_length()` and that it reports the byte count of UTF-8 text
// (not codepoints). CountOversizeSubUnitPayloads relies on this to
// compare bytes against embed.DefaultMaxBytes, which is a byte
// threshold; `length()` would surface the codepoint count and
// undercount multi-byte UTF-8.
func TestSQLite_OctetLengthAvailable(test *testing.T) {
	store := openTestIndex(test)

	var (
		bytesCount      int
		codepointsCount int
	)

	if scanErr := store.DB().QueryRow("SELECT octet_length('héllo'), length('héllo')").Scan(&bytesCount, &codepointsCount); scanErr != nil {
		test.Fatalf("octet_length probe failed (driver may lack support): %v", scanErr)
	}

	if bytesCount != 6 {
		test.Errorf("octet_length('héllo') = %d, want 6 (é is 2 bytes in UTF-8)", bytesCount)
	}

	if codepointsCount != 5 {
		test.Errorf("length('héllo') = %d, want 5 (codepoint count)", codepointsCount)
	}
}

// TestNodeRepo_CountOversizeSubUnitPayloads_BytesNotCodepoints inserts a
// sub-unit whose embed_payload is 3000 codepoints of multi-byte Cyrillic
// (≈6000 bytes total). A codepoint-based length comparison would report
// 3000 ≤ 4000 and miss the row; CountOversizeSubUnitPayloads must use
// byte length and return 1.
func TestNodeRepo_CountOversizeSubUnitPayloads_BytesNotCodepoints(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	parent := index.NodeRow{
		ID:             "notes/cyrillic",
		Type:           "note",
		Path:           "notes/cyrillic.md",
		Title:          "Cyrillic",
		PropertiesJSON: `{}`,
		LastChecksum:   "h",
	}

	if upsertErr := repo.Upsert(parent); upsertErr != nil {
		test.Fatalf("Upsert parent: %v", upsertErr)
	}

	// Cyrillic "д" (U+0434) is 2 bytes in UTF-8. 3000 codepoints → 6000
	// bytes, which exceeds the 4000-byte threshold but is below the
	// codepoint count.
	multiByteRune := "д"
	payloadRunes := strings.Repeat(multiByteRune, 3000)

	subUnit := index.NodeRow{
		ID:             "notes/cyrillic#bulk",
		Type:           "paragraph",
		Path:           "notes/cyrillic.md",
		Title:          "bulk",
		PropertiesJSON: `{}`,
		LastChecksum:   "h",
		ParentID:       sql.NullString{String: "notes/cyrillic", Valid: true},
		Ordinal:        sql.NullInt64{Int64: 0, Valid: true},
		EmbedPayload:   sql.NullString{String: payloadRunes, Valid: true},
	}

	if upsertErr := repo.BulkUpsert([]index.NodeRow{subUnit}); upsertErr != nil {
		test.Fatalf("BulkUpsert sub-unit: %v", upsertErr)
	}

	count, countErr := repo.CountOversizeSubUnitPayloads(4000)

	if countErr != nil {
		test.Fatalf("CountOversizeSubUnitPayloads: %v", countErr)
	}

	if count != 1 {
		test.Errorf("CountOversizeSubUnitPayloads(4000) = %d, want 1 (3000 codepoints × 2 bytes = 6000 bytes > 4000)", count)
	}

	// Sanity: the same payload measured against an 8000-byte threshold
	// must NOT count, ruling out the row simply tripping any threshold.
	count8000, _ := repo.CountOversizeSubUnitPayloads(8000)

	if count8000 != 0 {
		test.Errorf("CountOversizeSubUnitPayloads(8000) = %d, want 0 (6000 bytes ≤ 8000)", count8000)
	}
}
