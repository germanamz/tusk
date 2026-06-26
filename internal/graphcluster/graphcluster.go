// Package graphcluster discovers communities in an undirected weighted edge
// set and returns a node-id -> community-index partition. It is the engine
// behind the graph view's `by = "community"` cluster lens: the only producer
// that surfaces cross-branch dependency communities (systems/services that
// depend on each other across the containment tree, where the real cluster is
// neither the node type nor an ancestor).
//
// The single load-bearing invariant is determinism. Identical
// (nodeIDs, edges, opts) input always yields the identical map — the same
// communities, the same canonical indices, and the same tie-breaks — on every
// call, in every process, forever. The consumer (the snapshot producer) re-runs
// Detect on every reindex generation (~2s poll) and maps each new partition
// onto the previous one to keep colors sticky; a single re-labeled region would
// otherwise re-color and re-anchor a whole branch on an unrelated edit.
// Determinism is therefore not a nice-to-have but a contract.
//
// Determinism is enforced by ordering, never by chance. The engine reads no
// wall-clock time, no global RNG, and never lets Go's randomized map-iteration
// order influence a decision. Node ids are sorted lexicographically and visited
// in that fixed order on every pass and every call; ties on modularity gain
// break by lowest community index, then lowest member node id; and the returned
// community indices are canonicalized by first appearance in sorted node order
// so they are a pure function of the partition, independent of internal merge
// order. The Options.Seed field is part of the frozen API and reserved for any
// future tie-break need, but this implementation uses no randomization at all,
// so the result is identical for every Seed, including the default 0.
//
// The algorithm is deterministic Louvain modularity maximization with the
// Resolution (gamma) parameter: alternating local-moving and aggregation passes
// until no move improves Q. It uses the Go standard library only.
package graphcluster

// Edge is one undirected weighted link between two node ids. The detector
// treats (Source, Target) and (Target, Source) as the same edge, and folds
// parallel edges by summing their weights. Self-loops (Source == Target) are
// retained in the total edge weight but contribute no community-membership
// gain, per the standard Louvain treatment.
type Edge struct {
	Source string
	Target string
	Weight float64
}

// Options tunes the partition. Resolution is the modularity gamma: higher
// values favor more, smaller communities, lower values favor fewer, larger
// ones. A non-positive Resolution is treated as the default 1.0. Seed is part
// of the frozen contract and reserved for any tie-break/RNG need; this engine
// is fully deterministic and uses no randomization, so every Seed (including
// the default 0) produces the identical partition.
type Options struct {
	Resolution float64
	Seed       int64
}

// Detect partitions nodeIDs into communities from the undirected weighted
// edges and returns a nodeID -> community-index map. It is deterministic:
// identical inputs always produce the identical map.
//
// The node set is authoritative. Every distinct id in nodeIDs appears in the
// result exactly once; duplicate ids coalesce. Edges naming an id absent from
// nodeIDs are dropped. An id with no incident edge is its own singleton
// community. Empty input yields a non-nil empty map. Community indices are
// canonical: index 0 is the community of the lexicographically-first id, 1 the
// next not-yet-seen community, and so on.
func Detect(nodeIDs []string, edges []Edge, opts Options) map[string]int {
	resolution := opts.Resolution

	if resolution <= 0 {
		resolution = 1.0
	}

	graph := buildGraph(nodeIDs, edges)

	if len(graph.ids) == 0 {
		return map[string]int{}
	}

	communityOf := runLouvain(graph, resolution)

	return canonicalize(graph.ids, communityOf)
}
