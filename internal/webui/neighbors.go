package webui

import (
	"sort"

	"github.com/germanamz/tusk/internal/index"
)

// NodeLister resolves node rows by id. Satisfied by *index.NodeRepo and by
// each view's own node dependency (Go interface satisfaction is structural, so
// no adapter is needed at either call site).
type NodeLister interface {
	ListByIDs(ids []string) ([]index.NodeRow, error)
}

// EdgeLister lists the edges incident to one node.
type EdgeLister interface {
	ListBySource(sourceID string) ([]index.EdgeRow, error)
	ListByTarget(targetID string) ([]index.EdgeRow, error)
}

// Neighbor is one file-level neighbor of a focus node: the far-end node row,
// the edge reaching it, and the side the far end sits on. It deliberately
// carries the raw index rows rather than a view shape, because the graph and
// book views emit different JSON and project this into their own payload type.
//
// No view policy is applied here beyond the file-level rule: structural
// ("contains") edges to file nodes are returned, and a view that does not want
// them filters on Edge.Kind itself.
type Neighbor struct {
	Node      index.NodeRow
	Edge      index.EdgeRow
	Direction string // "out" when the focus is the edge's source, "in" when the target
}

// adjacentEdge pairs an edge touching the focus node with the direction the
// far end sits in ("out" when the focus is the source, "in" when it's the
// target). Collected from ListBySource/ListByTarget and re-sorted into the
// global ListAll order before neighbors are emitted.
type adjacentEdge struct {
	edge      index.EdgeRow
	farID     string
	direction string
}

// Neighbors returns the file-level neighbors of nodeID. It fetches only the
// edges incident to nodeID (ListBySource + ListByTarget) instead of scanning
// every edge, then resolves all distinct far-end nodes in a single batched
// ListByIDs lookup rather than one Get per edge.
//
// Self-loops are emitted once, as "out". Sub-unit and dangling far ends are
// dropped. The result is ordered by ListAll's global (SourceID, Type,
// TargetID), and is an empty, non-nil slice when the node has no neighbors.
//
// An unknown nodeID yields an empty slice and no error — this traverses edges
// and does not verify the focus node exists. Callers needing a 404 should
// verify the node with NodeRepo.Get first.
func Neighbors(nodes NodeLister, edges EdgeLister, nodeID string) ([]Neighbor, error) {
	outEdges, outErr := edges.ListBySource(nodeID)

	if outErr != nil {
		return nil, outErr
	}

	inEdges, inErr := edges.ListByTarget(nodeID)

	if inErr != nil {
		return nil, inErr
	}

	adjacent := make([]adjacentEdge, 0, len(outEdges)+len(inEdges))

	for _, row := range outEdges {
		adjacent = append(adjacent, adjacentEdge{edge: row, farID: row.TargetID, direction: "out"})
	}

	for _, row := range inEdges {
		// A self-loop (source_id == target_id == nodeID) is returned by both
		// ListBySource and ListByTarget; ListAll yields it once, classified as
		// "out" (the source case wins). Skip it here to avoid a double count.
		if row.SourceID == nodeID {
			continue
		}

		adjacent = append(adjacent, adjacentEdge{edge: row, farID: row.SourceID, direction: "in"})
	}

	// Reproduce ListAll's global ordering (source_id, type, target_id) so the
	// emitted neighbor order is byte-identical to the prior full-scan path.
	sort.SliceStable(adjacent, func(left, right int) bool {
		lhs, rhs := adjacent[left].edge, adjacent[right].edge

		if lhs.SourceID != rhs.SourceID {
			return lhs.SourceID < rhs.SourceID
		}

		if lhs.Type != rhs.Type {
			return lhs.Type < rhs.Type
		}

		return lhs.TargetID < rhs.TargetID
	})

	// Resolve the distinct far-end node rows in one batched lookup, preserving
	// first-seen order only for the (unused) request order; the map is what the
	// emit loop consults.
	farIDs := make([]string, 0, len(adjacent))
	seen := make(map[string]struct{}, len(adjacent))

	for _, adj := range adjacent {
		if _, ok := seen[adj.farID]; ok {
			continue
		}

		seen[adj.farID] = struct{}{}
		farIDs = append(farIDs, adj.farID)
	}

	farRows, listErr := nodes.ListByIDs(farIDs)

	if listErr != nil {
		return nil, listErr
	}

	byID := make(map[string]index.NodeRow, len(farRows))

	for _, far := range farRows {
		byID[far.ID] = far
	}

	// Non-nil so a caller marshalling the projection of an isolated node emits
	// [] rather than null.
	neighbors := make([]Neighbor, 0)

	for _, adj := range adjacent {
		far, found := byID[adj.farID]

		if !found {
			continue // dangling edge target; skip
		}

		// ListByIDs resolves sub-unit rows too (it queries all nodes by id),
		// so we cannot rely on a not-found to exclude them. A "contains" edge's
		// target is a sub-unit (ParentID set); the file-level view excludes it.
		if far.ParentID.Valid {
			continue
		}

		neighbors = append(neighbors, Neighbor{Node: far, Edge: adj.edge, Direction: adj.direction})
	}

	return neighbors, nil
}
