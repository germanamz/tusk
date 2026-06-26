package graphview

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
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

// newAncestorFixtureDeps builds a two-branch hierarchy:
//
//	orgA  <- teamA1 <- personA1
//	orgB  <- teamB1 <- personB1
//	offHierarchy (no parent edge)
//
// All connected via "parent" edges (child→parent direction).
func newAncestorFixtureDeps(clusterCfg manifest.GraphCluster) Deps {
	nodes := &fakeNodes{
		files: []index.NodeRow{
			fileRow("orgA", "org", "Org A", ""),
			fileRow("teamA1", "team", "Team A1", ""),
			fileRow("personA1", "person", "Person A1", ""),
			fileRow("orgB", "org", "Org B", ""),
			fileRow("teamB1", "team", "Team B1", ""),
			fileRow("personB1", "person", "Person B1", ""),
			fileRow("offHierarchy", "note", "Off Hierarchy", ""),
		},
	}
	edgesRepo := &fakeEdges{
		all: []index.EdgeRow{
			edge("parent", "teamA1", "orgA", "direct"),
			edge("parent", "personA1", "teamA1", "direct"),
			edge("parent", "teamB1", "orgB", "direct"),
			edge("parent", "personB1", "teamB1", "direct"),
		},
	}

	return Deps{
		Nodes:    nodes,
		Edges:    edgesRepo,
		Changes:  &fakeChanges{sig: Signal{Generation: 1, Epoch: 1}},
		Manifest: &manifest.Manifest{GraphCluster: clusterCfg},
	}
}

// getGraph fires a GET /api/graph against a fresh server and decodes the response.
func getGraph(test *testing.T, deps Deps) Graph {
	test.Helper()

	srv := New(deps)
	ts := httptest.NewServer(srv.Handler())

	test.Cleanup(ts.Close)

	resp, respErr := http.Get(ts.URL + "/api/graph")

	if respErr != nil {
		test.Fatalf("GET /api/graph: %v", respErr)
	}

	defer resp.Body.Close()

	var gr Graph

	if decodeErr := json.NewDecoder(resp.Body).Decode(&gr); decodeErr != nil {
		test.Fatalf("decode: %v", decodeErr)
	}

	return gr
}

// TestSnapshot_AncestorProducer_GroupsByBranchRoot confirms that by="ancestor"
// groups each node to its branch root and that off-hierarchy nodes get their
// own id as the group.
func TestSnapshot_AncestorProducer_GroupsByBranchRoot(test *testing.T) {
	deps := newAncestorFixtureDeps(manifest.GraphCluster{By: "ancestor", Edge: "parent"})
	gr := getGraph(test, deps)

	if gr.Cluster.By != "ancestor" {
		test.Errorf("cluster.by = %q, want %q", gr.Cluster.By, "ancestor")
	}

	if gr.Cluster.Property != "" {
		test.Errorf("cluster.property = %q, want empty for ancestor producer", gr.Cluster.Property)
	}

	nodeGroupByID := make(map[string]string, len(gr.Nodes))

	for _, node := range gr.Nodes {
		nodeGroupByID[node.ID] = node.Group
	}

	// Branch A nodes must all map to orgA.
	for _, nodeID := range []string{"orgA", "teamA1", "personA1"} {
		got := nodeGroupByID[nodeID]

		if got != "orgA" {
			test.Errorf("node %q: group = %q, want %q", nodeID, got, "orgA")
		}
	}

	// Branch B nodes must all map to orgB.
	for _, nodeID := range []string{"orgB", "teamB1", "personB1"} {
		got := nodeGroupByID[nodeID]

		if got != "orgB" {
			test.Errorf("node %q: group = %q, want %q", nodeID, got, "orgB")
		}
	}

	// Off-hierarchy node must get its own id as the group (distinct singleton).
	gotOff := nodeGroupByID["offHierarchy"]

	if gotOff != "offHierarchy" {
		test.Errorf("offHierarchy: group = %q, want own id %q", gotOff, "offHierarchy")
	}
}

// TestSnapshot_AncestorProducer_GroupsMustBeNonEmpty confirms every node in
// an ancestor snapshot has a non-empty group.
func TestSnapshot_AncestorProducer_GroupsMustBeNonEmpty(test *testing.T) {
	deps := newAncestorFixtureDeps(manifest.GraphCluster{By: "ancestor", Edge: "parent"})
	gr := getGraph(test, deps)

	for _, node := range gr.Nodes {
		if node.Group == "" {
			test.Errorf("node %q has empty group under ancestor producer", node.ID)
		}
	}
}

// TestSnapshot_TypeProducer_Regression confirms that by="type" (the default)
// still produces Group == Type for every node and the cluster meta is correct,
// regardless of the ancestor feature being present.
func TestSnapshot_TypeProducer_Regression(test *testing.T) {
	// Use a nil Manifest (defaults to by="type").
	deps := newGraphFixtureDeps()
	gr := getGraph(test, deps)

	if gr.Cluster.By != "type" {
		test.Errorf("cluster.by = %q, want %q", gr.Cluster.By, "type")
	}

	for _, node := range gr.Nodes {
		if node.Group != node.Type {
			test.Errorf("node %q: group = %q, want type %q", node.ID, node.Group, node.Type)
		}
	}
}

func TestSnapshot_FileLevelOnly(test *testing.T) {
	srv := New(newGraphFixtureDeps())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, respErr := http.Get(ts.URL + "/api/graph")

	if respErr != nil {
		test.Fatalf("GET /api/graph: %v", respErr)
	}

	defer resp.Body.Close()

	var graph Graph

	if decodeErr := json.NewDecoder(resp.Body).Decode(&graph); decodeErr != nil {
		test.Fatalf("decode: %v", decodeErr)
	}

	if graph.Generation != 7 || graph.Epoch != 1 {
		test.Fatalf("signal = (%d,%d), want (7,1)", graph.Generation, graph.Epoch)
	}

	if len(graph.Nodes) != 3 {
		test.Fatalf("nodes = %d, want 3", len(graph.Nodes))
	}

	if len(graph.Edges) != 1 {
		test.Fatalf("edges = %d, want 1 (subunit contains edge dropped)", len(graph.Edges))
	}

	var nodeA, nodeB GraphNode
	for _, node := range graph.Nodes {
		if node.ID == "notes/a" {
			nodeA = node
		}
		if node.ID == "notes/b" {
			nodeB = node
		}
	}

	if nodeA.Degree != 1 {
		test.Fatalf("notes/a degree = %d, want 1", nodeA.Degree)
	}

	if nodeA.InDegree != 0 {
		test.Fatalf("notes/a in_degree = %d, want 0 (source of the only kept edge)", nodeA.InDegree)
	}

	if nodeB.InDegree != 1 {
		test.Fatalf("notes/b in_degree = %d, want 1 (target of notes/a -> notes/b)", nodeB.InDegree)
	}

	if len(nodeA.Tags) != 2 || nodeA.Tags[0] != "x" {
		test.Fatalf("notes/a tags = %v, want [x y]", nodeA.Tags)
	}
}
