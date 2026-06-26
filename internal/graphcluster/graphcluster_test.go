package graphcluster

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
)

// distinctCommunities returns the number of distinct community indices in a
// partition. It is the headline signal for several tests.
func distinctCommunities(partition map[string]int) int {
	seen := make(map[int]struct{}, len(partition))

	for _, community := range partition {
		seen[community] = struct{}{}
	}

	return len(seen)
}

// cliqueEdges returns every undirected pair within ids as a unit-weight edge.
func cliqueEdges(ids []string, weight float64) []Edge {
	edges := make([]Edge, 0)

	for left := 0; left < len(ids); left++ {
		for right := left + 1; right < len(ids); right++ {
			edges = append(edges, Edge{Source: ids[left], Target: ids[right], Weight: weight})
		}
	}

	return edges
}

// Task 4(a): two cliques joined by a single low-weight bridge resolve to
// exactly two communities, and every clique member shares its clique's index.
func TestDetect_TwoCliquesOneBridge(test *testing.T) {
	left := []string{"a1", "a2", "a3", "a4"}
	right := []string{"b1", "b2", "b3", "b4"}

	nodeIDs := append(append([]string{}, left...), right...)

	edges := append(cliqueEdges(left, 1.0), cliqueEdges(right, 1.0)...)
	edges = append(edges, Edge{Source: "a1", Target: "b1", Weight: 0.01})

	partition := Detect(nodeIDs, edges, Options{Resolution: 1.0})

	if got := distinctCommunities(partition); got != 2 {
		test.Fatalf("distinct communities = %d, want 2; partition = %v", got, partition)
	}

	for _, member := range left[1:] {
		if partition[member] != partition[left[0]] {
			test.Errorf("left clique split: %s=%d, %s=%d", left[0], partition[left[0]], member, partition[member])
		}
	}

	for _, member := range right[1:] {
		if partition[member] != partition[right[0]] {
			test.Errorf("right clique split: %s=%d, %s=%d", right[0], partition[right[0]], member, partition[member])
		}
	}

	if partition[left[0]] == partition[right[0]] {
		test.Errorf("cliques merged into one community: %v", partition)
	}

	// Canonical: the lexicographically-first id "a1" anchors index 0.
	if partition["a1"] != 0 {
		test.Errorf("canonical index of a1 = %d, want 0", partition["a1"])
	}
}

// Task 4(b): repeated calls are deeply equal, and shuffling the edge slice
// order does not change the partition.
func TestDetect_DeterministicAcrossCallsAndEdgeOrder(test *testing.T) {
	left := []string{"n1", "n2", "n3", "n4", "n5"}
	right := []string{"m1", "m2", "m3", "m4", "m5"}

	nodeIDs := append(append([]string{}, left...), right...)

	edges := append(cliqueEdges(left, 1.0), cliqueEdges(right, 1.0)...)
	edges = append(edges, Edge{Source: "n1", Target: "m1", Weight: 0.05})

	first := Detect(nodeIDs, edges, Options{Resolution: 1.0})
	second := Detect(nodeIDs, edges, Options{Resolution: 1.0})

	if !reflect.DeepEqual(first, second) {
		test.Fatalf("repeated Detect differ:\n first = %v\n second = %v", first, second)
	}

	// Reverse the edge order; an undirected, canonicalized partition must be
	// invariant to edge input order.
	shuffled := make([]Edge, len(edges))

	for index := range edges {
		shuffled[index] = edges[len(edges)-1-index]
	}

	third := Detect(nodeIDs, shuffled, Options{Resolution: 1.0})

	if !reflect.DeepEqual(first, third) {
		test.Fatalf("edge order changed partition:\n original = %v\n shuffled = %v", first, third)
	}

	// Reversing the nodeIDs order must also leave the partition unchanged,
	// because the engine sorts ids and canonicalizes by sorted order.
	reversedIDs := make([]string, len(nodeIDs))

	for index := range nodeIDs {
		reversedIDs[index] = nodeIDs[len(nodeIDs)-1-index]
	}

	fourth := Detect(reversedIDs, edges, Options{Resolution: 1.0})

	if !reflect.DeepEqual(first, fourth) {
		test.Fatalf("node order changed partition:\n original = %v\n reversed = %v", first, fourth)
	}
}

