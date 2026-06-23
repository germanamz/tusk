package graphview

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/germanamz/tusk/internal/index"
)

// handleNode dispatches /api/node/{id} and /api/node/{id}/subunits. The id may
// contain slashes, so the wildcard captures the rest of the path and we branch
// on a trailing /subunits segment.
func (srv *Server) handleNode(writer http.ResponseWriter, request *http.Request) {
	rest := request.PathValue("id")

	if trimmed := strings.TrimSuffix(rest, "/subunits"); trimmed != rest {
		srv.respondSubunits(writer, trimmed)

		return
	}

	srv.respondDetail(writer, rest)
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

// neighborsOf returns the file-level neighbors of nodeID from ListAll, looking
// up the far end's type/title via Get.
func (srv *Server) neighborsOf(nodeID string) ([]Neighbor, error) {
	edgeRows, listErr := srv.deps.Edges.ListAll()
	if listErr != nil {
		return nil, listErr
	}

	neighbors := make([]Neighbor, 0)

	for _, row := range edgeRows {
		var (
			farID     string
			direction string
		)

		switch nodeID {
		case row.SourceID:
			farID, direction = row.TargetID, "out"
		case row.TargetID:
			farID, direction = row.SourceID, "in"
		default:
			continue
		}

		far, getErr := srv.deps.Nodes.Get(farID)
		if errors.Is(getErr, index.ErrNodeNotFound) {
			continue // dangling edge target; skip
		}

		if getErr != nil {
			return nil, getErr
		}

		// NodeRepo.Get resolves sub-unit rows too (it queries all nodes by id),
		// so we cannot rely on a not-found to exclude them. A "contains" edge's
		// target is a sub-unit (ParentID set); the file-level view excludes it.
		if far.ParentID.Valid {
			continue
		}

		neighbors = append(neighbors, Neighbor{
			ID:        far.ID,
			Type:      far.Type,
			Title:     far.Title,
			EdgeType:  row.Type,
			Kind:      row.Kind,
			Direction: direction,
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

	nodes := make([]GraphNode, 0, len(children))
	edges := make([]GraphEdge, 0, len(children))

	for _, child := range children {
		nodes = append(nodes, GraphNode{ID: child.ID, Type: child.Type, Title: child.Title, Path: child.Path})
		// Only the forward "contains" edge is materialized; synthesize it from
		// the known parent→child relation.
		edges = append(edges, GraphEdge{Source: parentID, Target: child.ID, Type: "contains", Kind: "structural"})
	}

	writeJSON(writer, SubunitGraph{Nodes: nodes, Edges: edges})
}
