package bookview

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

// writeNodeFile writes body to root/relPath, creating parent directories.
func writeNodeFile(test *testing.T, root, relPath, body string) {
	test.Helper()

	full := filepath.Join(root, relPath)

	if mkdirErr := os.MkdirAll(filepath.Dir(full), 0o755); mkdirErr != nil {
		test.Fatalf("mkdir for %s: %v", relPath, mkdirErr)
	}

	if writeErr := os.WriteFile(full, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write %s: %v", relPath, writeErr)
	}
}

// getNode drives handleNode for nodeID and returns the recorder. The route
// registers {id...}, so the id arrives via PathValue rather than the URL alone.
func getNode(srv *Server, nodeID string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/node/"+nodeID, nil)
	req.SetPathValue("id", nodeID)

	srv.handleNode(rec, req)

	return rec
}

// TestNodeReadPayload pins the reading payload: metadata from the index row,
// the raw markdown body read fresh from disk with only frontmatter stripped
// (the frontend renders it, so markup must survive verbatim), and the file-level
// links split by direction.
func TestNodeReadPayload(test *testing.T) {
	root := test.TempDir()
	writeNodeFile(test, root, "a.md", "---\ntitle: A\n---\n# Heading\n\nBody $x^2$ with *emphasis*.\n")

	nodes := fakeNodes{file: []index.NodeRow{
		{ID: "a", Type: "note", Title: "A", Path: "a.md", PropertiesJSON: `{"title":"A"}`},
		{ID: "b", Type: "note", Title: "B", Path: "b.md"},
		{ID: "c", Type: "spec", Title: "C", Path: "c.md"},
	}}

	edges := fakeEdges{all: []index.EdgeRow{
		{Type: "references", SourceID: "a", TargetID: "b", SourcePath: "a.md", Kind: "derived"},
		{Type: "depends-on", SourceID: "c", TargetID: "a", SourcePath: "c.md", Kind: "direct"},
	}}

	srv := New(Deps{Root: root, Nodes: nodes, Edges: edges})

	rec := getNode(srv, "a")

	if rec.Code != http.StatusOK {
		test.Fatalf("code=%d want 200: %s", rec.Code, rec.Body.String())
	}

	var got NodeReadPayload

	if unmarshalErr := json.Unmarshal(rec.Body.Bytes(), &got); unmarshalErr != nil {
		test.Fatalf("unmarshal %q: %v", rec.Body.Bytes(), unmarshalErr)
	}

	if got.ID != "a" || got.Type != "note" || got.Title != "A" || got.Path != "a.md" {
		test.Fatalf("metadata = %+v", got)
	}

	if string(got.Properties) != `{"title":"A"}` {
		test.Fatalf("properties=%s want %s", got.Properties, `{"title":"A"}`)
	}

	if strings.Contains(got.Markdown, "title: A") {
		test.Fatalf("frontmatter not stripped: %q", got.Markdown)
	}

	// The markup itself must survive: the browser renders it, so a stripped
	// heading hash or emphasis marker would reach the reader as plain prose.
	for _, want := range []string{"# Heading", "$x^2$", "*emphasis*"} {
		if !strings.Contains(got.Markdown, want) {
			test.Fatalf("markdown %q missing %q", got.Markdown, want)
		}
	}

	if len(got.Links.Out) != 1 || got.Links.Out[0] != (LinkRef{ID: "b", Title: "B", Type: "note", EdgeType: "references"}) {
		test.Fatalf("links.out=%+v", got.Links.Out)
	}

	if len(got.Links.In) != 1 || got.Links.In[0] != (LinkRef{ID: "c", Title: "C", Type: "spec", EdgeType: "depends-on"}) {
		test.Fatalf("links.in=%+v", got.Links.In)
	}
}

// TestNodeRouteServesSlashedID drives the real mux over HTTP rather than
// calling the handler directly. Node ids are path-derived, so a nested file's id
// contains slashes — the {id...} wildcard must capture the rest of the path and
// hand it over intact, which a direct SetPathValue call cannot prove.
func TestNodeRouteServesSlashedID(test *testing.T) {
	root := test.TempDir()
	writeNodeFile(test, root, "specs/deep/b.md", "---\ntitle: B\n---\n# Deep\n")

	nodes := fakeNodes{file: []index.NodeRow{
		{ID: "specs/deep/b", Type: "spec", Title: "B", Path: "specs/deep/b.md"},
	}}

	srv := New(Deps{Root: root, Nodes: nodes, Edges: fakeEdges{}})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp, getErr := http.Get(server.URL + "/api/node/specs/deep/b")

	if getErr != nil {
		test.Fatalf("get: %v", getErr)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		test.Fatalf("code=%d want 200", resp.StatusCode)
	}

	var got NodeReadPayload

	if decodeErr := json.NewDecoder(resp.Body).Decode(&got); decodeErr != nil {
		test.Fatalf("decode: %v", decodeErr)
	}

	if got.ID != "specs/deep/b" || got.Path != "specs/deep/b.md" {
		test.Fatalf("payload id=%q path=%q, want the slashed id intact", got.ID, got.Path)
	}

	if !strings.Contains(got.Markdown, "# Deep") {
		test.Fatalf("markdown=%q", got.Markdown)
	}
}

// TestNodeNotFound pins the 404: an id with no index row is a missing document,
// not an empty one. webui.Neighbors would happily return zero links for an
// unknown id, so without the Get check a typo'd id would render as a valid,
// blank page.
func TestNodeNotFound(test *testing.T) {
	srv := New(Deps{Root: test.TempDir(), Nodes: fakeNodes{}, Edges: fakeEdges{}})

	if rec := getNode(srv, "missing"); rec.Code != http.StatusNotFound {
		test.Fatalf("code=%d want 404", rec.Code)
	}
}

