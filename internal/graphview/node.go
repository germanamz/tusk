package graphview

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/germanamz/tusk/internal/index"
)

// handleNodeDetail serves GET /api/node/{id...}. The id may contain slashes, so
// the wildcard captures the rest of the path; PathValue unescapes each segment.
func (srv *Server) handleNodeDetail(writer http.ResponseWriter, request *http.Request) {
	srv.respondDetail(writer, request.PathValue("id"))
}

// handleSubunits serves GET /api/subunits/{id...}. A separate top-level prefix
// (rather than a /subunits suffix on the node route) keeps a node whose id ends
// in /subunits reachable for detail, and Go's ServeMux requires the {id...}
// wildcard to be the final path element.
func (srv *Server) handleSubunits(writer http.ResponseWriter, request *http.Request) {
	srv.respondSubunits(writer, request.PathValue("id"))
}

func (srv *Server) respondDetail(writer http.ResponseWriter, nodeID string) {
	row, getErr := srv.deps.Nodes.Get(nodeID)
	if errors.Is(getErr, index.ErrNodeNotFound) {
		http.Error(writer, "node not found: "+nodeID, http.StatusNotFound)

		return
	}

	if getErr != nil {
		http.Error(writer, getErr.Error(), http.StatusServiceUnavailable)

		return
	}

	rendered, renderErr := srv.deps.Render.Render(nodeID)
	if renderErr != nil {
		rendered = ""
	}

	neighbors, neighborErr := srv.neighborsOf(nodeID)
	if neighborErr != nil {
		http.Error(writer, neighborErr.Error(), http.StatusServiceUnavailable)

		return
	}

	properties := json.RawMessage(row.PropertiesJSON)
	if len(properties) == 0 {
		properties = json.RawMessage("{}")
	}

	writeJSON(writer, NodeDetail{
		ID:         row.ID,
		Type:       row.Type,
		Title:      row.Title,
		Path:       row.Path,
		Properties: properties,
		Rendered:   rendered,
		Neighbors:  neighbors,
	})
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

// neighborsOf returns the file-level neighbors of nodeID. It fetches only the
// edges incident to nodeID (ListBySource + ListByTarget) instead of scanning
// every edge, then resolves all distinct far-end nodes in a single batched
// ListByIDs lookup rather than one Get per edge.
func (srv *Server) neighborsOf(nodeID string) ([]Neighbor, error) {
	outEdges, outErr := srv.deps.Edges.ListBySource(nodeID)
	if outErr != nil {
		return nil, outErr
	}

	inEdges, inErr := srv.deps.Edges.ListByTarget(nodeID)
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

	farRows, listErr := srv.deps.Nodes.ListByIDs(farIDs)
	if listErr != nil {
		return nil, listErr
	}

	byID := make(map[string]index.NodeRow, len(farRows))

	for _, far := range farRows {
		byID[far.ID] = far
	}

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

		neighbors = append(neighbors, Neighbor{
			ID:        far.ID,
			Type:      far.Type,
			Title:     far.Title,
			EdgeType:  adj.edge.Type,
			Kind:      adj.edge.Kind,
			Direction: adj.direction,
		})
	}

	return neighbors, nil
}

func (srv *Server) respondSubunits(writer http.ResponseWriter, parentID string) {
	children, listErr := srv.deps.Nodes.ListByParent(parentID)
	if listErr != nil {
		http.Error(writer, listErr.Error(), http.StatusServiceUnavailable)

		return
	}

	degree, inDegree, degErr := srv.subunitDegrees(children)
	if degErr != nil {
		http.Error(writer, degErr.Error(), http.StatusServiceUnavailable)

		return
	}

	nodes := make([]GraphNode, 0, len(children))
	edges := make([]GraphEdge, 0, len(children))

	for _, child := range children {
		nodes = append(nodes, GraphNode{
			ID:       child.ID,
			Type:     child.Type,
			Title:    child.Title,
			Path:     child.Path,
			Degree:   degree[child.ID],
			InDegree: inDegree[child.ID],
		})
		// Only the forward "contains" edge is materialized; synthesize it from
		// the known parent→child relation.
		edges = append(edges, GraphEdge{Source: parentID, Target: child.ID, Type: "contains", Kind: "structural"})
	}

	writeJSON(writer, SubunitGraph{Nodes: nodes, Edges: edges})
}

// subunitDegrees tallies each child's degree / in-degree over a single ListAll
// scan, mirroring snapshot()'s metric so drill-down nodes size and brighten by
// real connectivity. Structural ("contains") edges are excluded: every sub-unit
// carries exactly one incoming contains edge from its parent, so counting it
// would add a constant +1 with no discriminating signal and diverge from the
// file-level rule (snapshot excludes contains edges by endpoint). Self-loops
// increment both endpoints, matching snapshot().
func (srv *Server) subunitDegrees(children []index.NodeRow) (map[string]int, map[string]int, error) {
	childSet := make(map[string]struct{}, len(children))
	for _, child := range children {
		childSet[child.ID] = struct{}{}
	}

	allEdges, edgeErr := srv.deps.Edges.ListAll()
	if edgeErr != nil {
		return nil, nil, edgeErr
	}

	degree := make(map[string]int, len(children))
	inDegree := make(map[string]int, len(children))

	for _, row := range allEdges {
		if row.Kind == "structural" {
			continue
		}

		if _, ok := childSet[row.SourceID]; ok {
			degree[row.SourceID]++
		}

		if _, ok := childSet[row.TargetID]; ok {
			degree[row.TargetID]++
			inDegree[row.TargetID]++
		}
	}

	return degree, inDegree, nil
}
