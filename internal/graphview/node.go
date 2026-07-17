package graphview

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/webui"
)

// handleNodeDetail serves GET /api/graph/node/{id...}. The id may contain slashes, so
// the wildcard captures the rest of the path; PathValue unescapes each segment.
func (srv *Server) handleNodeDetail(writer http.ResponseWriter, request *http.Request) {
	srv.respondDetail(writer, request.PathValue("id"))
}

// handleSubunits serves GET /api/graph/subunits/{id...}. A separate top-level prefix
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

// neighborsOf returns the file-level neighbors of nodeID, projecting the
// shared webui traversal (incident-edge lookup, batched far-end resolution,
// self-loop-once-as-out, sub-unit and dangling skips, ListAll ordering) into
// the graph view's own Neighbor payload. The book view projects the same
// traversal into its link shape; only the emitted struct differs.
func (srv *Server) neighborsOf(nodeID string) ([]Neighbor, error) {
	adjacent, adjErr := webui.Neighbors(srv.deps.Nodes, srv.deps.Edges, nodeID)

	if adjErr != nil {
		return nil, adjErr
	}

	// Non-nil so NodeDetail.Neighbors marshals to [] rather than null for a
	// node with no neighbors.
	neighbors := make([]Neighbor, 0, len(adjacent))

	for _, adj := range adjacent {
		neighbors = append(neighbors, Neighbor{
			ID:        adj.Node.ID,
			Type:      adj.Node.Type,
			Title:     adj.Node.Title,
			EdgeType:  adj.Edge.Type,
			Kind:      adj.Edge.Kind,
			Direction: adj.Direction,
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
