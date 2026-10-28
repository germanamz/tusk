package query

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/graphexpand"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
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
//
// collapse, when non-nil, is applied to every walked candidate id and edge
// endpoint after the walk and before the blend. The sub-unit file-level path
// passes fileIDFromSubUnit so section-anchored cross-file edges ([[other#S1]])
// fold to their parent file and lift it like a plain file-level link (#688
// finding 2); the file-level path passes nil (its ids are already files).
func expandAndBlend(ctx context.Context, deps Deps, req Request, ranked []filter.ScoredResult, collapse func(string) string) ([]filter.ScoredResult, map[string]blendedTrace, error) {
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

	if collapse != nil {
		walkedCandidates, walkedEdges = collapseWalkToFileLevel(walkedCandidates, walkedEdges, collapse)
	}

	// Drop walked-in candidates (never seeds) that have no live nodes row so
	// dangling wikilink targets ([[does/not-exist]]) don't surface as ghost
	// result rows with empty type/path/title (#688 finding 3). Seeds are real
	// by construction (they carry embeddings / came from the structural
	// pre-filter), so they are always kept.
	if deps.Nodes != nil {
		walkedCandidates, walkedEdges, walkErr = dropDanglingCandidates(walkedCandidates, walkedEdges, seedScores, deps.Nodes)

		if walkErr != nil {
			return nil, nil, fmt.Errorf("query: graph expansion existence check: %w", walkErr)
		}
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

	// Collapse walked candidate ids and edge endpoints to their parent file
	// (fileIDFromSubUnit) before blending: seed files already join at file
	// level, so a cross-file edge whose endpoint is a sub-unit id
	// ([[other#S1]], structural `contains` targets) must fold to its file to
	// contribute graph signal — and folded candidates never surface as '#'
	// rows (#688 finding 2, subsuming the old per-trace re-attribution).
	_, fileBlend, blendErr := expandAndBlend(ctx, deps, req, fileRanked, fileIDFromSubUnit)

	if blendErr != nil {
		return nil, blendErr
	}

	return fileBlend, nil
}

// collapseWalkToFileLevel rewrites every walked candidate id and edge endpoint
// through collapse (fileIDFromSubUnit for the sub-unit path), so a walk that
// reached a sub-unit id ([[other#S1]], structural `contains` targets) is scored
// at file granularity. Candidates mapping to the same id merge — the smallest
// distance and largest cosine win, matching the file seed aggregation — and
// edges that collapse to a self-loop (a file's own `contains` edge) are dropped.
func collapseWalkToFileLevel(
	candidates []graphexpand.Candidate,
	edges []graphexpand.NeighborEdge,
	collapse func(string) string,
) ([]graphexpand.Candidate, []graphexpand.NeighborEdge) {
	merged := make(map[string]graphexpand.Candidate, len(candidates))
	order := make([]string, 0, len(candidates))

	for _, candidate := range candidates {
		id := collapse(candidate.NodeID)

		existing, seen := merged[id]

		if !seen {
			merged[id] = graphexpand.Candidate{
				NodeID:      id,
				CosineScore: candidate.CosineScore,
				Distance:    candidate.Distance,
			}
			order = append(order, id)

			continue
		}

		existing.Distance = min(existing.Distance, candidate.Distance)
		existing.CosineScore = max(existing.CosineScore, candidate.CosineScore)

		merged[id] = existing
	}

	collapsedCandidates := make([]graphexpand.Candidate, 0, len(order))

	for _, id := range order {
		collapsedCandidates = append(collapsedCandidates, merged[id])
	}

	seenEdge := make(map[string]struct{}, len(edges))
	collapsedEdges := make([]graphexpand.NeighborEdge, 0, len(edges))

	for _, edge := range edges {
		source, target := collapse(edge.Source), collapse(edge.Target)

		if source == target {
			continue
		}

		first, second := source, target

		if first > second {
			first, second = second, first
		}

		key := edge.Type + "|" + first + "|" + second

		if _, dupe := seenEdge[key]; dupe {
			continue
		}

		seenEdge[key] = struct{}{}
		collapsedEdges = append(collapsedEdges, graphexpand.NeighborEdge{
			Source: source,
			Target: target,
			Type:   edge.Type,
		})
	}

	return collapsedCandidates, collapsedEdges
}

// dropDanglingCandidates removes walked-in candidates (non-seeds) whose id has
// no live nodes row and prunes any edge touching one, so a wikilink to a note
// that does not exist ([[does/not-exist]]) never becomes a scored ghost row
// with empty type/path/title (#688 finding 3). A dangling target is never a
// seed, so this can only ever lower noise — real nodes' graph scores are
// unaffected.
func dropDanglingCandidates(
	candidates []graphexpand.Candidate,
	edges []graphexpand.NeighborEdge,
	seedScores map[string]float64,
	nodes *index.NodeRepo,
) ([]graphexpand.Candidate, []graphexpand.NeighborEdge, error) {
	toCheck := make([]string, 0, len(candidates))

	for _, candidate := range candidates {
		if _, isSeed := seedScores[candidate.NodeID]; isSeed {
			continue
		}

		toCheck = append(toCheck, candidate.NodeID)
	}

	if len(toCheck) == 0 {
		return candidates, edges, nil
	}

	rows, listErr := nodes.ListByIDs(toCheck)

	if listErr != nil {
		return nil, nil, listErr
	}

	live := make(map[string]struct{}, len(rows))

	for _, row := range rows {
		live[row.ID] = struct{}{}
	}

	keep := func(id string) bool {
		if _, isSeed := seedScores[id]; isSeed {
			return true
		}

		_, exists := live[id]

		return exists
	}

	keptCandidates := make([]graphexpand.Candidate, 0, len(candidates))

	for _, candidate := range candidates {
		if keep(candidate.NodeID) {
			keptCandidates = append(keptCandidates, candidate)
		}
	}

	keptEdges := make([]graphexpand.NeighborEdge, 0, len(edges))

	for _, edge := range edges {
		if keep(edge.Source) && keep(edge.Target) {
			keptEdges = append(keptEdges, edge)
		}
	}

	return keptCandidates, keptEdges, nil
}

// Trace names the score components a semantic result's explain fields carry,
// so a renderer prints only what the ranking computed: every row has a cosine
// and a final score, and the rest depend on the workspace and request.
type Trace struct {
	// Lexical is set when [query] lexical-weight is above 0.
	Lexical bool
	// Title is set when [query] title-weight is above 0.
	Title bool
	// Graph is set when graph expansion ran: the graph score and distance.
	Graph bool
	// Weight is set when the manifest declares [query.type-weights] or
	// [query.path-weights].
	Weight bool
}

// newTrace returns the components req's ranking computes, or nil when req
// didn't ask to explain it.
func newTrace(deps Deps, req Request) *Trace {
	if !req.Explain {
		return nil
	}

	fusion := fusionWeights(deps)

	return &Trace{
		Lexical: fusion.Lexical > 0,
		Title:   fusion.Title > 0,
		Graph:   req.GraphExpansion != nil && req.GraphExpansion.Enabled,
		Weight:  !scoreWeights(deps).Empty(),
	}
}

// scoreTrace is one row's or unit's explain fields.
type scoreTrace struct {
	cosine, lexical, title, graph, weight, final float64
	distance                                     int
}

// explainScore builds the trace of a score: inputs carries the cosine,
// lexical and title inputs of the chunk behind it, blend its graph-expansion
// trace when blended, weight the type/path multiplier, and final the score it
// ranked by. A walked-in neighbor (distance > 0) ranks on graph merit alone,
// since the walker zeroes non-seed relevance, so its inputs show as 0.
func explainScore(inputs filter.ScoredResult, blend blendedTrace, blended bool, weight, final float64) scoreTrace {
	trace := scoreTrace{
		cosine:  inputs.Cosine,
		lexical: inputs.Lexical,
		title:   inputs.Title,
		weight:  weight,
		final:   final,
	}

	if blended {
		trace.graph = blend.Graph
		trace.distance = blend.Distance

		if blend.Distance > 0 {
			trace.cosine, trace.lexical, trace.title = 0, 0, 0
		}
	}

	return trace
}

func (row *ScoredRow) setTrace(trace scoreTrace) {
	row.CosineScore = trace.cosine
	row.LexicalScore = trace.lexical
	row.TitleScore = trace.title
	row.GraphScore = trace.graph
	row.Weight = trace.weight
	row.FinalScore = trace.final
	row.Distance = trace.distance
}

func (unit *MatchedUnit) setTrace(trace scoreTrace) {
	unit.CosineScore = trace.cosine
	unit.LexicalScore = trace.lexical
	unit.TitleScore = trace.title
	unit.GraphScore = trace.graph
	unit.Weight = trace.weight
	unit.FinalScore = trace.final
	unit.Distance = trace.distance
}

// fileWeights resolves the [query] type/path weight of each file a ranking
// touches, once per file. metaOf returns a file's row when the ranking already
// holds it; a file it doesn't hold (a walked-in neighbor outside the
// structural pre-filter) is looked up in nodes. With no weights declared every
// file weighs 1 and nothing is looked up.
type fileWeights struct {
	weights manifest.ScoreWeights
	metaOf  func(fileID string) (Row, bool)
	nodes   *index.NodeRepo
	byFile  map[string]float64
}

func newFileWeights(deps Deps, metaOf func(fileID string) (Row, bool)) *fileWeights {
	return &fileWeights{
		weights: scoreWeights(deps),
		metaOf:  metaOf,
		nodes:   deps.Nodes,
		byFile:  make(map[string]float64),
	}
}

// active reports whether the manifest declares any weight; when it doesn't,
// every file weighs 1 and callers skip the re-scale.
func (resolver *fileWeights) active() bool {
	return !resolver.weights.Empty()
}

// of returns fileID's weight.
func (resolver *fileWeights) of(fileID string) (float64, error) {
	if !resolver.active() {
		return 1, nil
	}

	if weight, resolved := resolver.byFile[fileID]; resolved {
		return weight, nil
	}

	meta, known := resolver.metaOf(fileID)

	if !known && resolver.nodes != nil {
		nodeRow, getErr := resolver.nodes.Get(fileID)

		switch {
		case getErr == nil:
			meta = Row{ID: nodeRow.ID, Type: nodeRow.Type, Path: nodeRow.Path}
		case !errors.Is(getErr, index.ErrNodeNotFound):
			return 0, getErr
		}
	}

	weight := resolver.weights.Weight(meta.Type, meta.Path)
	resolver.byFile[fileID] = weight

	return weight, nil
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

// sortRanked re-orders ranked by score descending after the type/path
// weights re-scale it. The sort is stable, so ties keep the order the rank or
// the graph blend gave them.
func sortRanked(ranked []filter.ScoredResult) {
	sort.SliceStable(ranked, func(left, right int) bool {
		return ranked[left].Score > ranked[right].Score
	})
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
