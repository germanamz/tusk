package query

import (
	"context"
	"fmt"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/graphexpand"
	"github.com/germanamz/tusk/internal/typeref"
)

// blendedTrace is the per-node scoring breakdown captured when graph expansion
// runs: the bare cosine, the graph contribution, the blended final, and the hop
// distance from the seed set. It maps 1:1 onto graphexpand.Scored and feeds the
// Explain-trace fields on ScoredRow and MatchedUnit.
type blendedTrace struct {
	Cosine   float64
	Graph    float64
	Final    float64
	Distance int
}

// expandAndBlend runs graph expansion over the cosine-ranked candidates and
// returns the re-ranked candidate list (each row's Score replaced by its
// blended FinalScore) plus a per-node trace map. When graph expansion is
// inactive it returns (ranked, nil, nil) unchanged, so callers gate Explain
// output on a non-nil trace map. Used by both the file-level (query_service.go)
// and leaf-level (semantic_subunits.go) semantic ranking paths.
func expandAndBlend(ctx context.Context, deps Deps, req Request, ranked []filter.ScoredResult) ([]filter.ScoredResult, map[string]blendedTrace, error) {
	if req.GraphExpansion == nil || !req.GraphExpansion.Enabled {
		return ranked, nil, nil
	}

	if deps.Edges == nil {
		return nil, nil, fmt.Errorf("query: graph expansion requires Edges in Deps")
	}

	// K = take * candidate-multiplier (spec §6.2). When the caller left Take
	// unset (0), use SemanticDefaultTake as the basis; when both are 0 — CLI
	// "return all ranked" mode — cap K at len(ranked) so the entire candidate
	// pool is used as seed material.
	baseTake := req.Take

	if baseTake <= 0 {
		baseTake = req.SemanticDefaultTake
	}

	seedLimit := baseTake * req.GraphExpansion.CandidateMultiplier

	if baseTake == 0 || seedLimit <= 0 || seedLimit > len(ranked) {
		seedLimit = len(ranked)
	}

	seedScores := make(map[string]float64, seedLimit)
	seedCandidates := make([]graphexpand.Candidate, 0, seedLimit)

	for index := 0; index < seedLimit; index++ {
		scoredResult := ranked[index]
		// Clip to [0,1] when seeding so the walker / blender see a canonical
		// positive cosine range (mirrors blend.clipUnit).
		clipped := scoredResult.Score

		if clipped < 0 {
			clipped = 0
		}

		seedScores[scoredResult.NodeID] = clipped
		seedCandidates = append(seedCandidates, graphexpand.Candidate{
			NodeID:      scoredResult.NodeID,
			CosineScore: clipped,
			Distance:    0,
		})
	}

	edgeRefs, parseErr := typeref.ParseMany(req.GraphExpansion.EdgeTypes)

	if parseErr != nil {
		return nil, nil, fmt.Errorf("query: graph expansion parse edge types: %w", parseErr)
	}

	walker := graphexpand.NewWalker(deps.Edges, edgeRefs, req.GraphExpansion.Hops)

	walkedCandidates, walkedEdges, walkErr := walker.Expand(ctx, seedCandidates)

	if walkErr != nil {
		return nil, nil, fmt.Errorf("query: graph expansion walk: %w", walkErr)
	}

	blender := graphexpand.Blender{Weight: req.GraphExpansion.Weight}
	blendedRows := blender.Score(walkedCandidates, walkedEdges, seedScores)

	blendedByID := make(map[string]blendedTrace, len(blendedRows))

	// Existing chunk-body lookups assume one ScoredResult per node id. Build an
	// index over the original rank so we can carry the best chunk body / score
	// back into the new ordering. Walked neighbors not present in the original
	// rank get an empty BestChunkBody and score 0 (they had no embedding); the
	// blender already filled in the graph-derived score.
	rankByID := make(map[string]filter.ScoredResult, len(ranked))

	for _, scoredResult := range ranked {
		rankByID[scoredResult.NodeID] = scoredResult
	}

	newRanked := make([]filter.ScoredResult, 0, len(blendedRows))

	for _, blended := range blendedRows {
		blendedByID[blended.NodeID] = blendedTrace{
			Cosine:   blended.CosineScore,
			Graph:    blended.GraphScore,
			Final:    blended.FinalScore,
			Distance: blended.Distance,
		}

		previous, hasPrevious := rankByID[blended.NodeID]

		if !hasPrevious {
			previous = filter.ScoredResult{NodeID: blended.NodeID}
		}

		// Replace the bare cosine with the blended final so downstream MinScore
		// + ordering operate on FinalScore (spec §6.1).
		previous.Score = blended.FinalScore
		newRanked = append(newRanked, previous)
	}

	return newRanked, blendedByID, nil
}

// applyMinScore drops candidates scoring below minScore, returning the kept
// slice and the count dropped. minScore <= 0 is a no-op. The filter reuses the
// input's backing array in place (ranked[:0]) exactly as the two semantic
// ranking paths did inline, so a later window() re-slice still aliases it.
func applyMinScore(ranked []filter.ScoredResult, minScore float64) ([]filter.ScoredResult, int) {
	if minScore <= 0 {
		return ranked, 0
	}

	filtered := 0
	kept := ranked[:0]

	for _, scored := range ranked {
		if scored.Score >= minScore {
			kept = append(kept, scored)

			continue
		}

		filtered++
	}

	return kept, filtered
}

// window applies take/skip paging to an already-ordered slice, re-slicing the
// backing array in place (never copying). take <= 0 falls back to defaultTake;
// when both are <= 0 the slice is returned untouched. Mirrors the inline paging
// both semantic ranking paths applied to their final ordered results.
func window[E any](items []E, skip, take, defaultTake int) []E {
	if take <= 0 {
		take = defaultTake
	}

	if take <= 0 {
		return items
	}

	startIdx := skip

	if startIdx > len(items) {
		startIdx = len(items)
	}

	endIdx := startIdx + take

	if endIdx > len(items) {
		endIdx = len(items)
	}

	return items[startIdx:endIdx]
}
