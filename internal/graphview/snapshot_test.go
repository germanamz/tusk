package graphview

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func newGraphFixtureDeps() Deps {
	nodes := &fakeNodes{
		files: []index.NodeRow{
			fileRow("notes/a", "note", "A", `{"tags":["x","y"]}`),
			fileRow("notes/b", "note", "B", ""),
			fileRow("notes/c", "note", "C", ""),
		},
	}
	edges := &fakeEdges{
		all: []index.EdgeRow{
			edge("references", "notes/a", "notes/b", "direct"),      // file<->file: kept
			edge("contains", "notes/a", "notes/a#s1", "structural"), // file->subunit: dropped from file graph
		},
	}

	return Deps{Nodes: nodes, Edges: edges, Changes: &fakeChanges{sig: Signal{Generation: 7, Epoch: 1}}}
}

func TestSnapshot_FileLevelOnly(t *testing.T) {
	srv := New(newGraphFixtureDeps())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/graph")
	if err != nil {
		t.Fatalf("GET /api/graph: %v", err)
	}
	defer resp.Body.Close()

	var graph Graph
	if decodeErr := json.NewDecoder(resp.Body).Decode(&graph); decodeErr != nil {
		t.Fatalf("decode: %v", decodeErr)
	}

	if graph.Generation != 7 || graph.Epoch != 1 {
		t.Fatalf("signal = (%d,%d), want (7,1)", graph.Generation, graph.Epoch)
	}

	if len(graph.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(graph.Nodes))
	}

	if len(graph.Edges) != 1 {
		t.Fatalf("edges = %d, want 1 (subunit contains edge dropped)", len(graph.Edges))
	}

	var nodeA GraphNode
	for _, node := range graph.Nodes {
		if node.ID == "notes/a" {
			nodeA = node
		}
	}

	if nodeA.Degree != 1 {
		t.Fatalf("notes/a degree = %d, want 1", nodeA.Degree)
	}

	if len(nodeA.Tags) != 2 || nodeA.Tags[0] != "x" {
		t.Fatalf("notes/a tags = %v, want [x y]", nodeA.Tags)
	}
}
