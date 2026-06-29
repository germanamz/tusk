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

// newSubunitsIDFixtureDeps builds a FILE node whose id ends in "/subunits"
// (notes/subunits) with one child, so the detail route and the subunits route
// can be exercised against the same id. Before E9 split the routes, a GET on
// /api/node/notes/subunits was read as "subunits of notes" and never returned
// the file's detail.
func newSubunitsIDFixtureDeps() Deps {
	nodes := &fakeNodes{
		byID: map[string]index.NodeRow{
			"notes/subunits":    fileRow("notes/subunits", "note", "Subunits", ""),
			"notes/subunits#s1": subRow("notes/subunits#s1", "notes/subunits", "section", "Section 1"),
		},
		children: map[string][]index.NodeRow{
			"notes/subunits": {subRow("notes/subunits#s1", "notes/subunits", "section", "Section 1")},
		},
	}
	edges := &fakeEdges{all: []index.EdgeRow{
		edge("contains", "notes/subunits", "notes/subunits#s1", "structural"),
	}}
	render := &fakeRenderer{text: map[string]string{"notes/subunits": "Subunits body text"}}

	return Deps{Nodes: nodes, Edges: edges, Render: render}
}

func TestNodeDetail_IdEndingInSubunits(t *testing.T) {
	srv := New(newSubunitsIDFixtureDeps())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	detailResp, detailErr := http.Get(ts.URL + "/api/node/notes/subunits")
	if detailErr != nil {
		t.Fatalf("GET detail: %v", detailErr)
	}
	defer detailResp.Body.Close()

	if detailResp.StatusCode != http.StatusOK {
		t.Fatalf("detail status = %d, want 200", detailResp.StatusCode)
	}

	var detail NodeDetail
	if decodeErr := json.NewDecoder(detailResp.Body).Decode(&detail); decodeErr != nil {
		t.Fatalf("decode detail: %v", decodeErr)
	}

	if detail.ID != "notes/subunits" {
		t.Fatalf("detail.ID = %q, want notes/subunits", detail.ID)
	}

	subResp, subErr := http.Get(ts.URL + "/api/subunits/notes/subunits")
	if subErr != nil {
		t.Fatalf("GET subunits: %v", subErr)
	}
	defer subResp.Body.Close()

	var sub SubunitGraph
	if decodeErr := json.NewDecoder(subResp.Body).Decode(&sub); decodeErr != nil {
		t.Fatalf("decode subunits: %v", decodeErr)
	}

	if len(sub.Nodes) != 1 || sub.Nodes[0].ID != "notes/subunits#s1" {
		t.Fatalf("subunit nodes = %+v", sub.Nodes)
	}
}

func TestNodeSubunits(t *testing.T) {
	srv := New(newNodeFixtureDeps())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/subunits/notes/a")
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

// newSubunitDegreeFixtureDeps builds a parent with three children of differing
// connectivity so the drill-down degree tally can be exercised:
//   - hub:  one outgoing direct + one incoming derived edge -> degree 2, in 1
//   - leaf: only the parent "contains" edge -> degree 0 (structural excluded)
//   - self: a self-loop -> degree 2, in 1 (both endpoints, matching snapshot)
func newSubunitDegreeFixtureDeps() Deps {
	nodes := &fakeNodes{
		byID: map[string]index.NodeRow{
			"notes/p":      fileRow("notes/p", "note", "P", ""),
			"notes/p#hub":  subRow("notes/p#hub", "notes/p", "section", "Hub"),
			"notes/p#leaf": subRow("notes/p#leaf", "notes/p", "section", "Leaf"),
			"notes/p#self": subRow("notes/p#self", "notes/p", "section", "Self"),
		},
		children: map[string][]index.NodeRow{
			"notes/p": {
				subRow("notes/p#hub", "notes/p", "section", "Hub"),
				subRow("notes/p#leaf", "notes/p", "section", "Leaf"),
				subRow("notes/p#self", "notes/p", "section", "Self"),
			},
		},
	}
	edges := &fakeEdges{all: []index.EdgeRow{
		// Structural parent->child edges MUST NOT count toward sub-unit degree.
		edge("contains", "notes/p", "notes/p#hub", "structural"),
		edge("contains", "notes/p", "notes/p#leaf", "structural"),
		edge("contains", "notes/p", "notes/p#self", "structural"),
		// hub: one out (direct), one in (derived).
		edge("references", "notes/p#hub", "notes/x", "direct"),
		edge("mentions", "notes/y", "notes/p#hub", "derived"),
		// self-loop: counted at both endpoints, matching snapshot() semantics.
		edge("related", "notes/p#self", "notes/p#self", "direct"),
		// leaf: no non-structural edges.
	}}

	return Deps{Nodes: nodes, Edges: edges}
}

func TestNodeSubunits_Degree(t *testing.T) {
	srv := New(newSubunitDegreeFixtureDeps())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/subunits/notes/p")
	if err != nil {
		t.Fatalf("GET subunits: %v", err)
	}
	defer resp.Body.Close()

	var sub SubunitGraph
	if decodeErr := json.NewDecoder(resp.Body).Decode(&sub); decodeErr != nil {
		t.Fatalf("decode: %v", decodeErr)
	}

	byID := make(map[string]GraphNode, len(sub.Nodes))
	for _, node := range sub.Nodes {
		byID[node.ID] = node
	}

	if got := byID["notes/p#hub"]; got.Degree != 2 || got.InDegree != 1 {
		t.Fatalf("hub degree=%d in_degree=%d, want 2/1", got.Degree, got.InDegree)
	}

	// Only the parent "contains" edge touches leaf; structural edges are excluded.
	if got := byID["notes/p#leaf"]; got.Degree != 0 || got.InDegree != 0 {
		t.Fatalf("leaf degree=%d in_degree=%d, want 0/0 (structural contains excluded)", got.Degree, got.InDegree)
	}

	// A self-loop increments both endpoints, matching snapshot() degree semantics.
	if got := byID["notes/p#self"]; got.Degree != 2 || got.InDegree != 1 {
		t.Fatalf("self degree=%d in_degree=%d, want 2/1", got.Degree, got.InDegree)
	}
}
