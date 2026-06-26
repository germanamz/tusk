package graphcluster

import "sort"

// graph is the internal, index-based representation of the node set and its
// folded undirected weighted adjacency. ids holds the distinct node ids in
// lexicographic order; every other slice is indexed by that position. adjacency
// excludes self-loops (their weight lives in selfLoop), so neighbor iteration
// never visits a node's own index. The representation is built once per Detect
// call and never mutated thereafter.
type graph struct {
	// ids are the distinct node ids, sorted lexicographically. The slot of an
	// id in this slice is its internal integer index.
	ids []string

	// adjacency[node] maps a neighbor index to the summed weight of the edges
	// between node and that neighbor (parallel and reverse edges folded). It
	// never contains the node's own index.
	adjacency []map[int]float64

	// selfLoop[node] is the summed weight of edges whose endpoints both refer
	// to node. It counts toward degree and total weight but yields no
	// membership gain.
	selfLoop []float64

	// degree[node] is the weighted degree: the sum of incident edge weights,
	// counting each self-loop twice (the standard convention).
	degree []float64

	// totalWeight is m: the sum of all edge weights, with self-loops counted
	// once. 2*totalWeight is the normalizer in the modularity formula.
	totalWeight float64
}

// buildGraph folds the edge list into the internal index-based graph. The node
// set is authoritative: ids come only from nodeIDs (deduplicated and sorted),
// and any edge naming an id absent from that set is dropped. Parallel and
// reverse edges are summed; self-loops are tracked separately.
func buildGraph(nodeIDs []string, edges []Edge) graph {
	indexByID := make(map[string]int, len(nodeIDs))
	ids := make([]string, 0, len(nodeIDs))

	for _, nodeID := range nodeIDs {
		if _, seen := indexByID[nodeID]; seen {
			continue
		}

		indexByID[nodeID] = len(ids)
		ids = append(ids, nodeID)
	}

	sort.Strings(ids)

	// Re-index after sorting so the integer index of an id matches its slot in
	// the sorted ids slice; this is what makes the visit order canonical.
	for index, nodeID := range ids {
		indexByID[nodeID] = index
	}

	nodeCount := len(ids)
	built := graph{
		ids:       ids,
		adjacency: make([]map[int]float64, nodeCount),
		selfLoop:  make([]float64, nodeCount),
		degree:    make([]float64, nodeCount),
	}

	for index := range built.adjacency {
		built.adjacency[index] = make(map[int]float64)
	}

	for _, edge := range edges {
		source, sourceOK := indexByID[edge.Source]

		if !sourceOK {
			continue
		}

		target, targetOK := indexByID[edge.Target]

		if !targetOK {
			continue
		}

		weight := edge.Weight

		if source == target {
			built.selfLoop[source] += weight
			built.degree[source] += 2 * weight
			built.totalWeight += weight

			continue
		}

		built.adjacency[source][target] += weight
		built.adjacency[target][source] += weight
		built.degree[source] += weight
		built.degree[target] += weight
		built.totalWeight += weight
	}

	return built
}

// runLouvain returns a community index per node (indexed like graph.ids) by
// alternating deterministic local-moving and aggregation passes until no move
// improves modularity. The returned indices are internal and not yet canonical;
// canonicalize renumbers them.
func runLouvain(base graph, resolution float64) []int {
	// communityOf maps each original node index to its community index in the
	// current (possibly aggregated) working graph. It starts as the identity:
	// each original node is its own community.
	communityOf := make([]int, len(base.ids))

	for index := range communityOf {
		communityOf[index] = index
	}

	working := base

	for {
		labels, improved := localMoving(working, resolution)

		// Renumber the working-graph labels to a dense 0..k-1 range and project
		// them back onto the original node indices.
		dense, communityCount := densify(labels)

		for index := range communityOf {
			communityOf[index] = dense[communityOf[index]]
		}

		if !improved || communityCount == len(working.degree) {
			// Either no node moved, or every node is already alone in its own
			// community: aggregation would not change the partition.
			break
		}

		working = aggregate(working, dense, communityCount)
	}

	return communityOf
}

