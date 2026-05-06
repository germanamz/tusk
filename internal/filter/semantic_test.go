package filter_test

import (
	"testing"

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
