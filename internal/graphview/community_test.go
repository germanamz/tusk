package graphview

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/germanamz/tusk/internal/graphcluster"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// communityDeps builds a Deps value with the given cluster config. Changes uses
// the provided fakeChanges so tests can advance the generation between snapshots.
func communityDeps(clusterCfg manifest.GraphCluster, nodes []index.NodeRow, edgeRows []index.EdgeRow, changes *fakeChanges) Deps {
	nodesByID := make(map[string]index.NodeRow, len(nodes))

	for _, row := range nodes {
		nodesByID[row.ID] = row
	}

	return Deps{
		Nodes:    &fakeNodes{files: nodes, byID: nodesByID},
		Edges:    &fakeEdges{all: edgeRows},
		Changes:  changes,
		Manifest: &manifest.Manifest{GraphCluster: clusterCfg},
	}
}

// nodeGroupMap extracts a nodeID->group map from a Graph snapshot.
func nodeGroupMap(graph Graph) map[string]string {
	out := make(map[string]string, len(graph.Nodes))

	for _, node := range graph.Nodes {
		out[node.ID] = node.Group
	}

	return out
}

// TestCommunity_CrossTypeGrouping confirms that nodes of different types that
// are densely linked share one community group while a separate cluster of nodes
// gets a different group — proving community grouping ignores node type.
//
// Fixture:
//
//	Triangle A: team/a, system/b, person/c — fully connected via "depends-on" edges
//	Triangle B: team/d, system/e, person/f — fully connected via "depends-on" edges
//	No cross-triangle edges
func TestCommunity_CrossTypeGrouping(test *testing.T) {
	nodes := []index.NodeRow{
		fileRow("team/a", "team", "Team A", ""),
		fileRow("system/b", "system", "System B", ""),
		fileRow("person/c", "person", "Person C", ""),
		fileRow("team/d", "team", "Team D", ""),
		fileRow("system/e", "system", "System E", ""),
		fileRow("person/f", "person", "Person F", ""),
	}

	edgeRows := []index.EdgeRow{
		// Triangle A
		edge("depends-on", "team/a", "system/b", "direct"),
		edge("depends-on", "system/b", "person/c", "direct"),
		edge("depends-on", "team/a", "person/c", "direct"),
		// Triangle B
		edge("depends-on", "team/d", "system/e", "direct"),
		edge("depends-on", "system/e", "person/f", "direct"),
		edge("depends-on", "team/d", "person/f", "direct"),
	}

	cfg := manifest.GraphCluster{
		By:             "community",
		CommunityEdges: []string{"depends-on"},
		Resolution:     1.0,
	}

	changes := &fakeChanges{sig: Signal{Generation: 1, Epoch: 1}}
	srv := New(communityDeps(cfg, nodes, edgeRows, changes))

	graph, snapErr := srv.snapshot()

	if snapErr != nil {
		test.Fatalf("snapshot: %v", snapErr)
	}

	if graph.Cluster.By != "community" {
		test.Errorf("cluster.by = %q, want %q", graph.Cluster.By, "community")
	}

	groups := nodeGroupMap(graph)

	// All three members of triangle A must share one group.
	groupA := groups["team/a"]

	for _, nodeID := range []string{"system/b", "person/c"} {
		if groups[nodeID] != groupA {
			test.Errorf("node %q: group = %q, want same as team/a (%q)", nodeID, groups[nodeID], groupA)
		}
	}

	// All three members of triangle B must share one group.
	groupB := groups["team/d"]

	for _, nodeID := range []string{"system/e", "person/f"} {
		if groups[nodeID] != groupB {
			test.Errorf("node %q: group = %q, want same as team/d (%q)", nodeID, groups[nodeID], groupB)
		}
	}

	// The two triangles must be in different groups.
	if groupA == groupB {
		test.Errorf("triangle A and triangle B share group %q — expected distinct communities", groupA)
	}

	// Every node must have a non-empty group.
	for _, node := range graph.Nodes {
		if node.Group == "" {
			test.Errorf("node %q has empty group", node.ID)
		}
	}
}

