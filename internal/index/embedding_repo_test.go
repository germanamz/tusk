package index_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

// newTestEmbeddingRepo opens a fresh index, seeds the provided node ids
// (required since the P2 migration added a foreign key from
// embeddings.node_id to nodes.id), and returns an EmbeddingRepo against
// it.
func newTestEmbeddingRepo(test *testing.T, nodeIDs ...string) *index.EmbeddingRepo {
	test.Helper()

	store := openTestIndex(test)

	if len(nodeIDs) > 0 {
		seedNodes(test, store, nodeIDs...)
	}

	return index.NewEmbeddingRepo(store)
}

func TestEmbeddingRepo_UpsertAndGet(test *testing.T) {
	repo := newTestEmbeddingRepo(test, "tickets/foo")

	row := index.EmbeddingRow{
		NodeID:      "tickets/foo",
		ChunkIdx:    0,
		Model:       "nomic-embed-text",
		ContentHash: "abc123",
		Vector:      []float32{0.1, 0.2, 0.3},
		Dim:         3,
	}

	if upsertErr := repo.Upsert(row); upsertErr != nil {
		test.Fatalf("Upsert: %v", upsertErr)
	}

	loaded, getErr := repo.GetByNodeID("tickets/foo")

	if getErr != nil {
		test.Fatalf("GetByNodeID: %v", getErr)
	}

	if len(loaded) != 1 {
		test.Fatalf("len = %d, want 1", len(loaded))
	}

	if !reflect.DeepEqual(loaded[0].Vector, []float32{0.1, 0.2, 0.3}) {
		test.Errorf("Vector = %v, want [0.1 0.2 0.3]", loaded[0].Vector)
	}

	if loaded[0].Dim != 3 {
		test.Errorf("Dim = %d", loaded[0].Dim)
	}
}

func TestEmbeddingRepo_UpsertReplacesByContentHash(test *testing.T) {
	repo := newTestEmbeddingRepo(test, "x")

	first := index.EmbeddingRow{
		NodeID: "x", ChunkIdx: 0, Model: "m", ContentHash: "h1",
		Vector: []float32{0.1}, Dim: 1,
	}

	if upsertErr := repo.Upsert(first); upsertErr != nil {
		test.Fatalf("first: %v", upsertErr)
	}

	second := index.EmbeddingRow{
		NodeID: "x", ChunkIdx: 0, Model: "m", ContentHash: "h2",
		Vector: []float32{0.5}, Dim: 1,
	}

	if upsertErr := repo.Upsert(second); upsertErr != nil {
		test.Fatalf("second: %v", upsertErr)
	}

	loaded, _ := repo.GetByNodeID("x")

	if len(loaded) != 1 {
		test.Fatalf("len = %d, want 1 after replace", len(loaded))
	}

	if loaded[0].ContentHash != "h2" {
		test.Errorf("ContentHash = %q, want h2", loaded[0].ContentHash)
	}
}

func TestEmbeddingRepo_Upsert_AllowsMultipleChunksPerNode(test *testing.T) {
	repo := newTestEmbeddingRepo(test, "n")

	base := index.EmbeddingRow{
		NodeID: "n", Model: "m", ContentHash: "h", Vector: []float32{0.1}, Dim: 1,
	}

	base.ChunkIdx = 0

	if err := repo.Upsert(base); err != nil {
		test.Fatalf("upsert chunk 0: %v", err)
	}

	base.ChunkIdx = 1
	base.ContentHash = "h2"

	if err := repo.Upsert(base); err != nil {
		test.Fatalf("upsert chunk 1: %v", err)
	}

	rows, getErr := repo.GetByNodeID("n")

	if getErr != nil {
		test.Fatalf("GetByNodeID: %v", getErr)
	}

	if len(rows) != 2 {
		test.Errorf("rows = %d, want 2 (UNIQUE(node_id, chunk_idx) keeps both)", len(rows))
	}
}

