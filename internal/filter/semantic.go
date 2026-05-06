package filter

import (
	"sort"

	"github.com/germanamz/tusk/internal/embed"
)

// SemanticCandidate pairs a node id with its embedding vector for ranking.
type SemanticCandidate struct {
	NodeID string
	Vector []float32
}

// ScoredResult is one ranked candidate.
type ScoredResult struct {
	NodeID string
	Score  float64
}

// SemanticRank computes cosine similarity between each candidate's vector and
// queryVector, then returns the candidates sorted by descending score.
// Candidates whose vectors mismatch queryVector's dimension are silently
// skipped (they cannot be ranked).
func SemanticRank(candidates []SemanticCandidate, queryVector []float32) []ScoredResult {
	scored := make([]ScoredResult, 0, len(candidates))

	for _, candidate := range candidates {
		if len(candidate.Vector) != len(queryVector) {
			continue
		}

		score := embed.CosineSimilarity(candidate.Vector, queryVector)
		scored = append(scored, ScoredResult{NodeID: candidate.NodeID, Score: score})
	}

	sort.Slice(scored, func(left, right int) bool {
		return scored[left].Score > scored[right].Score
	})

	return scored
}