// TestCommunity_StickyLabels confirms that when a new edge is added that does
// not merge two communities, the pre-existing nodes keep their original Group
// strings (no wholesale relabel).
//
// Fixture: two separate triangles as above. Generation 1: snapshot with no
// cross-triangle edge. Generation 2: add one intra-triangle edge (which cannot
// merge the communities since each triangle is already fully connected). Nodes
// in each triangle must keep the same group label across the two snapshots.
func TestCommunity_StickyLabels(test *testing.T) {
	nodes := []index.NodeRow{
		fileRow("aa", "note", "AA", ""),
		fileRow("ab", "note", "AB", ""),
		fileRow("ac", "note", "AC", ""),
		fileRow("ba", "note", "BA", ""),
		fileRow("bb", "note", "BB", ""),
		fileRow("bc", "note", "BC", ""),
	}

	// Initial edges: two separate triangles, fully connected.
	initialEdges := []index.EdgeRow{
		edge("depends-on", "aa", "ab", "direct"),
		edge("depends-on", "ab", "ac", "direct"),
		edge("depends-on", "aa", "ac", "direct"),
		edge("depends-on", "ba", "bb", "direct"),
		edge("depends-on", "bb", "bc", "direct"),
		edge("depends-on", "ba", "bc", "direct"),
	}

	cfg := manifest.GraphCluster{
		By:             "community",
		CommunityEdges: []string{"depends-on"},
		Resolution:     1.0,
	}

	changes := &fakeChanges{sig: Signal{Generation: 1, Epoch: 1}}
	nodesByID := make(map[string]index.NodeRow, len(nodes))

	for _, row := range nodes {
		nodesByID[row.ID] = row
	}

	fakeEdgesRepo := &fakeEdges{all: initialEdges}

	deps := Deps{
		Nodes:    &fakeNodes{files: nodes, byID: nodesByID},
		Edges:    fakeEdgesRepo,
		Changes:  changes,
		Manifest: &manifest.Manifest{GraphCluster: cfg},
	}

	srv := New(deps)

	// --- Generation 1: first snapshot.
	graph1, snapErr1 := srv.snapshot()

	if snapErr1 != nil {
		test.Fatalf("snapshot gen1: %v", snapErr1)
	}

	groups1 := nodeGroupMap(graph1)

	// Confirm the two triangles are in different groups.
	if groups1["aa"] == groups1["ba"] {
		test.Fatalf("gen1: triangles A and B share group %q — test fixture invalid", groups1["aa"])
	}

	// --- Generation 2: add one new edge within triangle A, bump the generation.
	// Adding aa→ab again is a no-op structurally (it is already there), so we
	// add a different kind to make it distinct while keeping communities intact.
	fakeEdgesRepo.all = append(fakeEdgesRepo.all, edge("references", "aa", "ab", "direct"))

	changes.setSig(Signal{Generation: 2, Epoch: 1})

	graph2, snapErr2 := srv.snapshot()

	if snapErr2 != nil {
		test.Fatalf("snapshot gen2: %v", snapErr2)
	}

	groups2 := nodeGroupMap(graph2)

	// All pre-existing nodes must keep the same group label.
	for _, nodeID := range []string{"aa", "ab", "ac", "ba", "bb", "bc"} {
		if groups1[nodeID] != groups2[nodeID] {
			test.Errorf("node %q: group changed gen1→gen2: %q → %q", nodeID, groups1[nodeID], groups2[nodeID])
		}
	}
}

