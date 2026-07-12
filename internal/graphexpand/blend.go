package graphexpand

import "sort"

// Scored is the blender's per-candidate output: the cosine score (clipped to
// [0, 1]), the seed-neighbor-derived graph score, and the blended final score
// the query path orders results by. Distance is the hop at which the walker
// first reached the node and is forwarded from the matching Candidate.
type Scored struct {
	NodeID      string
	Distance    int
	CosineScore float64
	GraphScore  float64
	FinalScore  float64
}

// Blender re-ranks walker output by combining each candidate's cosine score
// with a graph-derived score (the average of its seed-neighbors' cosines).
// Weight is the per-hop attenuation read from `[query.graph-expansion] weight`
// and must lie in [0, 1]; the query service enforces this bound at config
// load, so the blender does not re-validate.
type Blender struct {
	Weight float64 // [0, 1]
}

// Score blends cosine and graph scores per spec §6.1, extended with per-hop
// decay for distance > 1:
//
//	graph_score(c) at distance <= 1 = avg of cosine scores of c's neighbors
//	                 that are in the seed set (the top-K cosine candidates),
//	                 0 if c has no such neighbors.
//	graph_score(c) at distance d >= 2 = Weight * avg of graph_score over c's
//	                 neighbors one hop closer to the seed set (distance d-1).
//	final_score(c) = (1 - Weight) * cosine + Weight * graph_score
//
// The distance>=2 rule is what makes Weight the "per-hop attenuation" the
// manifest documents: a hop-2 neighbor of a strong seed surfaces with a
// Weight^2-style non-zero score instead of bottoming out at exactly 0. BFS
// guarantees every distance-d node has at least one distance-(d-1) neighbor,
// so the graph signal always has a path to decay along (#688 finding 1).
//
// Cosine clip: embed.CosineSimilarity returns the mathematical cosine in
// [-1, 1]. nomic-embed-text rarely produces negative similarities for natural
// text but it can; the blender clips every cosine input to [0, 1] before
// blending so MinScore comparisons against FinalScore remain meaningful. The
// graph_score is itself an average of clipped seed cosines, so it inherits
// the clip without extra work.
//
// Inputs:
//
//	candidates: walker output. Seeds carry their original CosineScore;
//	            walked neighbors carry CosineScore=0 and Distance > 0.
//	edges:      walker's neighbor edges, already deduped (undirected).
//	seedScores: the original cosine top-K, keyed by NodeID. Only seeds
//	            contribute to a distance<=1 candidate's graph_score.
//
// Output: one Scored per candidate, sorted by FinalScore desc, ties broken
// by (Distance asc, NodeID asc).
func (blender *Blender) Score(
	candidates []Candidate,
	edges []NeighborEdge,
	seedScores map[string]float64,
) []Scored {
	if len(candidates) == 0 {
		return nil
	}

	// Build a per-node neighbor index from the edge list so we can iterate
	// every candidate's neighbors in O(deg). The walk is undirected; whichever
	// endpoint we hold, the other is the neighbor.
	neighborsByNode := make(map[string][]string, len(candidates))

	for _, edge := range edges {
		neighborsByNode[edge.Source] = append(neighborsByNode[edge.Source], edge.Target)
		neighborsByNode[edge.Target] = append(neighborsByNode[edge.Target], edge.Source)
	}

	// Clip seed scores to [0, 1] up front so the graph_score averaging step
	// doesn't re-clip per-neighbor. seedScores comes from the caller (the
	// query service) and is conceptually read-only; we copy values rather
	// than mutate the caller's map.
	clippedSeeds := make(map[string]float64, len(seedScores))

	for nodeID, score := range seedScores {
		clippedSeeds[nodeID] = clipUnit(score)
	}

	// Per-hop decay reads the already-computed graph_score of nodes one hop
	// closer to the seeds, so process candidates in ascending-distance order
	// and remember each node's distance + graph_score as we go. A stable copy
	// keeps the caller's slice untouched.
	distanceByID := make(map[string]int, len(candidates))

	for _, candidate := range candidates {
		distanceByID[candidate.NodeID] = candidate.Distance
	}

	ordered := make([]Candidate, len(candidates))
	copy(ordered, candidates)
	sort.SliceStable(ordered, func(left, right int) bool {
		return ordered[left].Distance < ordered[right].Distance
	})

	graphByID := make(map[string]float64, len(ordered))
	out := make([]Scored, 0, len(ordered))

	for _, candidate := range ordered {
		cosine := clipUnit(candidate.CosineScore)

		var graphScore float64

		if candidate.Distance <= 1 {
			graphScore = computeGraphScore(candidate.NodeID, neighborsByNode[candidate.NodeID], clippedSeeds)
		} else {
			graphScore = blender.Weight * propagateGraphScore(
				candidate.NodeID,
				candidate.Distance,
				neighborsByNode[candidate.NodeID],
				distanceByID,
				graphByID,
			)
		}

		graphByID[candidate.NodeID] = graphScore

		final := (1-blender.Weight)*cosine + blender.Weight*graphScore

		out = append(out, Scored{
			NodeID:      candidate.NodeID,
			Distance:    candidate.Distance,
			CosineScore: cosine,
			GraphScore:  graphScore,
			FinalScore:  final,
		})
	}

	sort.Slice(out, func(left, right int) bool {
		if out[left].FinalScore != out[right].FinalScore {
			return out[left].FinalScore > out[right].FinalScore
		}

		if out[left].Distance != out[right].Distance {
			return out[left].Distance < out[right].Distance
		}

		return out[left].NodeID < out[right].NodeID
	})

	return out
}