// Task 4(b) extended: Seed must not change the result (the engine uses no RNG).
func TestDetect_SeedInvariant(test *testing.T) {
	left := []string{"a1", "a2", "a3", "a4"}
	right := []string{"b1", "b2", "b3", "b4"}

	nodeIDs := append(append([]string{}, left...), right...)

	edges := append(cliqueEdges(left, 1.0), cliqueEdges(right, 1.0)...)
	edges = append(edges, Edge{Source: "a1", Target: "b1", Weight: 0.02})

	base := Detect(nodeIDs, edges, Options{Resolution: 1.0, Seed: 0})

	for _, seed := range []int64{1, 42, -7, 1 << 40} {
		got := Detect(nodeIDs, edges, Options{Resolution: 1.0, Seed: seed})

		if !reflect.DeepEqual(base, got) {
			test.Errorf("Seed %d changed partition:\n base = %v\n got = %v", seed, base, got)
		}
	}
}

// Task 4(c): resolution monotonicity sanity. The test graph is a chain of five
// 4-node cliques linked end-to-end by single unit-weight bridges:
//
//	[c0]-[c1]-[c2]-[c3]-[c4]
//
// At low resolution the modularity optimum merges adjacent cliques into fewer,
// larger communities; at high resolution it splits them apart. So a higher
// Resolution must yield at least as many distinct communities as a lower one.
func TestDetect_ResolutionMonotonicity(test *testing.T) {
	const cliqueCount = 5
	const cliqueSize = 4

	var (
		nodeIDs []string
		edges   []Edge
		first   []string
	)

	for clique := 0; clique < cliqueCount; clique++ {
		members := make([]string, cliqueSize)

		for member := 0; member < cliqueSize; member++ {
			members[member] = fmt.Sprintf("c%d_n%d", clique, member)
		}

		nodeIDs = append(nodeIDs, members...)
		edges = append(edges, cliqueEdges(members, 1.0)...)

		if clique > 0 {
			edges = append(edges, Edge{Source: first[len(first)-1], Target: members[0], Weight: 1.0})
		}

		first = members
	}

	low := distinctCommunities(Detect(nodeIDs, edges, Options{Resolution: 0.5}))
	high := distinctCommunities(Detect(nodeIDs, edges, Options{Resolution: 2.0}))

	if high < low {
		test.Errorf("resolution not monotone: communities at 0.5 = %d, at 2.0 = %d (want high >= low)", low, high)
	}
}

// Task 4(d): empty input yields a non-nil empty map.
func TestDetect_EmptyInput(test *testing.T) {
	for _, nodeIDs := range [][]string{nil, {}} {
		partition := Detect(nodeIDs, nil, Options{})

		if partition == nil {
			test.Fatalf("Detect(%v, nil) returned nil map, want non-nil empty", nodeIDs)
		}

		if len(partition) != 0 {
			test.Errorf("Detect(%v, nil) = %v, want empty", nodeIDs, partition)
		}
	}
}

// Task 4(d): a single node with no edges is community 0.
func TestDetect_SingleNode(test *testing.T) {
	partition := Detect([]string{"solo"}, nil, Options{})

	want := map[string]int{"solo": 0}

	if !reflect.DeepEqual(partition, want) {
		test.Errorf("Detect single node = %v, want %v", partition, want)
	}
}

// Task 4(d): several fully-isolated nodes each get a distinct index, assigned
// by sorted order.
func TestDetect_IsolatedNodes(test *testing.T) {
	nodeIDs := []string{"z", "m", "a", "q"}

	partition := Detect(nodeIDs, nil, Options{})

	if distinctCommunities(partition) != len(nodeIDs) {
		test.Fatalf("isolated nodes not distinct: %v", partition)
	}

	// Canonical assignment follows sorted id order: a=0, m=1, q=2, z=3.
	want := map[string]int{"a": 0, "m": 1, "q": 2, "z": 3}

	if !reflect.DeepEqual(partition, want) {
		test.Errorf("isolated partition = %v, want %v", partition, want)
	}
}

// Task 4(d): a node that appears only in nodeIDs but in no edge still gets its
// own singleton even when other nodes are clustered.
func TestDetect_IsolatedNodeAlongsideClique(test *testing.T) {
	clique := []string{"a", "b", "c", "d"}
	nodeIDs := append(append([]string{}, clique...), "island")

	partition := Detect(nodeIDs, cliqueEdges(clique, 1.0), Options{Resolution: 1.0})

	for _, member := range clique[1:] {
		if partition[member] != partition["a"] {
			test.Errorf("clique split: a=%d, %s=%d", partition["a"], member, partition[member])
		}
	}

	if partition["island"] == partition["a"] {
		test.Errorf("isolated node joined clique: %v", partition)
	}

	if distinctCommunities(partition) != 2 {
		test.Errorf("want 2 communities (clique + island), got %v", partition)
	}
}

