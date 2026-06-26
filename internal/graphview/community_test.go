package graphview

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/germanamz/tusk/internal/graphcluster"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// TestStableLabels_BijectiveOnCollision verifies that stableLabels always
// returns a bijection over communities even when the fallback label of one
// community collides with a label already claimed by an earlier community.
//
// Scenario:
//
//	prev    = {"a":"x", "m":"x"}  — community {a,m} has plurality winner "x"
//	next    = {"a":0, "m":0, "x":1, "y":1}  — community {x,y} has no prior votes,
//	                                            fallback is "x" (smallest member)
//	nodeIDs = []string{"a", "m", "x", "y"}
//
// Without the fix, both communities would receive label "x". With the fix,
// community {a,m} receives "x" (plurality winner) and community {x,y}
// receives a distinct label ("y", the next unclaimed member id).
func TestStableLabels_BijectiveOnCollision(test *testing.T) {
	prev := map[string]string{
		"a": "x",
		"m": "x",
	}
	next := map[string]int{
		"a": 0,
		"m": 0,
		"x": 1,
		"y": 1,
	}
	nodeIDs := []string{"a", "m", "x", "y"}

	result := stableLabels(prev, next, nodeIDs)

	// Both communities must have labels.
	labelAM := result["a"]
	if result["m"] != labelAM {
		test.Errorf("nodes a and m must share a label: got a=%q, m=%q", labelAM, result["m"])
	}

	labelXY := result["x"]
	if result["y"] != labelXY {
		test.Errorf("nodes x and y must share a label: got x=%q, y=%q", labelXY, result["y"])
	}

	// The two communities must have DISTINCT labels (bijection invariant).
	if labelAM == labelXY {
		test.Errorf("communities {a,m} and {x,y} both received label %q — stableLabels is not bijective", labelAM)
	}

	// No label must be empty.
	for nodeID, label := range result {
		if label == "" {
			test.Errorf("node %q has empty label", nodeID)
		}
	}

	// Quick bijection check: collect the label assigned to each community index.
	// Two distinct indices must not map to the same label.
	commLabels := make(map[int]string)
	for nodeID, commIdx := range next {
		label := result[nodeID]
		if prev, exists := commLabels[commIdx]; exists && prev != label {
			test.Errorf("community %d has inconsistent labels: %q and %q", commIdx, prev, label)
		}
		commLabels[commIdx] = label
	}

	seen := make(map[string]int)
	for commIdx, label := range commLabels {
		if other, dup := seen[label]; dup {
			test.Errorf("communities %d and %d both got label %q — not a bijection", other, commIdx, label)
		}
		seen[label] = commIdx
	}
}

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

// TestCommunity_StickyLabels confirms that when the graph changes in a way that
// does NOT merge two pre-existing communities (a new node joins one community
// via a depends-on edge), the pre-existing nodes keep their original Group
// strings (labels are sticky via prior-label overlap, not merely from identical
// detector input).
//
// Fixture: two separate triangles. Generation 1: snapshot — two communities
// with distinct labels. Generation 2: a new node "az" is added to the node
// list and connected into triangle A with a depends-on edge. This changes the
// partition (community A grows from 3 to 4 members) without merging A and B.
// The pre-existing 6 nodes must keep the same group label.
func TestCommunity_StickyLabels(test *testing.T) {
	// Triangle A nodes; triangle B nodes. "az" is the new node added in gen 2.
	nodesGen1 := []index.NodeRow{
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

	nodesByID := make(map[string]index.NodeRow)
	for _, row := range nodesGen1 {
		nodesByID[row.ID] = row
	}

	fakeNodesRepo := &fakeNodes{files: nodesGen1, byID: nodesByID}
	fakeEdgesRepo := &fakeEdges{all: initialEdges}

	deps := Deps{
		Nodes:    fakeNodesRepo,
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

	// --- Generation 2: add new node "az" connected into triangle A via depends-on.
	// This causes community A to grow (partition changes), but does NOT merge A and B.
	// We use depends-on so the edge is included by CommunityEdges — the detector
	// input is genuinely different from gen 1, exercising the sticky-label code path.
	newNode := fileRow("az", "note", "AZ", "")
	nodesByID["az"] = newNode
	fakeNodesRepo.files = append(fakeNodesRepo.files, newNode)
	fakeNodesRepo.byID = nodesByID

	fakeEdgesRepo.all = append(fakeEdgesRepo.all, edge("depends-on", "aa", "az", "direct"))

	changes.setSig(Signal{Generation: 2, Epoch: 1})

	graph2, snapErr2 := srv.snapshot()

	if snapErr2 != nil {
		test.Fatalf("snapshot gen2: %v", snapErr2)
	}

	groups2 := nodeGroupMap(graph2)

	// The new node "az" must be in the same group as triangle A.
	if groups2["az"] != groups2["aa"] {
		test.Errorf("gen2: new node az group = %q, want same as aa (%q)", groups2["az"], groups2["aa"])
	}

	// Triangle A and triangle B must still be in different groups.
	if groups2["aa"] == groups2["ba"] {
		test.Errorf("gen2: triangles A and B share group %q after az was added", groups2["aa"])
	}

	// All PRE-EXISTING nodes must keep the same group label across gen1→gen2.
	// This proves labels are sticky via prior-label overlap (stableLabels reuses
	// the gen-1 plurality label), not merely from an identical detector input.
	for _, nodeID := range []string{"aa", "ab", "ac", "ba", "bb", "bc"} {
		if groups1[nodeID] != groups2[nodeID] {
			test.Errorf("node %q: group changed gen1→gen2: %q → %q (labels not sticky)", nodeID, groups1[nodeID], groups2[nodeID])
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