// computeGraphScore returns the average of seed-neighbor cosines for nodeID.
// A neighbor that is not in seedScores does not count; a candidate with no
// seed neighbors gets 0 (the spec's null-neighbor behavior).
func computeGraphScore(nodeID string, neighbors []string, seedScores map[string]float64) float64 {
	if len(neighbors) == 0 {
		return 0
	}

	var (
		total float64
		count int
	)

	seen := make(map[string]struct{}, len(neighbors))

	for _, neighborID := range neighbors {
		if neighborID == nodeID {
			// An edge whose endpoints both refer to nodeID (degenerate but
			// legal). Skip — a node is not its own neighbor for the purpose
			// of graph_score.
			continue
		}

		if _, dupe := seen[neighborID]; dupe {
			continue
		}

		seen[neighborID] = struct{}{}

		score, isSeed := seedScores[neighborID]

		if !isSeed {
			continue
		}

		total += score
		count++
	}

	if count == 0 {
		return 0
	}

	return total / float64(count)
}

// propagateGraphScore returns the average graph_score of nodeID's neighbors
// that sit one hop closer to the seed set (distance == nodeDistance-1). The
// caller multiplies the result by Weight, so a distance-d node's graph signal
// is the decayed share of the strongest layer that reached it. Neighbors at
// the same or greater distance are ignored — they carry no fresher seed
// signal — and a node is never its own neighbor. Returns 0 when no closer
// neighbor exists (BFS makes that impossible for a genuine distance-d node,
// but a pruned/orphaned candidate degrades cleanly to 0).
func propagateGraphScore(
	nodeID string,
	nodeDistance int,
	neighbors []string,
	distanceByID map[string]int,
	graphByID map[string]float64,
) float64 {
	var (
		total float64
		count int
	)

	seen := make(map[string]struct{}, len(neighbors))

	for _, neighborID := range neighbors {
		if neighborID == nodeID {
			continue
		}

		if _, dupe := seen[neighborID]; dupe {
			continue
		}

		seen[neighborID] = struct{}{}

		if distanceByID[neighborID] != nodeDistance-1 {
			continue
		}

		total += graphByID[neighborID]
		count++
	}

	if count == 0 {
		return 0
	}

	return total / float64(count)
}

// clipUnit clamps the input to [0, 1]. Used to keep FinalScore comparable
// across queries even when an outlier cosine goes slightly negative.
func clipUnit(value float64) float64 {
	if value < 0 {
		return 0
	}

	if value > 1 {
		return 1
	}

	return value
}
