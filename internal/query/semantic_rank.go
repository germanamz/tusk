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

// expandAndBlendFileLevel runs graph expansion for the sub-unit semantic path.
// User-declared edges (wikilink, ref-derived, direct frontmatter) join FILE
// node ids, while the sub-unit path's cosine rank carries `file#hash` leaf
// ids — walking the leaf ids directly can never match those edges, which left
// expansion inert on the default path (graph_score = 0 on every row). This
// wrapper aggregates the leaf rank to per-file seeds (file cosine = max leaf
// cosine, the same max-aggregation the file ordering uses downstream), reuses
// expandAndBlend at the file level, and returns the per-FILE trace map. The
// caller maps each file's graph term back onto its leaves and surfaces
// walked-in neighbor files. Returns nil when expansion is inactive.
func expandAndBlendFileLevel(ctx context.Context, deps Deps, req Request, leafRanked []filter.ScoredResult) (map[string]blendedTrace, error) {
	if req.GraphExpansion == nil || !req.GraphExpansion.Enabled {
		return nil, nil
	}

	// leafRanked is sorted by score desc, so the first leaf seen per file
	// carries the file's max cosine and the aggregated slice stays sorted.
	seen := make(map[string]struct{}, len(leafRanked))
	fileRanked := make([]filter.ScoredResult, 0, len(leafRanked))

	for _, leaf := range leafRanked {
		fileID := fileIDFromSubUnit(leaf.NodeID)

		if _, dupe := seen[fileID]; dupe {
			continue
		}

		seen[fileID] = struct{}{}
		fileRanked = append(fileRanked, filter.ScoredResult{NodeID: fileID, Score: leaf.Score})
	}

	_, fileBlend, blendErr := expandAndBlend(ctx, deps, req, fileRanked)

	if blendErr != nil {
		return nil, blendErr
	}

	mergeSubUnitTraces(fileBlend)

	return fileBlend, nil
}

// mergeSubUnitTraces re-attributes walked-in sub-unit candidates to their
// parent FILE id, in place. The walk reaches `file#hash` ids two ways:
// structural `contains` edges from a seed file to its own sub-units (parent
// already traced — the sub-unit entry is simply dropped), and cross-file
// edges that target another file's sub-unit (wikilinks like [[b#S1P3]] or
// agent-added edges). For the cross-file case the parent inherits the
// sub-unit's graph-derived trace — strongest Final wins across several
// walked-in sub-units — so the file surfaces as the result row; rows on this
// path never carry '#' ids.
func mergeSubUnitTraces(fileBlend map[string]blendedTrace) {
	for id, trace := range fileBlend {
		parentID := fileIDFromSubUnit(id)

		if parentID == id {
			continue
		}

		delete(fileBlend, id)

		// A parent with its own seed trace keeps it; a parent walked in with
		// a weaker signal is upgraded to the strongest sub-unit trace.
		if existing, exists := fileBlend[parentID]; exists {
			if existing.Distance == 0 || existing.Final >= trace.Final {
				continue
			}
		}

		fileBlend[parentID] = trace
	}
}

// clipUnitScore clamps a cosine to [0, 1], mirroring graphexpand's blender
// clip so leaf-level blends stay comparable to file-level FinalScores.
func clipUnitScore(value float64) float64 {
	if value < 0 {
		return 0
	}

	if value > 1 {
		return 1
	}

	return value
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