// TestCommunity_Memoization confirms that running snapshot() twice at the same
// generation calls the detector at most once (the second call hits the memo).
// The test substitutes a counting wrapper for srv.detect.
func TestCommunity_Memoization(test *testing.T) {
	nodes := []index.NodeRow{
		fileRow("memo/a", "note", "A", ""),
		fileRow("memo/b", "note", "B", ""),
		fileRow("memo/c", "note", "C", ""),
	}

	edgeRows := []index.EdgeRow{
		edge("depends-on", "memo/a", "memo/b", "direct"),
		edge("depends-on", "memo/b", "memo/c", "direct"),
	}

	cfg := manifest.GraphCluster{
		By:             "community",
		CommunityEdges: []string{"depends-on"},
		Resolution:     1.0,
	}

	changes := &fakeChanges{sig: Signal{Generation: 42, Epoch: 1}}
	srv := New(communityDeps(cfg, nodes, edgeRows, changes))

	// Replace the detect function with a counting wrapper.
	var detectCount atomic.Int64

	srv.detect = func(nodeIDs []string, edges []graphcluster.Edge, opts graphcluster.Options) map[string]int {
		detectCount.Add(1)

		return graphcluster.Detect(nodeIDs, edges, opts)
	}

	// First snapshot — must run detect.
	_, snapErr1 := srv.snapshot()

	if snapErr1 != nil {
		test.Fatalf("snapshot 1: %v", snapErr1)
	}

	if got := detectCount.Load(); got != 1 {
		test.Errorf("after first snapshot: detectCount = %d, want 1", got)
	}

	// Second snapshot at the same generation — must hit memo, not re-detect.
	_, snapErr2 := srv.snapshot()

	if snapErr2 != nil {
		test.Fatalf("snapshot 2: %v", snapErr2)
	}

	if got := detectCount.Load(); got != 1 {
		test.Errorf("after second snapshot (same generation): detectCount = %d, want still 1", got)
	}

	// Advance the generation — must run detect again.
	changes.setSig(Signal{Generation: 43, Epoch: 1})

	_, snapErr3 := srv.snapshot()

	if snapErr3 != nil {
		test.Fatalf("snapshot 3: %v", snapErr3)
	}

	if got := detectCount.Load(); got != 2 {
		test.Errorf("after third snapshot (new generation): detectCount = %d, want 2", got)
	}
}

// TestCommunity_EdgeFilter confirms that edges not matching the CommunityEdges
// filter are excluded from clustering, so nodes linked only by the excluded
// edge type land in distinct singleton communities.
func TestCommunity_EdgeFilter(test *testing.T) {
	test.Run("filter-by-Type", func(subtest *testing.T) {
		// nodes/a and nodes/b are linked by "references" (excluded).
		// nodes/c and nodes/d are linked by "depends-on" (included) — they share a community.
		// nodes/a and nodes/b must each be singletons.
		nodes := []index.NodeRow{
			fileRow("nodes/a", "note", "A", ""),
			fileRow("nodes/b", "note", "B", ""),
			fileRow("nodes/c", "note", "C", ""),
			fileRow("nodes/d", "note", "D", ""),
		}

		edgeRows := []index.EdgeRow{
			edge("references", "nodes/a", "nodes/b", "direct"),
			edge("depends-on", "nodes/c", "nodes/d", "direct"),
		}

		cfg := manifest.GraphCluster{
			By:             "community",
			CommunityEdges: []string{"depends-on"},
			Resolution:     1.0,
		}

		changes := &fakeChanges{sig: Signal{Generation: 1, Epoch: 1}}
		srv := New(communityDeps(cfg, nodes, edgeRows, changes))

		graph, snapErr := srv.snapshot()

		if snapErr != nil {
			subtest.Fatalf("snapshot: %v", snapErr)
		}

		groups := nodeGroupMap(graph)

		// nodes/c and nodes/d share a community (linked by "depends-on").
		if groups["nodes/c"] != groups["nodes/d"] {
			subtest.Errorf("nodes/c group = %q, nodes/d group = %q — want same community", groups["nodes/c"], groups["nodes/d"])
		}

		// nodes/a and nodes/b are singletons (linked only by excluded "references").
		if groups["nodes/a"] == groups["nodes/b"] {
			subtest.Errorf("nodes/a and nodes/b share group %q — want distinct singletons (references was filtered out)", groups["nodes/a"])
		}
	})

	test.Run("filter-by-Kind", func(subtest *testing.T) {
		// nodes/p and nodes/q are linked by "derived" kind (included via Kind match).
		// nodes/r and nodes/s are linked by "structural" kind (excluded).
		// nodes/p and nodes/q must share a community; nodes/r and nodes/s must be singletons.
		nodes := []index.NodeRow{
			fileRow("nodes/p", "note", "P", ""),
			fileRow("nodes/q", "note", "Q", ""),
			fileRow("nodes/r", "note", "R", ""),
			fileRow("nodes/s", "note", "S", ""),
		}

		edgeRows := []index.EdgeRow{
			edge("wikilink", "nodes/p", "nodes/q", "derived"),
			edge("contains", "nodes/r", "nodes/s", "structural"),
		}

		cfg := manifest.GraphCluster{
			By:             "community",
			CommunityEdges: []string{"derived"},
			Resolution:     1.0,
		}

		changes := &fakeChanges{sig: Signal{Generation: 1, Epoch: 1}}
		srv := New(communityDeps(cfg, nodes, edgeRows, changes))

		graph, snapErr := srv.snapshot()

		if snapErr != nil {
			subtest.Fatalf("snapshot: %v", snapErr)
		}

		groups := nodeGroupMap(graph)

		// nodes/p and nodes/q share a community (linked by "derived" kind).
		if groups["nodes/p"] != groups["nodes/q"] {
			subtest.Errorf("nodes/p group = %q, nodes/q group = %q — want same community (derived kind included)", groups["nodes/p"], groups["nodes/q"])
		}

		// nodes/r and nodes/s are singletons (structural kind excluded).
		if groups["nodes/r"] == groups["nodes/s"] {
			subtest.Errorf("nodes/r and nodes/s share group %q — want distinct singletons (structural was filtered out)", groups["nodes/r"])
		}
	})
}

