package filter

import (
	"sort"

	"github.com/germanamz/tusk/internal/embed"
)

// SemanticCandidate pairs a node's chunk vector with its ids for ranking.
// ChunkIdx is used internally to disambiguate multiple chunks per node and
// to give downstream code (e.g. snippet generation) a hook to identify which
// chunk matched best.
type SemanticCandidate struct {
	NodeID   string
	ChunkIdx int
	Vector   []float32
}

// ScoredResult is one ranked node. Score is the max cosine similarity across
// the node's chunks.
type ScoredResult struct {
	NodeID string
	Score  float64
}

// SemanticRank scores each candidate by cosine similarity to queryVector and
// returns one row per node, with Score equal to the maximum chunk score for
// that node. Results are sorted by score descending, ties broken by NodeID
// ascending for determinism. Candidates whose vectors mismatch queryVector's
// dimension are silently skipped.
func SemanticRank(candidates []SemanticCandidate, queryVector []float32) []ScoredResult {
	bestByNode := make(map[string]float64, len(candidates))

	for _, candidate := range candidates {
		if len(candidate.Vector) != len(queryVector) {
			continue
		}

		score := embed.CosineSimilarity(candidate.Vector, queryVector)

		if prev, present := bestByNode[candidate.NodeID]; !present || score > prev {
			bestByNode[candidate.NodeID] = score
		}
	}

	scored := make([]ScoredResult, 0, len(bestByNode))

	for nodeID, score := range bestByNode {
		scored = append(scored, ScoredResult{NodeID: nodeID, Score: score})
	}

	sort.Slice(scored, func(left, right int) bool {
		if scored[left].Score == scored[right].Score {
			return scored[left].NodeID < scored[right].NodeID
		}

		return scored[left].Score > scored[right].Score
	})

	return scored
}
