package bookview

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/render"
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
// The reading rails come from linksOf, which projects links to file level: a
// link authored in a section of another note surfaces as that note.
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

	// Resolved against the stripped body rather than the raw one, because this
	// is the text the client rewrites: a frontmatter ref never appears in it, so
	// an entry for one could only be a key nothing matches.
	markdown := render.StripFrontmatter(body)

	wikilinks, wikilinksErr := srv.resolveWikilinks(markdown)

	if wikilinksErr != nil {
		http.Error(writer, wikilinksErr.Error(), http.StatusServiceUnavailable)

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
		Markdown:   string(markdown),
		Wikilinks:  wikilinks,
	}

	payload.Links.Out = links.out
	payload.Links.In = links.in

	writeJSON(writer, payload)
}

// handleAsset serves a vault-relative asset (images referenced from node
// bodies).
func (srv *Server) handleAsset(writer http.ResponseWriter, _ *http.Request) {
	http.Error(writer, "not implemented", http.StatusNotImplemented)
}
