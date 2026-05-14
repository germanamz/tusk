package index_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func newTestEmbeddingRepo(test *testing.T) *index.EmbeddingRepo {
	test.Helper()

	store := openTestIndex(test)

	return index.NewEmbeddingRepo(store)
}

func TestEmbeddingRepo_UpsertAndGet(test *testing.T) {
	repo := newTestEmbeddingRepo(test)

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
	repo := newTestEmbeddingRepo(test)

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
	repo := newTestEmbeddingRepo(test)

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
	repo := newTestEmbeddingRepo(test)

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
	repo := newTestEmbeddingRepo(test)

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
	repo := newTestEmbeddingRepo(test)

	rows := []index.EmbeddingRow{
		{NodeID: "b/two", ChunkIdx: 0, Model: "m", ContentHash: "h", Vector: []float32{0.1}, Dim: 1},
		{NodeID: "a/one", ChunkIdx: 0, Model: "m", ContentHash: "h", Vector: []float32{0.1}, Dim: 1},
		{NodeID: "a/one", ChunkIdx: 1, Model: "m", ContentHash: "h", Vector: []float32{0.1}, Dim: 1},
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
	repo := newTestEmbeddingRepo(test)

	insert := func(nodeID string, count int, bodyLen int) {
		body := strings.Repeat("x", bodyLen)
		for chunkIdx := 0; chunkIdx < count; chunkIdx++ {
			row := index.EmbeddingRow{
				NodeID:      nodeID,
				ChunkIdx:    chunkIdx,
				Model:       "m",
				ContentHash: "h",
				Vector:      []float32{0.1},
				Dim:         1,
				Body:        body,
			}

			if upsertErr := repo.Upsert(row); upsertErr != nil {
				test.Fatalf("Upsert %s/%d: %v", nodeID, chunkIdx, upsertErr)
			}
		}
	}

	// node a: 1 chunk @ 500 bytes
	// node b: 3 chunks @ 100 bytes
	// node c: 5 chunks @ 3700 bytes (large)
	// node d: 2 chunks @ 100 bytes
	insert("a", 1, 500)
	insert("b", 3, 100)
	insert("c", 5, 3700)
	insert("d", 2, 100)

	stats, statsErr := repo.Stats(3600)

	if statsErr != nil {
		test.Fatalf("Stats: %v", statsErr)
	}

	if stats.TotalNodes != 4 {
		test.Errorf("TotalNodes = %d, want 4", stats.TotalNodes)
	}

	if stats.TotalChunks != 11 {
		test.Errorf("TotalChunks = %d, want 11", stats.TotalChunks)
	}

	if stats.MaxChunks != 5 {
		test.Errorf("MaxChunks = %d, want 5", stats.MaxChunks)
	}

	// per-node chunk counts: [1, 3, 5, 2] → sorted [1, 2, 3, 5] → median = (2+3)/2 = 2 (integer floor)
	if stats.MedianChunks != 2 {
		test.Errorf("MedianChunks = %d, want 2", stats.MedianChunks)
	}

	wantMean := 11.0 / 4.0
	if stats.MeanChunks != wantMean {
		test.Errorf("MeanChunks = %v, want %v", stats.MeanChunks, wantMean)
	}

	if len(stats.TopByChunks) != 4 || stats.TopByChunks[0].NodeID != "c" || stats.TopByChunks[0].Chunks != 5 {
		test.Errorf("TopByChunks = %+v", stats.TopByChunks)
	}

	// 5 large chunks expected (all of c, body=3700 >= 3600)
	if len(stats.LargeChunks) != 5 {
		test.Errorf("LargeChunks count = %d, want 5", len(stats.LargeChunks))
	}

	for _, large := range stats.LargeChunks {
		if large.NodeID != "c" || large.BodyLen != 3700 {
			test.Errorf("LargeChunks entry %+v", large)
		}
	}
}