// localMoving runs the Louvain local-moving phase on one working graph: repeat
// full sweeps, each visiting nodes in ascending index order (which is
// lexicographic id order in the base graph), moving each node into the
// neighboring community with the greatest positive modularity gain. It returns
// the per-node community labels (not yet densified) and whether any node moved.
//
// Determinism: nodes are visited in fixed ascending index order every sweep;
// candidate communities are evaluated in ascending community index, so the
// lowest community index wins a gain tie; and within the chosen community the
// constituent nodes are, by construction, the lowest-id members. No RNG, clock,
// or map-order read participates in any decision.
func localMoving(working graph, resolution float64) ([]int, bool) {
	nodeCount := len(working.degree)
	community := make([]int, nodeCount)
	communityTotalDegree := make([]float64, nodeCount)

	for node := range community {
		community[node] = node
		communityTotalDegree[node] = working.degree[node]
	}

	twoM := 2 * working.totalWeight
	movedEver := false

	for {
		movedThisSweep := false

		for node := 0; node < nodeCount; node++ {
			current := community[node]

			// Weight from node into each candidate community. Iterating the
			// adjacency map to accumulate sums is order-independent (addition
			// commutes), so map-iteration order does not affect the result.
			weightToCommunity := make(map[int]float64)

			for neighbor, weight := range working.adjacency[node] {
				weightToCommunity[community[neighbor]] += weight
			}

			// Remove node from its current community before scoring moves, so
			// the "stay" option is evaluated on equal footing with the rest.
			communityTotalDegree[current] -= working.degree[node]

			bestCommunity := current
			bestGain := modularityGain(
				weightToCommunity[current],
				working.degree[node],
				communityTotalDegree[current],
				twoM,
				resolution,
			)

			// Evaluate candidate communities in ascending index order so the
			// lowest community index deterministically wins any gain tie.
			candidates := make([]int, 0, len(weightToCommunity))

			for candidate := range weightToCommunity {
				candidates = append(candidates, candidate)
			}

			sort.Ints(candidates)

			for _, candidate := range candidates {
				if candidate == current {
					continue
				}

				gain := modularityGain(
					weightToCommunity[candidate],
					working.degree[node],
					communityTotalDegree[candidate],
					twoM,
					resolution,
				)

				if gain > bestGain {
					bestGain = gain
					bestCommunity = candidate
				}
			}

			communityTotalDegree[bestCommunity] += working.degree[node]
			community[node] = bestCommunity

			if bestCommunity != current {
				movedThisSweep = true
				movedEver = true
			}
		}

		if !movedThisSweep {
			break
		}
	}

	return community, movedEver
}

// modularityGain returns the change in modularity from adding a node into a
// community, in the standard Louvain delta-Q form (the constant terms that
// cancel across candidates are dropped). weightToCommunity is the summed weight
// from the node into the community; nodeDegree is k_i; communityDegree is the
// summed weighted degree of the community's current members (with the node
// already removed); twoM is 2m; resolution is gamma.
func modularityGain(
	weightToCommunity float64,
	nodeDegree float64,
	communityDegree float64,
	twoM float64,
	resolution float64,
) float64 {
	if twoM == 0 {
		return 0
	}

	return weightToCommunity - resolution*nodeDegree*communityDegree/twoM
}

// densify remaps an arbitrary label slice to a contiguous 0..k-1 range,
// assigning new ids by first appearance in ascending node index order. It
// returns the per-node dense labels and the community count k. The
// first-appearance-in-index-order rule keeps aggregation deterministic.
func densify(labels []int) ([]int, int) {
	remap := make(map[int]int, len(labels))
	dense := make([]int, len(labels))
	next := 0

	for node, label := range labels {
		mapped, seen := remap[label]

		if !seen {
			mapped = next
			remap[label] = mapped
			next++
		}

		dense[node] = mapped
	}

	return dense, next
}

// aggregate collapses each community of the working graph into a single
// super-node, producing a smaller graph whose node indices are the dense
// community ids. Inter-community edge weights become super-node adjacency;
// intra-community weights (including the working graph's own self-loops) become
// the super-node's self-loop. Total weight and degrees are preserved exactly,
// so modularity is conserved across the aggregation boundary.
func aggregate(working graph, dense []int, communityCount int) graph {
	aggregated := graph{
		ids:         make([]string, communityCount),
		adjacency:   make([]map[int]float64, communityCount),
		selfLoop:    make([]float64, communityCount),
		degree:      make([]float64, communityCount),
		totalWeight: working.totalWeight,
	}

	for index := range aggregated.adjacency {
		aggregated.adjacency[index] = make(map[int]float64)
	}

	for node := 0; node < len(working.degree); node++ {
		superNode := dense[node]

		// The working self-loop is intra-community by definition; fold it in
		// doubled, matching the degree convention.
		aggregated.selfLoop[superNode] += working.selfLoop[node]
		aggregated.degree[superNode] += working.degree[node]

		for neighbor, weight := range working.adjacency[node] {
			superNeighbor := dense[neighbor]

			if superNode == superNeighbor {
				// Intra-community edge. The adjacency stores each undirected
				// edge twice (once per endpoint); halve to count it once into
				// the self-loop weight.
				aggregated.selfLoop[superNode] += weight / 2

				continue
			}

			aggregated.adjacency[superNode][superNeighbor] += weight
		}
	}

	return aggregated
}

// canonicalize renumbers communityOf into the final node-id -> community-index
// map, assigning indices by first appearance in sorted node order: index 0 to
// the community of the lexicographically-first id, 1 to the next not-yet-seen
// community, and so on. sortedIDs must already be lexicographically sorted (as
// graph.ids is). The result is a pure function of the partition, independent of
// the algorithm's internal merge order.
func canonicalize(sortedIDs []string, communityOf []int) map[string]int {
	canonicalIndex := make(map[int]int, len(communityOf))
	result := make(map[string]int, len(sortedIDs))
	next := 0

	for position, nodeID := range sortedIDs {
		internal := communityOf[position]
		mapped, seen := canonicalIndex[internal]

		if !seen {
			mapped = next
			canonicalIndex[internal] = mapped
			next++
		}

		result[nodeID] = mapped
	}

	return result
}