// Task 4(d): an edge naming an id absent from nodeIDs is dropped, and that id
// never appears in the result.
func TestDetect_EdgeToAbsentIDDropped(test *testing.T) {
	nodeIDs := []string{"a", "b"}

	edges := []Edge{
		{Source: "a", Target: "b", Weight: 1.0},
		{Source: "a", Target: "ghost", Weight: 5.0},
		{Source: "ghost", Target: "phantom", Weight: 5.0},
	}

	partition := Detect(nodeIDs, edges, Options{Resolution: 1.0})

	if _, present := partition["ghost"]; present {
		test.Errorf("absent id ghost present in result: %v", partition)
	}

	if _, present := partition["phantom"]; present {
		test.Errorf("absent id phantom present in result: %v", partition)
	}

	if len(partition) != 2 {
		test.Errorf("result has %d keys, want exactly {a, b}: %v", len(partition), partition)
	}
}

// Task 4(d): duplicate ids in nodeIDs coalesce to a single key, no panic.
func TestDetect_DuplicateNodeIDsCoalesce(test *testing.T) {
	nodeIDs := []string{"a", "b", "a", "b", "a"}

	edges := []Edge{{Source: "a", Target: "b", Weight: 1.0}}

	partition := Detect(nodeIDs, edges, Options{Resolution: 1.0})

	if len(partition) != 2 {
		test.Fatalf("duplicates not coalesced: %v", partition)
	}

	keys := make([]string, 0, len(partition))

	for key := range partition {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	if !reflect.DeepEqual(keys, []string{"a", "b"}) {
		test.Errorf("keys = %v, want [a b]", keys)
	}
}

// The returned keys are exactly the distinct nodeIDs; every distinct id is
// present, even isolated ones, in a mixed graph.
func TestDetect_KeysAreExactlyDistinctNodeIDs(test *testing.T) {
	nodeIDs := []string{"a", "b", "c", "lonely", "a"}

	edges := []Edge{
		{Source: "a", Target: "b", Weight: 1.0},
		{Source: "b", Target: "c", Weight: 1.0},
	}

	partition := Detect(nodeIDs, edges, Options{Resolution: 1.0})

	gotKeys := make([]string, 0, len(partition))

	for key := range partition {
		gotKeys = append(gotKeys, key)
	}

	sort.Strings(gotKeys)

	wantKeys := []string{"a", "b", "c", "lonely"}

	if !reflect.DeepEqual(gotKeys, wantKeys) {
		test.Errorf("keys = %v, want %v", gotKeys, wantKeys)
	}
}

// Parallel and reverse edges fold by summing weight: two weak parallel edges
// plus a reverse edge should bind a pair as strongly as one heavy edge. This
// guards the buildGraph folding path.
func TestDetect_ParallelAndReverseEdgesFold(test *testing.T) {
	// Two tight pairs, each bound by folded parallel/reverse edges, joined by a
	// single weak bridge. Expect two communities.
	nodeIDs := []string{"a", "b", "c", "d"}

	edges := []Edge{
		{Source: "a", Target: "b", Weight: 1.0},
		{Source: "b", Target: "a", Weight: 1.0}, // reverse, folds onto a-b
		{Source: "a", Target: "b", Weight: 1.0}, // parallel, folds onto a-b
		{Source: "c", Target: "d", Weight: 1.0},
		{Source: "d", Target: "c", Weight: 1.0},
		{Source: "c", Target: "d", Weight: 1.0},
		{Source: "b", Target: "c", Weight: 0.01}, // weak bridge
	}

	partition := Detect(nodeIDs, edges, Options{Resolution: 1.0})

	if partition["a"] != partition["b"] {
		test.Errorf("a,b not grouped despite folded weight: %v", partition)
	}

	if partition["c"] != partition["d"] {
		test.Errorf("c,d not grouped despite folded weight: %v", partition)
	}

	if partition["a"] == partition["c"] {
		test.Errorf("weak bridge merged the pairs: %v", partition)
	}
}

// Self-loops must not panic and must not pull a node into a neighbor; an
// isolated node carrying only a self-loop stays its own community.
func TestDetect_SelfLoopIsolated(test *testing.T) {
	nodeIDs := []string{"a", "b", "c"}

	edges := []Edge{
		{Source: "a", Target: "a", Weight: 3.0}, // self-loop only
		{Source: "b", Target: "c", Weight: 1.0},
	}

	partition := Detect(nodeIDs, edges, Options{Resolution: 1.0})

	if partition["b"] != partition["c"] {
		test.Errorf("b,c not grouped: %v", partition)
	}

	if partition["a"] == partition["b"] {
		test.Errorf("self-looped a merged with b,c: %v", partition)
	}

	if distinctCommunities(partition) != 2 {
		test.Errorf("want 2 communities, got %v", partition)
	}
}