func TestEmbeddingRepo_ListByNodeIDs(test *testing.T) {
	repo := newTestEmbeddingRepo(test, "a", "b", "c")

	for _, row := range []index.EmbeddingRow{
		{NodeID: "a", ChunkIdx: 0, Model: "m", ContentHash: "h", Vector: []float32{0.1}, Dim: 1},
		{NodeID: "b", ChunkIdx: 0, Model: "m", ContentHash: "h", Vector: []float32{0.2}, Dim: 1},
		{NodeID: "c", ChunkIdx: 0, Model: "m", ContentHash: "h", Vector: []float32{0.3}, Dim: 1},
	} {
		repo.Upsert(row)
	}

	loaded, listErr := repo.ListByNodeIDs([]string{"a", "c"})

	if listErr != nil {
		test.Fatalf("ListByNodeIDs: %v", listErr)
	}

	if len(loaded) != 2 {
		test.Errorf("len = %d, want 2", len(loaded))
	}
}

func TestEmbeddingRepo_DeleteByNodeID(test *testing.T) {
	repo := newTestEmbeddingRepo(test, "doomed")

	repo.Upsert(index.EmbeddingRow{
		NodeID: "doomed", ChunkIdx: 0, Model: "m", ContentHash: "h",
		Vector: []float32{0.1}, Dim: 1,
	})

	if deleteErr := repo.DeleteByNodeID("doomed"); deleteErr != nil {
		test.Fatalf("DeleteByNodeID: %v", deleteErr)
	}

	loaded, _ := repo.GetByNodeID("doomed")

	if len(loaded) != 0 {
		test.Errorf("len = %d, want 0", len(loaded))
	}
}

func TestEmbeddingRepo_BodyRoundTrip(test *testing.T) {
	repo := newTestEmbeddingRepo(test, "tickets/foo")

	row := index.EmbeddingRow{
		NodeID:      "tickets/foo",
		ChunkIdx:    0,
		Model:       "nomic-embed-text",
		ContentHash: "h1",
		Vector:      []float32{0.1},
		Dim:         1,
		Body:        "# Heading\n\nFirst paragraph of the chunk.",
	}

	if upsertErr := repo.Upsert(row); upsertErr != nil {
		test.Fatalf("Upsert: %v", upsertErr)
	}

	loaded, getErr := repo.GetByNodeID("tickets/foo")

	if getErr != nil {
		test.Fatalf("GetByNodeID: %v", getErr)
	}

	if len(loaded) != 1 {
		test.Fatalf("len = %d, want 1", len(loaded))
	}

	if loaded[0].Body != row.Body {
		test.Errorf("Body = %q, want %q", loaded[0].Body, row.Body)
	}
}

func TestEmbeddingRepo_ListNodeIDs(test *testing.T) {
	repo := newTestEmbeddingRepo(test, "a/one", "b/two", "c/three")

	// P2 schema makes embeddings unique by node_id; re-upserting the
	// same id replaces in place, so a single upsert per id is enough to
	// cover the listing path.
	rows := []index.EmbeddingRow{
		{NodeID: "b/two", ChunkIdx: 0, Model: "m", ContentHash: "h", Vector: []float32{0.1}, Dim: 1},
		{NodeID: "a/one", ChunkIdx: 0, Model: "m", ContentHash: "h", Vector: []float32{0.1}, Dim: 1},
		{NodeID: "c/three", ChunkIdx: 0, Model: "m", ContentHash: "h", Vector: []float32{0.1}, Dim: 1},
	}

	for _, row := range rows {
		if upsertErr := repo.Upsert(row); upsertErr != nil {
			test.Fatalf("Upsert %s/%d: %v", row.NodeID, row.ChunkIdx, upsertErr)
		}
	}

	ids, listErr := repo.ListNodeIDs()

	if listErr != nil {
		test.Fatalf("ListNodeIDs: %v", listErr)
	}

	want := []string{"a/one", "b/two", "c/three"}

	if !reflect.DeepEqual(ids, want) {
		test.Errorf("ListNodeIDs = %v, want %v", ids, want)
	}
}

func TestEmbeddingRepo_ListNodeIDs_Empty(test *testing.T) {
	repo := newTestEmbeddingRepo(test)

	ids, listErr := repo.ListNodeIDs()

	if listErr != nil {
		test.Fatalf("ListNodeIDs: %v", listErr)
	}

	if len(ids) != 0 {
		test.Errorf("ListNodeIDs = %v, want empty", ids)
	}
}

