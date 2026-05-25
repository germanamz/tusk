// Package graphexpand performs bounded N-hop graph walks over the edge index
// to augment a cosine-ranked candidate set with edge-type-restricted
// neighbors. The walker is consumed by the semantic-query path when
// `[query.graph-expansion]` is enabled.
package graphexpand

import (
	"context"
	"fmt"
	"sort"

	"github.com/germanamz/tusk/internal/index"
)

// Candidate is a node under consideration for ranking. Seeds carry their
// original cosine score and Distance=0; neighbors discovered by the walk
// carry CosineScore=0 and the hop at which they were first reached.
type Candidate struct {
	NodeID      string
	CosineScore float64
	Distance    int
}

// NeighborEdge is one undirected edge touched by the walk. The walker
// deduplicates by (Type, sorted endpoint pair) so each underlying row
// contributes at most one entry.
type NeighborEdge struct {
	Source string
	Target string
	Type   string
}

// Walker performs bounded N-hop graph walks restricted to a set of edge
// types. Callers reuse a Walker across queries; it carries no mutable state.
type Walker struct {
	Edges     *index.EdgeRepo
	EdgeTypes []string
	MaxHops   int
}

// NewWalker constructs a Walker. It defensively copies edgeTypes so the
// caller (typically the MCP handler with a manifest-derived slice) can
// safely mutate or reuse its original.
func NewWalker(repo *index.EdgeRepo, edgeTypes []string, maxHops int) *Walker {
	copied := make([]string, len(edgeTypes))
	copy(copied, edgeTypes)

	return &Walker{
		Edges:     repo,
		EdgeTypes: copied,
		MaxHops:   maxHops,
	}
}

// Expand walks the graph from seeds along Walker.EdgeTypes, up to
// Walker.MaxHops. Returns the augmented candidate set (seeds plus newly
// discovered neighbors) and the deduplicated edges touched by the walk.
//
// The walk is undirected: an edge with type in EdgeTypes contributes to the
// neighbor set whether the seed is on its source or target side.
//
// Ordering: candidates are returned sorted by (Distance asc, NodeID asc);
// edges by (Type, Source, Target) asc. Context cancellation short-circuits
// the walk and returns ctx.Err().
//
// Cost: at most MaxHops SQL calls regardless of seed-set size — never per-
// candidate. SQLite's default 32766 parameter limit bounds the seed-set
// size, but at realistic K (50..250) we are nowhere near it.
func (walker *Walker) Expand(ctx context.Context, seeds []Candidate) ([]Candidate, []NeighborEdge, error) {
	if walker.MaxHops < 1 {
		return nil, nil, fmt.Errorf("graphexpand: MaxHops must be >= 1, got %d", walker.MaxHops)
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, nil, ctxErr
	}

	// distanceByID tracks the smallest hop at which each node was reached.
	// seeds always live at distance 0; subsequent hops only insert if the
	// node has not been seen yet.
	distanceByID := make(map[string]int, len(seeds))
	cosineByID := make(map[string]float64, len(seeds))

	// Dedupe seeds by NodeID; first occurrence wins for the cosine score.
	for _, seed := range seeds {
		if _, exists := distanceByID[seed.NodeID]; exists {
			continue
		}

		distanceByID[seed.NodeID] = 0
		cosineByID[seed.NodeID] = seed.CosineScore
	}

	if len(distanceByID) == 0 || len(walker.EdgeTypes) == 0 {
		return walker.finalize(distanceByID, cosineByID), nil, nil
	}

	// edgeKey is the canonical (Type, min(Source,Target), max(Source,Target))
	// dedupe key. The map value carries the original directional pair so we
	// preserve the row's source→target orientation in the result.
	edgeByKey := make(map[string]NeighborEdge)

	frontier := keysOf(distanceByID)

	for hop := 1; hop <= walker.MaxHops; hop++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, nil, ctxErr
		}

		if len(frontier) == 0 {
			break
		}

		rows, queryErr := walker.Edges.NeighborsByEdgeTypes(frontier, walker.EdgeTypes)

		if queryErr != nil {
			return nil, nil, fmt.Errorf("graphexpand: hop %d: %w", hop, queryErr)
		}

		for _, row := range rows {
			key := edgeDedupKey(row.Type, row.SourceID, row.TargetID)

			if _, exists := edgeByKey[key]; !exists {
				edgeByKey[key] = NeighborEdge{
					Source: row.SourceID,
					Target: row.TargetID,
					Type:   row.Type,
				}
			}

			// The walk is undirected: whichever endpoint is unknown becomes
			// a candidate at the current hop.
			for _, endpoint := range []string{row.SourceID, row.TargetID} {
				if _, seen := distanceByID[endpoint]; seen {
					continue
				}

				distanceByID[endpoint] = hop
				cosineByID[endpoint] = 0
			}
		}

		if hop == walker.MaxHops {
			break
		}

		// For hop N+1, the SQL takes the union of (existing visited set ∪
		// newly discovered nodes) so we capture edges that cross from an
		// interior node back out. The repo dedupes inputs internally.
		frontier = keysOf(distanceByID)
	}

	edges := make([]NeighborEdge, 0, len(edgeByKey))

	for _, edge := range edgeByKey {
		edges = append(edges, edge)
	}

	sort.Slice(edges, func(left, right int) bool {
		if edges[left].Type != edges[right].Type {
			return edges[left].Type < edges[right].Type
		}

		if edges[left].Source != edges[right].Source {
			return edges[left].Source < edges[right].Source
		}

		return edges[left].Target < edges[right].Target
	})

	return walker.finalize(distanceByID, cosineByID), edges, nil
}

func (walker *Walker) finalize(
	distanceByID map[string]int,
	cosineByID map[string]float64,
) []Candidate {
	candidates := make([]Candidate, 0, len(distanceByID))

	for nodeID, distance := range distanceByID {
		candidates = append(candidates, Candidate{
			NodeID:      nodeID,
			CosineScore: cosineByID[nodeID],
			Distance:    distance,
		})
	}

	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].Distance != candidates[right].Distance {
			return candidates[left].Distance < candidates[right].Distance
		}

		return candidates[left].NodeID < candidates[right].NodeID
	})

	return candidates
}

func keysOf(set map[string]int) []string {
	keys := make([]string, 0, len(set))

	for key := range set {
		keys = append(keys, key)
	}

	return keys
}

func edgeDedupKey(edgeType, source, target string) string {
	first, second := source, target

	if first > second {
		first, second = second, first
	}

	return edgeType + "|" + first + "|" + second
}
