package index_test

import (
	"reflect"
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