// TestNodeBodyMissingFromDisk pins the stale-index path: the row survives in the
// index but its file is gone. That is an unreadable source, not a missing node —
// 404 would tell the frontend to forget an id the index still lists.
func TestNodeBodyMissingFromDisk(test *testing.T) {
	nodes := fakeNodes{file: []index.NodeRow{{ID: "a", Type: "note", Title: "A", Path: "a.md"}}}

	srv := New(Deps{Root: test.TempDir(), Nodes: nodes, Edges: fakeEdges{}})

	if rec := getNode(srv, "a"); rec.Code != http.StatusServiceUnavailable {
		test.Fatalf("code=%d want 503", rec.Code)
	}
}

// TestNodeLinksMarshalEmptyArray guards the nil-slice trap at the wire-byte
// level: an isolated node's links must marshal to [] rather than null. A JSON
// decode cannot tell the two apart, so only the raw bytes can pin this.
func TestNodeLinksMarshalEmptyArray(test *testing.T) {
	root := test.TempDir()
	writeNodeFile(test, root, "a.md", "---\ntitle: A\n---\nBody\n")

	nodes := fakeNodes{file: []index.NodeRow{{ID: "a", Type: "note", Title: "A", Path: "a.md"}}}

	srv := New(Deps{Root: root, Nodes: nodes, Edges: fakeEdges{}})

	body := getNode(srv, "a").Body.String()

	if !strings.Contains(body, `"links":{"out":[],"in":[]}`) {
		test.Fatalf("body=%s want links.out/links.in as []", body)
	}
}

// TestNodeEmptyPropertiesMarshalObject pins the empty-column path: NodeRow's
// PropertiesJSON is a plain string, so an unset column would emit a bare
// "properties": on the wire — syntactically invalid JSON that breaks the whole
// payload, not just the field.
func TestNodeEmptyPropertiesMarshalObject(test *testing.T) {
	root := test.TempDir()
	writeNodeFile(test, root, "a.md", "Body only\n")

	nodes := fakeNodes{file: []index.NodeRow{{ID: "a", Type: "note", Title: "A", Path: "a.md"}}}

	srv := New(Deps{Root: root, Nodes: nodes, Edges: fakeEdges{}})

	rec := getNode(srv, "a")

	if !strings.Contains(rec.Body.String(), `"properties":{}`) {
		test.Fatalf("body=%s want properties as {}", rec.Body.String())
	}

	var got NodeReadPayload

	if unmarshalErr := json.Unmarshal(rec.Body.Bytes(), &got); unmarshalErr != nil {
		test.Fatalf("unmarshal %q: %v", rec.Body.Bytes(), unmarshalErr)
	}

	// A body with no frontmatter block is returned unchanged.
	if got.Markdown != "Body only\n" {
		test.Fatalf("markdown=%q want %q", got.Markdown, "Body only\n")
	}
}

// TestNodeExcludesStructuralLinks pins the view policy: a structural "contains"
// edge is index plumbing, not a link a reader follows. webui.Neighbors applies
// no view policy and already drops the sub-unit far end of a file's own contains
// edges, so the case that reaches bookview is the sub-unit focus — where the
// parent file arrives as an incoming "contains". The reading rails exclude it.
func TestNodeExcludesStructuralLinks(test *testing.T) {
	root := test.TempDir()
	writeNodeFile(test, root, "a.md", "---\ntitle: A\n---\n## Section\n\nBody\n")

	nodes := fakeNodes{file: []index.NodeRow{
		{ID: "a", Type: "note", Title: "A", Path: "a.md"},
		{
			ID:       "a#section",
			Type:     "note",
			Title:    "Section",
			Path:     "a.md",
			ParentID: sql.NullString{String: "a", Valid: true},
		},
		{ID: "b", Type: "note", Title: "B", Path: "b.md"},
	}}

	edges := fakeEdges{all: []index.EdgeRow{
		{
			Type:       "contains",
			SourceID:   "a",
			TargetID:   "a#section",
			SourcePath: "a.md",
			Kind:       "structural",
			Source:     sql.NullString{String: "markdown", Valid: true},
		},
		{Type: "references", SourceID: "a#section", TargetID: "b", SourcePath: "a.md", Kind: "derived"},
	}}

	srv := New(Deps{Root: root, Nodes: nodes, Edges: edges})

	// The file focus: its contains edge points at a sub-unit, which Neighbors
	// already drops — no structural link either way.
	var fileGot NodeReadPayload

	if unmarshalErr := json.Unmarshal(getNode(srv, "a").Body.Bytes(), &fileGot); unmarshalErr != nil {
		test.Fatalf("unmarshal file payload: %v", unmarshalErr)
	}

	if len(fileGot.Links.Out) != 0 || len(fileGot.Links.In) != 0 {
		test.Fatalf("file links out=%+v in=%+v, want none", fileGot.Links.Out, fileGot.Links.In)
	}

	// The sub-unit focus: the parent's contains edge resolves to a file-level
	// far end, so only bookview's Kind filter keeps it out of the rails.
	var unitGot NodeReadPayload

	if unmarshalErr := json.Unmarshal(getNode(srv, "a#section").Body.Bytes(), &unitGot); unmarshalErr != nil {
		test.Fatalf("unmarshal sub-unit payload: %v", unmarshalErr)
	}

	if len(unitGot.Links.In) != 0 {
		test.Fatalf("sub-unit links.in=%+v, want the structural contains excluded", unitGot.Links.In)
	}

	if len(unitGot.Links.Out) != 1 || unitGot.Links.Out[0].ID != "b" {
		test.Fatalf("sub-unit links.out=%+v, want the derived reference kept", unitGot.Links.Out)
	}
}
