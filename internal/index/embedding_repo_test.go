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
	// P2 made embeddings UNIQUE(node_id), so the "N chunks per node"
	// fixtures that pre-dated the migration now collapse to one row per
	// node. The aggregate stats still exercise the same code paths —
	// per-node count, median, max, large-chunk detection — just with the
	// new "always one chunk per node" reality. Task 4 may later restore
	// "many chunks per file" semantics by inserting one node per
	// sub-unit; this test then exercises that path automatically.
	nodeIDs := []string{"a", "b", "c", "d"}
	repo := newTestEmbeddingRepo(test, nodeIDs...)

	insert := func(nodeID string, bodyLen int) {
		body := strings.Repeat("x", bodyLen)
		row := index.EmbeddingRow{
			NodeID:      nodeID,
			ChunkIdx:    0,
			Model:       "m",
			ContentHash: "h",
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
		test.Errorf("TotalChunks = %d, want 4 (one per node under UNIQUE(node_id))", stats.TotalChunks)
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
