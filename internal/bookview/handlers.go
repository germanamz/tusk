package bookview

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/render"
	"github.com/germanamz/tusk/internal/webui"
)

// The handlers below are registered by routes() so the route table is complete,
// but their bodies land with the tasks that own them. Each reports 501 until
// then.

// handleIndex serves the Contents pane's node index: every file-level node
// (NodeSource.ListFileNodes already excludes sub-units, filtering on
// parent_id IS NULL).
func (srv *Server) handleIndex(writer http.ResponseWriter, _ *http.Request) {
	rows, listErr := srv.deps.Nodes.ListFileNodes()

	if listErr != nil {
		http.Error(writer, listErr.Error(), http.StatusServiceUnavailable)

		return
	}

	out := IndexResponse{Nodes: make([]IndexNode, 0, len(rows))}

	for _, row := range rows {
		out.Nodes = append(out.Nodes, IndexNode{
			ID:    row.ID,
			Type:  row.Type,
			Title: row.Title,
			Path:  row.Path,
		})
	}

	writeJSON(writer, out)
}

// handleNode serves one node as a readable document: the index row's metadata
// plus the raw markdown body, read fresh from disk (node bodies are never stored
// in the index) with only the frontmatter stripped. Rendering it to HTML is the
// frontend's job, so the markup survives verbatim.
//
// The id may contain slashes, so the route captures the rest of the path;
// PathValue unescapes each segment.
func (srv *Server) handleNode(writer http.ResponseWriter, request *http.Request) {
	nodeID := request.PathValue("id")

	row, getErr := srv.deps.Nodes.Get(nodeID)

	if errors.Is(getErr, index.ErrNodeNotFound) {
		http.Error(writer, "node not found: "+nodeID, http.StatusNotFound)

		return
	}

	if getErr != nil {
		http.Error(writer, getErr.Error(), http.StatusServiceUnavailable)

		return
	}

	// Path comes from the index walk, not the request, so it is vault-relative
	// by construction — this is not the traversal-guarded surface /api/asset is.
	body, readErr := os.ReadFile(filepath.Join(srv.deps.Root, row.Path))

	if readErr != nil {
		// The row outlived its file (a stale index). The node is not missing —
		// a 404 would tell the frontend to forget an id the Contents pane still
		// lists — the source is momentarily unreadable.
		http.Error(writer, readErr.Error(), http.StatusServiceUnavailable)

		return
	}

	links, linksErr := srv.linksOf(nodeID)

	if linksErr != nil {
		http.Error(writer, linksErr.Error(), http.StatusServiceUnavailable)

		return
	}

	// PropertiesJSON is a plain string: an unset column would emit a bare
	// "properties": and invalidate the whole payload, not just the field.
	properties := json.RawMessage(row.PropertiesJSON)

	if len(properties) == 0 {
		properties = json.RawMessage("{}")
	}

	payload := NodeReadPayload{
		ID:         row.ID,
		Type:       row.Type,
		Title:      row.Title,
		Path:       row.Path,
		Properties: properties,
		Markdown:   string(render.StripFrontmatter(body)),
	}

	payload.Links.Out = links.out
	payload.Links.In = links.in

	writeJSON(writer, payload)
}

// nodeLinks holds one node's reading rails, split by direction.
type nodeLinks struct {
	out []LinkRef
	in  []LinkRef
}

// linksOf returns the file-level links of nodeID, projecting the shared webui
// traversal (incident-edge lookup, batched far-end resolution,
// self-loop-once-as-out, sub-unit and dangling skips, ListAll ordering) into the
// reading rails. The graph view projects the same traversal into its own
// neighbor payload; only the emitted struct and the policy below differ.
//
// Structural ("contains") edges are excluded: they are index plumbing rather
// than a link a reader follows. webui.Neighbors applies no view policy, and for
// a file focus its sub-unit skip already drops them — but a sub-unit focus sees
// its parent file arrive as an incoming "contains", and containment is what the
// Contents pane expresses, not the rails. Filtering here keeps that policy
// consistent from both ends instead of leaving it an accident of which side is a
// sub-unit. graphview deliberately keeps these edges, which is why the filter
// lives in bookview and not in webui.Neighbors.
func (srv *Server) linksOf(nodeID string) (nodeLinks, error) {
	adjacent, adjErr := webui.Neighbors(srv.deps.Nodes, srv.deps.Edges, nodeID)

	if adjErr != nil {
		return nodeLinks{}, adjErr
	}

	// Non-nil so an isolated node's rails marshal to [] rather than null, which
	// would force every frontend consumer to null-check before iterating.
	links := nodeLinks{out: make([]LinkRef, 0), in: make([]LinkRef, 0)}

	for _, adj := range adjacent {
		if adj.Edge.Kind == "structural" {
			continue
		}

		ref := LinkRef{
			ID:       adj.Node.ID,
			Title:    adj.Node.Title,
			Type:     adj.Node.Type,
			EdgeType: adj.Edge.Type,
		}

		if adj.Direction == "out" {
			links.out = append(links.out, ref)

			continue
		}

		links.in = append(links.in, ref)
	}

	return links, nil
}

// handleAsset serves a vault-relative asset (images referenced from node
// bodies).
func (srv *Server) handleAsset(writer http.ResponseWriter, _ *http.Request) {
	http.Error(writer, "not implemented", http.StatusNotImplemented)
}
