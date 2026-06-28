package index

import (
	"path/filepath"
	"testing"
)

// TestListByIDs_ChunksLargeIDSets shrinks the IN-variable cap so a small id set
// still spans multiple chunks, proving the chunked query returns every row in
// the original sorted order (and never builds one oversized IN clause).
func TestListByIDs_ChunksLargeIDSets(test *testing.T) {
	store, openErr := Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	nodes := NewNodeRepo(store)
	embeddings := NewEmbeddingRepo(store)

	ids := []string{"n0", "n1", "n2", "n3", "n4"}

	for _, id := range ids {
		if upsertErr := nodes.Upsert(NodeRow{ID: id, Type: "note", Path: id + ".md", Title: id, PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
			test.Fatalf("upsert %s: %v", id, upsertErr)
		}

		if embErr := embeddings.Upsert(EmbeddingRow{NodeID: id, ChunkIdx: 0, ContentHash: "h-" + id, Model: "stub", Vector: []float32{0.1, 0.2, 0.3}, Dim: 3}); embErr != nil {
			test.Fatalf("embed upsert %s: %v", id, embErr)
		}
	}

	original := maxInVariables
	maxInVariables = 2

	defer func() { maxInVariables = original }()

	scrambled := []string{"n3", "n0", "n4", "n1", "n2"}

	nodeRows, listErr := nodes.ListByIDs(scrambled)

	if listErr != nil {
		test.Fatalf("ListByIDs: %v", listErr)
	}

	gotNodeIDs := make([]string, len(nodeRows))

	for idx, row := range nodeRows {
		gotNodeIDs[idx] = row.ID
	}

	wantSorted := []string{"n0", "n1", "n2", "n3", "n4"}

	if len(gotNodeIDs) != len(wantSorted) {
		test.Fatalf("ListByIDs returned %v, want %v", gotNodeIDs, wantSorted)
	}

	for idx := range wantSorted {
		if gotNodeIDs[idx] != wantSorted[idx] {
			test.Fatalf("ListByIDs order = %v, want sorted %v", gotNodeIDs, wantSorted)
		}
	}

	embRows, embListErr := embeddings.ListByNodeIDs(scrambled)

	if embListErr != nil {
		test.Fatalf("ListByNodeIDs: %v", embListErr)
	}

	if len(embRows) != len(wantSorted) {
		test.Fatalf("ListByNodeIDs returned %d rows, want %d", len(embRows), len(wantSorted))
	}

	for idx := 1; idx < len(embRows); idx++ {
		if embRows[idx-1].NodeID > embRows[idx].NodeID {
			test.Fatalf("ListByNodeIDs not ordered by node_id: %s before %s", embRows[idx-1].NodeID, embRows[idx].NodeID)
		}
	}
}
