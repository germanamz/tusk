package filter

import (
	"sort"

	"github.com/germanamz/tusk/internal/embed"
)

// SemanticCandidate pairs a node's chunk vector with its ids for ranking.
// Body carries the chunk's body text (no header prefix) so renderers can
// produce a snippet of the highest-scoring chunk per node.
type SemanticCandidate struct {
	NodeID   string
	ChunkIdx int
	Vector   []float32
	Body     string
}

// ScoredResult is one ranked node. Score is the max cosine similarity across
// the node's chunks. BestChunkIdx and BestChunkBody identify and carry the
// body of the chunk that produced Score.
type ScoredResult struct {
	NodeID        string
	Score         float64
	BestChunkIdx  int
	BestChunkBody string
}

// SemanticRank scores each candidate by cosine similarity to queryVector and
// returns one row per node, with Score equal to the maximum chunk score for
// that node. BestChunkIdx and BestChunkBody come from the chunk that produced
// the max. Results are sorted by score descending, ties broken by NodeID
// ascending. Candidates whose vectors mismatch queryVector's dimension are
// silently skipped.
func SemanticRank(candidates []SemanticCandidate, queryVector []float32) []ScoredResult {
	type bestEntry struct {
		score    float64
		chunkIdx int
		body     string
	}

	bestByNode := make(map[string]bestEntry, len(candidates))

	for _, candidate := range candidates {
		if len(candidate.Vector) != len(queryVector) {
			continue
		}

		score := embed.CosineSimilarity(candidate.Vector, queryVector)

		prev, present := bestByNode[candidate.NodeID]

		if !present || score > prev.score {
			bestByNode[candidate.NodeID] = bestEntry{
				score:    score,
				chunkIdx: candidate.ChunkIdx,
				body:     candidate.Body,
			}
		}
	}

	scored := make([]ScoredResult, 0, len(bestByNode))

	for nodeID, entry := range bestByNode {
		scored = append(scored, ScoredResult{
			NodeID:        nodeID,
			Score:         entry.score,
			BestChunkIdx:  entry.chunkIdx,
			BestChunkBody: entry.body,
		})
	}

	sort.Slice(scored, func(left, right int) bool {
		if scored[left].Score == scored[right].Score {
			return scored[left].NodeID < scored[right].NodeID
		}

		return scored[left].Score > scored[right].Score
	})

	return scored
}

// RenderSnippet returns the leading maxRunes runes of body with internal
// whitespace runs collapsed to single spaces, trailing whitespace stripped,
// and an ellipsis (U+2026) appended when truncation occurred. Returns the
// empty string for empty input or when only whitespace remains.
func RenderSnippet(body string, maxRunes int) string {
	if maxRunes <= 0 || len(body) == 0 {
		return ""
	}

	var (
		builder       []rune
		previousSpace bool
		truncated     bool
	)

	for _, ch := range body {
		if ch == '\n' || ch == '\r' || ch == '\t' || ch == ' ' {
			if len(builder) == 0 {
				// Skip leading whitespace.
				continue
			}

			if previousSpace {
				continue
			}

			if len(builder) >= maxRunes {
				truncated = true
				break
			}

			builder = append(builder, ' ')
			previousSpace = true

			continue
		}

		if len(builder) >= maxRunes {
			truncated = true
			break
		}

		builder = append(builder, ch)
		previousSpace = false
	}

	// Strip trailing space (only one possible because we collapsed).
	for len(builder) > 0 && builder[len(builder)-1] == ' ' {
		builder = builder[:len(builder)-1]
	}

	if len(builder) == 0 {
		return ""
	}

	result := string(builder)

	if truncated {
		result += "…"
	}

	return result
}
