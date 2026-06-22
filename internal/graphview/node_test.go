package graphview

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

// subRow builds a sub-unit NodeRow under parentID (ParentID set). First used here.
func subRow(id, parentID, nodeType, title string) index.NodeRow {
	return index.NodeRow{ID: id, Type: nodeType, Title: title, Path: parentID + ".md", ParentID: sql.NullString{String: parentID, Valid: true}}
}

type fakeRenderer struct {
	text map[string]string
}

func (fake *fakeRenderer) Render(nodeID string) (string, error) { return fake.text[nodeID], nil }

func newNodeFixtureDeps() Deps {
	nodes := &fakeNodes{
		byID: map[string]index.NodeRow{
			"notes/a":    fileRow("notes/a", "note", "A", `{"status":"open"}`),
			"notes/b":    fileRow("notes/b", "note", "B", ""),
			"notes/a#s1": subRow("notes/a#s1", "notes/a", "section", "Section 1"), // a real node row — Get() finds it
		},
		children: map[string][]index.NodeRow{
			"notes/a": {subRow("notes/a#s1", "notes/a", "section", "Section 1")},
		},
	}
	edges := &fakeEdges{all: []index.EdgeRow{
		edge("references", "notes/a", "notes/b", "direct"),
		edge("contains", "notes/a", "notes/a#s1", "structural"), // sub-unit far-end: MUST NOT be a file-level neighbor
	}}
	render := &fakeRenderer{text: map[string]string{"notes/a": "A body text"}}

	return Deps{Nodes: nodes, Edges: edges, Render: render}
}

func TestNodeDetail(t *testing.T) {
	srv := New(newNodeFixtureDeps())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/node/notes/a")
	if err != nil {
		t.Fatalf("GET detail: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var detail NodeDetail
	if decodeErr := json.NewDecoder(resp.Body).Decode(&detail); decodeErr != nil {
		t.Fatalf("decode: %v", decodeErr)
	}

	if detail.Rendered != "A body text" {
		t.Fatalf("rendered = %q", detail.Rendered)
	}

	if len(detail.Neighbors) != 1 || detail.Neighbors[0].ID != "notes/b" || detail.Neighbors[0].Direction != "out" {
		t.Fatalf("neighbors = %+v", detail.Neighbors)
	}
}

func TestNodeDetail_NotFound(t *testing.T) {
	srv := New(newNodeFixtureDeps())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/node/notes/missing")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestNodeSubunits(t *testing.T) {
	srv := New(newNodeFixtureDeps())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/node/notes/a/subunits")
	if err != nil {
		t.Fatalf("GET subunits: %v", err)
	}
	defer resp.Body.Close()

	var sub SubunitGraph
	if decodeErr := json.NewDecoder(resp.Body).Decode(&sub); decodeErr != nil {
		t.Fatalf("decode: %v", decodeErr)
	}

	if len(sub.Nodes) != 1 || sub.Nodes[0].ID != "notes/a#s1" {
		t.Fatalf("subunit nodes = %+v", sub.Nodes)
	}

	if len(sub.Edges) != 1 || sub.Edges[0].Source != "notes/a" || sub.Edges[0].Kind != "structural" {
		t.Fatalf("subunit edges = %+v", sub.Edges)
	}
}
