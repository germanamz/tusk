package filter_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/filter"
)

func TestSemanticRank_OrdersByDescendingCosine(test *testing.T) {
	candidates := []filter.SemanticCandidate{
		{NodeID: "far", Vector: []float32{0, 1, 0}},
		{NodeID: "close", Vector: []float32{1, 0, 0}},
		{NodeID: "medium", Vector: []float32{0.7, 0.7, 0}},
	}

	query := []float32{1, 0, 0}

	ranked := filter.SemanticRank(candidates, query)

	if len(ranked) != 3 {
		test.Fatalf("len = %d", len(ranked))
	}

	if ranked[0].NodeID != "close" {
		test.Errorf("ranked[0] = %q, want close", ranked[0].NodeID)
	}

	if ranked[len(ranked)-1].NodeID != "far" {
		test.Errorf("last = %q, want far", ranked[len(ranked)-1].NodeID)
	}
}

func TestSemanticRank_HandlesEmptyCandidates(test *testing.T) {
	ranked := filter.SemanticRank(nil, []float32{1, 0})

	if len(ranked) != 0 {
		test.Errorf("len = %d", len(ranked))
	}
}

func TestSemanticRank_SkipsCandidatesWithMismatchedDim(test *testing.T) {
	candidates := []filter.SemanticCandidate{
		{NodeID: "good", Vector: []float32{1, 0}},
		{NodeID: "bad", Vector: []float32{1, 0, 0}},
	}

	ranked := filter.SemanticRank(candidates, []float32{1, 0})

	if len(ranked) != 1 || ranked[0].NodeID != "good" {
		test.Errorf("ranked = %+v, want only good", ranked)
	}
}

func TestSemanticRank_MaxPerNodeAcrossChunks(test *testing.T) {
	candidates := []filter.SemanticCandidate{
		{NodeID: "alpha", ChunkIdx: 0, Vector: []float32{0.1, 0, 0}},   // weak
		{NodeID: "alpha", ChunkIdx: 1, Vector: []float32{1, 0, 0}},     // strong — should win for alpha
		{NodeID: "bravo", ChunkIdx: 0, Vector: []float32{0.5, 0.5, 0}}, // medium
		{NodeID: "bravo", ChunkIdx: 1, Vector: []float32{0.6, 0.5, 0}}, // slightly stronger
	}

	ranked := filter.SemanticRank(candidates, []float32{1, 0, 0})

	if len(ranked) != 2 {
		test.Fatalf("expected 2 unique nodes, got %d: %+v", len(ranked), ranked)
	}

	if ranked[0].NodeID != "alpha" {
		test.Errorf("alpha's strong chunk should rank first; got %+v", ranked)
	}

	// Bravo's score must equal the higher of its two chunks (chunk 1, not chunk 0).
	chunk1Score := embed.CosineSimilarity([]float32{0.6, 0.5, 0}, []float32{1, 0, 0})

	for _, result := range ranked {
		if result.NodeID == "bravo" && result.Score != chunk1Score {
			test.Errorf("bravo.Score = %v, want %v (max-per-node)", result.Score, chunk1Score)
		}
	}
}

func TestSemanticRank_DeterministicTieBreakByNodeID(test *testing.T) {
	candidates := []filter.SemanticCandidate{
		{NodeID: "zebra", ChunkIdx: 0, Vector: []float32{1, 0, 0}},
		{NodeID: "apple", ChunkIdx: 0, Vector: []float32{1, 0, 0}},
		{NodeID: "mango", ChunkIdx: 0, Vector: []float32{1, 0, 0}},
	}

	ranked := filter.SemanticRank(candidates, []float32{1, 0, 0})

	if len(ranked) != 3 {
		test.Fatalf("len = %d", len(ranked))
	}

	if ranked[0].NodeID != "apple" || ranked[1].NodeID != "mango" || ranked[2].NodeID != "zebra" {
		test.Errorf("equal scores should sort by NodeID ascending; got %+v", ranked)
	}
}