// TestCommunity_ConcurrentSnapshots confirms that concurrent calls to snapshot()
// at the same generation do not produce data races (verified by -race) and all
// return a consistent, non-empty label set.
func TestCommunity_ConcurrentSnapshots(test *testing.T) {
	nodes := []index.NodeRow{
		fileRow("conc/a", "note", "A", ""),
		fileRow("conc/b", "note", "B", ""),
		fileRow("conc/c", "note", "C", ""),
		fileRow("conc/d", "note", "D", ""),
	}

	edgeRows := []index.EdgeRow{
		edge("depends-on", "conc/a", "conc/b", "direct"),
		edge("depends-on", "conc/b", "conc/c", "direct"),
		edge("depends-on", "conc/c", "conc/d", "direct"),
	}

	cfg := manifest.GraphCluster{
		By:             "community",
		CommunityEdges: []string{"depends-on"},
		Resolution:     1.0,
	}

	changes := &fakeChanges{sig: Signal{Generation: 100, Epoch: 1}}
	srv := New(communityDeps(cfg, nodes, edgeRows, changes))

	const goroutineCount = 10

	var waitGroup sync.WaitGroup
	waitGroup.Add(goroutineCount)

	errs := make([]error, goroutineCount)
	graphs := make([]Graph, goroutineCount)

	for idx := range goroutineCount {
		go func(slot int) {
			defer waitGroup.Done()

			graph, snapErr := srv.snapshot()

			errs[slot] = snapErr
			graphs[slot] = graph
		}(idx)
	}

	waitGroup.Wait()

	// All snapshots must succeed.
	for idx, err := range errs {
		if err != nil {
			test.Errorf("goroutine %d: snapshot error: %v", idx, err)
		}
	}

	// All snapshots must agree on the generation.
	for idx, graph := range graphs {
		if graph.Generation != 100 {
			test.Errorf("goroutine %d: generation = %d, want 100", idx, graph.Generation)
		}
	}

	// Every node in every snapshot must have a non-empty group.
	for idx, graph := range graphs {
		for _, node := range graph.Nodes {
			if node.Group == "" {
				test.Errorf("goroutine %d: node %q has empty group", idx, node.ID)
			}
		}
	}

	// All snapshots must produce the same label set (deterministic + memoized).
	if len(graphs) > 0 {
		ref := nodeGroupMap(graphs[0])

		for idx := 1; idx < len(graphs); idx++ {
			got := nodeGroupMap(graphs[idx])

			for nodeID, refGroup := range ref {
				if got[nodeID] != refGroup {
					test.Errorf("goroutine %d: node %q group = %q, want %q (from goroutine 0)", idx, nodeID, got[nodeID], refGroup)
				}
			}
		}
	}
}