func TestEmbeddingRepo_Stats_Empty(test *testing.T) {
	repo := newTestEmbeddingRepo(test)

	stats, statsErr := repo.Stats(3600)

	if statsErr != nil {
		test.Fatalf("Stats: %v", statsErr)
	}

	if stats.TotalNodes != 0 || stats.TotalChunks != 0 || stats.MaxChunks != 0 {
		test.Errorf("Stats empty case: %+v", stats)
	}

	if stats.MeanChunks != 0 {
		test.Errorf("MeanChunks empty = %v, want 0", stats.MeanChunks)
	}

	if len(stats.TopByChunks) != 0 || len(stats.LargeChunks) != 0 {
		test.Errorf("TopByChunks/LargeChunks empty case: %+v", stats)
	}
}

func TestEmbeddingRepo_Stats_Aggregates(test *testing.T) {
	nodeIDs := []string{"a", "b", "c", "d"}
	repo := newTestEmbeddingRepo(test, nodeIDs...)

	insert := func(nodeID string, bodyLen int) {
		body := strings.Repeat("x", bodyLen)
		row := index.EmbeddingRow{
			NodeID:      nodeID,
			ChunkIdx:    0,
			Model:       "m",
			ContentHash: "h-" + nodeID, // distinct content per node (no sharing)
			Vector:      []float32{0.1},
			Dim:         1,
			Body:        body,
		}

		if upsertErr := repo.Upsert(row); upsertErr != nil {
			test.Fatalf("Upsert %s: %v", nodeID, upsertErr)
		}
	}

	insert("a", 500)
	insert("b", 100)
	insert("c", 3700)
	insert("d", 100)

	stats, statsErr := repo.Stats(3600)

	if statsErr != nil {
		test.Fatalf("Stats: %v", statsErr)
	}

	if stats.TotalNodes != 4 {
		test.Errorf("TotalNodes = %d, want 4", stats.TotalNodes)
	}

	if stats.TotalChunks != 4 {
		test.Errorf("TotalChunks = %d, want 4", stats.TotalChunks)
	}

	if stats.MaxChunks != 1 {
		test.Errorf("MaxChunks = %d, want 1", stats.MaxChunks)
	}

	if stats.MedianChunks != 1 {
		test.Errorf("MedianChunks = %d, want 1", stats.MedianChunks)
	}

	if stats.MeanChunks != 1.0 {
		test.Errorf("MeanChunks = %v, want 1.0", stats.MeanChunks)
	}

	if len(stats.TopByChunks) != 4 {
		test.Errorf("TopByChunks = %+v", stats.TopByChunks)
	}

	// 1 large chunk expected (c, body=3700 >= 3600)
	if len(stats.LargeChunks) != 1 {
		test.Errorf("LargeChunks count = %d, want 1", len(stats.LargeChunks))
	}

	for _, large := range stats.LargeChunks {
		if large.NodeID != "c" || large.BodyLen != 3700 {
			test.Errorf("LargeChunks entry %+v", large)
		}
	}
}

// TestEmbeddingRepo_ListSubUnitsForFiles_UnderscoreNoLeakage pins the GLOB
// pattern fix: with SQL LIKE, `notes/foo_a` would match `notes/foo b`'s
// sub-units because `_` is a LIKE wildcard. GLOB only honors `*` and `?`,
// neither of which can appear in a workspace file id.
func TestEmbeddingRepo_ListSubUnitsForFiles_UnderscoreNoLeakage(test *testing.T) {
	repo := newTestEmbeddingRepo(test,
		"notes/foo_a",
		"notes/foo b",
		"notes/foo_a#aaa",
		"notes/foo b#xxx",
	)

	rows := []index.EmbeddingRow{
		{NodeID: "notes/foo_a#aaa", ChunkIdx: 0, Model: "m", ContentHash: "h1", Vector: []float32{0.1}, Dim: 1},
		{NodeID: "notes/foo b#xxx", ChunkIdx: 0, Model: "m", ContentHash: "h2", Vector: []float32{0.2}, Dim: 1},
	}

	for _, row := range rows {
		if upsertErr := repo.Upsert(row); upsertErr != nil {
			test.Fatalf("Upsert %s: %v", row.NodeID, upsertErr)
		}
	}

	loaded, listErr := repo.ListSubUnitsForFiles([]string{"notes/foo_a"})

	if listErr != nil {
		test.Fatalf("ListSubUnitsForFiles foo_a: %v", listErr)
	}

	if len(loaded) != 1 {
		test.Fatalf("foo_a embeddings = %d, want 1 (no leakage from foo b)", len(loaded))
	}

	if loaded[0].NodeID != "notes/foo_a#aaa" {
		test.Errorf("foo_a embedding id = %q, want notes/foo_a#aaa", loaded[0].NodeID)
	}
}

