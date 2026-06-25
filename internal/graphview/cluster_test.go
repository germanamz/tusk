package graphview

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// newClusterDeps builds a Deps with a manifest that has the given GraphCluster
// config. Reuses the file-node set from the standard fixture (three notes).
func newClusterDeps(cluster manifest.GraphCluster) Deps {
	mf := &manifest.Manifest{}
	mf.GraphCluster = cluster

	nodes := &fakeNodes{
		files: []index.NodeRow{
			fileRow("notes/a", "note", "A", `{"team":"eng","tags":["x"]}`),
			fileRow("notes/b", "spec", "B", `{"team":"design"}`),
			fileRow("notes/c", "note", "C", ``),
		},
	}
	edges := &fakeEdges{}

	return Deps{
		Nodes:    nodes,
		Edges:    edges,
		Changes:  &fakeChanges{sig: Signal{Generation: 1, Epoch: 1}},
		Manifest: mf,
	}
}

// decodeGraphFromServer is a helper that starts a test server, hits
// /api/graph, and decodes the response into a Graph.
func decodeGraphFromServer(test *testing.T, deps Deps) Graph {
	test.Helper()

	srv := New(deps)
	ts := httptest.NewServer(srv.Handler())

	defer ts.Close()

	resp, reqErr := http.Get(ts.URL + "/api/graph")

	if reqErr != nil {
		test.Fatalf("GET /api/graph: %v", reqErr)
	}

	defer resp.Body.Close()

	var graph Graph

	if decodeErr := json.NewDecoder(resp.Body).Decode(&graph); decodeErr != nil {
		test.Fatalf("decode: %v", decodeErr)
	}

	return graph
}

// nodeByID returns the GraphNode with the given id from nodes, or fails.
func nodeByID(test *testing.T, nodes []GraphNode, id string) GraphNode {
	test.Helper()

	for _, nd := range nodes {
		if nd.ID == id {
			return nd
		}
	}

	test.Fatalf("node %q not found in snapshot", id)

	return GraphNode{}
}

// TestSnapshot_GroupByType confirms that with by = "type", Group == Type for
// every node, and the returned Cluster.By is "type".
func TestSnapshot_GroupByType(test *testing.T) {
	cfg := manifest.GraphCluster{By: "type"}
	graph := decodeGraphFromServer(test, newClusterDeps(cfg))

	if graph.Cluster.By != "type" {
		test.Errorf("Cluster.By = %q, want %q", graph.Cluster.By, "type")
	}

	for _, nd := range graph.Nodes {
		if nd.Group != nd.Type {
			test.Errorf("node %q: Group = %q, want Type = %q", nd.ID, nd.Group, nd.Type)
		}
	}
}

// TestSnapshot_GroupByTypeNilManifest confirms that a nil Manifest (no config
// at all) is treated as by = "type", matching the default.
func TestSnapshot_GroupByTypeNilManifest(test *testing.T) {
	deps := Deps{
		Nodes: &fakeNodes{
			files: []index.NodeRow{
				fileRow("notes/a", "note", "A", ""),
				fileRow("notes/b", "spec", "B", ""),
			},
		},
		Edges:   &fakeEdges{},
		Changes: &fakeChanges{sig: Signal{Generation: 1, Epoch: 1}},
		// Manifest intentionally nil.
	}

	graph := decodeGraphFromServer(test, deps)

	for _, nd := range graph.Nodes {
		if nd.Group != nd.Type {
			test.Errorf("node %q: Group = %q, want Type = %q (nil manifest)", nd.ID, nd.Group, nd.Type)
		}
	}
}

// TestSnapshot_GroupByProperty confirms that with by = "property" and
// property = "team", Group equals the team field value, and nodes missing the
// field get an empty group.
func TestSnapshot_GroupByProperty(test *testing.T) {
	cfg := manifest.GraphCluster{By: "property", Property: "team"}
	graph := decodeGraphFromServer(test, newClusterDeps(cfg))

	if graph.Cluster.By != "property" {
		test.Errorf("Cluster.By = %q, want %q", graph.Cluster.By, "property")
	}

	if graph.Cluster.Property != "team" {
		test.Errorf("Cluster.Property = %q, want %q", graph.Cluster.Property, "team")
	}

	nodeA := nodeByID(test, graph.Nodes, "notes/a")
	if nodeA.Group != "eng" {
		test.Errorf("notes/a Group = %q, want %q", nodeA.Group, "eng")
	}

	nodeB := nodeByID(test, graph.Nodes, "notes/b")
	if nodeB.Group != "design" {
		test.Errorf("notes/b Group = %q, want %q", nodeB.Group, "design")
	}

	nodeC := nodeByID(test, graph.Nodes, "notes/c")
	if nodeC.Group != "" {
		test.Errorf("notes/c Group = %q, want %q (absent field)", nodeC.Group, "")
	}
}

// TestSnapshot_ClusterMetaDefaultHuddleFalse confirms that Cluster.Huddle is
// always false in Phase 2 (it becomes meaningful in Phase 4).
func TestSnapshot_ClusterMetaDefaultHuddleFalse(test *testing.T) {
	cfg := manifest.GraphCluster{By: "type"}
	graph := decodeGraphFromServer(test, newClusterDeps(cfg))

	if graph.Cluster.Huddle {
		test.Errorf("Cluster.Huddle = true, want false (Phase 4 enables this)")
	}
}