func TestEmbeddingRepo_GCOrphanVectors(test *testing.T) {
	store := openTestIndex(test)
	seedNodes(test, store, "notes/a#S1P1", "notes/b#S1P1")
	repo := index.NewEmbeddingRepo(store)

	if upsertErr := repo.Upsert(index.EmbeddingRow{
		NodeID: "notes/a#S1P1", ChunkIdx: 0, Model: "m", ContentHash: "h1",
		Vector: []float32{0.1}, Dim: 1,
	}); upsertErr != nil {
		test.Fatalf("upsert a: %v", upsertErr)
	}

	if upsertErr := repo.Upsert(index.EmbeddingRow{
		NodeID: "notes/b#S1P1", ChunkIdx: 0, Model: "m", ContentHash: "h2",
		Vector: []float32{0.2}, Dim: 1,
	}); upsertErr != nil {
		test.Fatalf("upsert b: %v", upsertErr)
	}

	// Drop a's mapping: h1's vector is now orphaned; h2 is still referenced.
	if deleteErr := repo.DeleteByNodeID("notes/a#S1P1"); deleteErr != nil {
		test.Fatalf("delete a: %v", deleteErr)
	}

	removed, gcErr := repo.GCOrphanVectors()

	if gcErr != nil {
		test.Fatalf("GCOrphanVectors: %v", gcErr)
	}

	if removed != 1 {
		test.Fatalf("gc removed %d, want 1", removed)
	}

	var surviving int

	if scanErr := store.DB().QueryRow(`SELECT COUNT(*) FROM embeddings`).Scan(&surviving); scanErr != nil {
		test.Fatalf("count embeddings: %v", scanErr)
	}

	if surviving != 1 {
		test.Fatalf("expected 1 surviving vector (h2), got %d", surviving)
	}

	// The still-referenced node keeps resolving its vector.
	second, _ := repo.GetByNodeID("notes/b#S1P1")

	if len(second) != 1 {
		test.Fatalf("b should still resolve its vector, got %d", len(second))
	}
}

func TestEmbeddingRepo_DedupeByContentHash(test *testing.T) {
	store := openTestIndex(test)
	seedNodes(test, store, "notes/a#S1P1", "notes/b#S1P1")
	repo := index.NewEmbeddingRepo(store)

	vec := []float32{0.1, 0.2}

	for _, id := range []string{"notes/a#S1P1", "notes/b#S1P1"} {
		if upsertErr := repo.Upsert(index.EmbeddingRow{
			NodeID: id, ChunkIdx: 0, Model: "m", ContentHash: "shared",
			Vector: vec, Dim: 2, Body: "x",
		}); upsertErr != nil {
			test.Fatalf("Upsert %s: %v", id, upsertErr)
		}
	}

	// Identical content across two nodes is stored as a single shared vector.
	var vectorRows int

	if scanErr := store.DB().QueryRow(`SELECT COUNT(*) FROM embeddings`).Scan(&vectorRows); scanErr != nil {
		test.Fatalf("count embeddings: %v", scanErr)
	}

	if vectorRows != 1 {
		test.Fatalf("expected 1 shared vector row, got %d", vectorRows)
	}

	// ...but each node still resolves its vector through the junction.
	first, _ := repo.GetByNodeID("notes/a#S1P1")
	second, _ := repo.GetByNodeID("notes/b#S1P1")

	if len(first) != 1 || len(second) != 1 {
		test.Fatalf("each node resolves its vector: a=%d b=%d", len(first), len(second))
	}

	if first[0].ContentHash != "shared" || second[0].ContentHash != "shared" {
		test.Errorf("content hash mismatch: a=%q b=%q", first[0].ContentHash, second[0].ContentHash)
	}
}
