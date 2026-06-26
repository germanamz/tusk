package graphview

import (
	"testing"
)

// twobranchEdges builds the two-branch synthetic org tree used across
// ancestor helper tests. The edges use the "parent" type and model a
// child→parent direction (parentIsSource = false):
//
//	orgA  <- teamA1 <- personA1
//	                <- personA2
//	orgB  <- teamB1 <- personB1
//
// Each "<-" is a "parent" edge: Source = child, Target = parent.
func twobranchEdges() []GraphEdge {
	return []GraphEdge{
		{Source: "teamA1", Target: "orgA", Type: "parent", Kind: "direct"},
		{Source: "personA1", Target: "teamA1", Type: "parent", Kind: "direct"},
		{Source: "personA2", Target: "teamA1", Type: "parent", Kind: "direct"},
		{Source: "teamB1", Target: "orgB", Type: "parent", Kind: "direct"},
		{Source: "personB1", Target: "teamB1", Type: "parent", Kind: "direct"},
	}
}

// TestAncestorGroups_Depth0_WalksToRoot confirms that depth 0 walks every
// node to its branch root.
func TestAncestorGroups_Depth0_WalksToRoot(test *testing.T) {
	groups := ancestorGroups(twobranchEdges(), "parent", 0, false)

	cases := map[string]string{
		"personA1": "orgA",
		"personA2": "orgA",
		"teamA1":   "orgA",
		"orgA":     "orgA",
		"personB1": "orgB",
		"teamB1":   "orgB",
		"orgB":     "orgB",
	}

	for nodeID, want := range cases {
		got, ok := groups[nodeID]

		if !ok {
			test.Errorf("node %q missing from result", nodeID)

			continue
		}

		if got != want {
			test.Errorf("node %q: group = %q, want %q", nodeID, got, want)
		}
	}
}

// TestAncestorGroups_Depth1_ImmediateParent confirms that depth 1 maps each
// node to its immediate parent, and root nodes map to themselves.
func TestAncestorGroups_Depth1_ImmediateParent(test *testing.T) {
	groups := ancestorGroups(twobranchEdges(), "parent", 1, false)

	cases := map[string]string{
		"personA1": "teamA1",
		"personA2": "teamA1",
		"teamA1":   "orgA",
		"orgA":     "orgA", // root: no parent, stays self
		"personB1": "teamB1",
		"teamB1":   "orgB",
		"orgB":     "orgB", // root: no parent, stays self
	}

	for nodeID, want := range cases {
		got, ok := groups[nodeID]

		if !ok {
			test.Errorf("node %q missing from result", nodeID)

			continue
		}

		if got != want {
			test.Errorf("node %q: group = %q, want %q", nodeID, got, want)
		}
	}
}

// TestAncestorGroups_DepthDeep_ClipsAtRoot confirms that a depth larger than
// the chain length still returns the topmost reachable ancestor (root).
func TestAncestorGroups_DepthDeep_ClipsAtRoot(test *testing.T) {
	// depth = 5 exceeds the 2-hop chain personA1→teamA1→orgA
	groups := ancestorGroups(twobranchEdges(), "parent", 5, false)

	got, ok := groups["personA1"]

	if !ok {
		test.Fatal("personA1 missing from result")
	}

	if got != "orgA" {
		test.Errorf("personA1 at depth 5: group = %q, want %q", got, "orgA")
	}
}

// TestAncestorGroups_RootsMapToSelf confirms root nodes (no outgoing parent
// edge) are their own group.
func TestAncestorGroups_RootsMapToSelf(test *testing.T) {
	groups := ancestorGroups(twobranchEdges(), "parent", 0, false)

	for _, root := range []string{"orgA", "orgB"} {
		got, ok := groups[root]

		if !ok {
			test.Errorf("root %q missing from result", root)

			continue
		}

		if got != root {
			test.Errorf("root %q: group = %q, want self", root, got)
		}
	}
}

// TestAncestorGroups_ParentIsSource confirms the walk works correctly when
// the edge direction is inverted (parent→child edges, parentIsSource = true).
func TestAncestorGroups_ParentIsSource(test *testing.T) {
	// Same tree, but edges flow parent→child (Source = parent, Target = child).
	invertedEdges := []GraphEdge{
		{Source: "orgA", Target: "teamA1", Type: "parent", Kind: "direct"},
		{Source: "teamA1", Target: "personA1", Type: "parent", Kind: "direct"},
		{Source: "teamA1", Target: "personA2", Type: "parent", Kind: "direct"},
		{Source: "orgB", Target: "teamB1", Type: "parent", Kind: "direct"},
		{Source: "teamB1", Target: "personB1", Type: "parent", Kind: "direct"},
	}

	groups := ancestorGroups(invertedEdges, "parent", 0, true)

	cases := map[string]string{
		"personA1": "orgA",
		"personA2": "orgA",
		"teamA1":   "orgA",
		"orgA":     "orgA",
		"personB1": "orgB",
		"teamB1":   "orgB",
		"orgB":     "orgB",
	}

	for nodeID, want := range cases {
		got, ok := groups[nodeID]

		if !ok {
			test.Errorf("node %q missing from result", nodeID)

			continue
		}

		if got != want {
			test.Errorf("node %q: group = %q, want %q", nodeID, got, want)
		}
	}
}

// TestAncestorGroups_CycleSafety confirms the walk terminates and returns a
// finite result when the input edges form a cycle (x→y→z→x).
func TestAncestorGroups_CycleSafety(test *testing.T) {
	cycleEdges := []GraphEdge{
		{Source: "xx", Target: "yy", Type: "parent", Kind: "direct"},
		{Source: "yy", Target: "zz", Type: "parent", Kind: "direct"},
		{Source: "zz", Target: "xx", Type: "parent", Kind: "direct"},
	}

	// This must not hang or panic.
	groups := ancestorGroups(cycleEdges, "parent", 0, false)

	// All three nodes must be in the result, each with a finite, non-empty group.
	for _, nodeID := range []string{"xx", "yy", "zz"} {
		grp, ok := groups[nodeID]

		if !ok {
			test.Errorf("node %q missing from cycle result", nodeID)

			continue
		}

		if grp == "" {
			test.Errorf("node %q: got empty group", nodeID)
		}
	}
}

// TestAncestorGroups_UnrelatedEdgeTypesIgnored confirms that edges of a
// different type do not contribute to the ancestor walk.
func TestAncestorGroups_UnrelatedEdgeTypesIgnored(test *testing.T) {
	mixedEdges := []GraphEdge{
		// The hierarchy edge we care about.
		{Source: "child", Target: "root", Type: "parent", Kind: "direct"},
		// An unrelated edge that would create a false parent if included.
		{Source: "child", Target: "wrong-root", Type: "references", Kind: "direct"},
	}

	groups := ancestorGroups(mixedEdges, "parent", 0, false)

	got, ok := groups["child"]

	if !ok {
		test.Fatal("child missing from result")
	}

	if got != "root" {
		test.Errorf("child: group = %q, want %q (unrelated edge type must be ignored)", got, "root")
	}

	// "wrong-root" must not appear as a participant at all from the "parent" walk.
	if _, ok := groups["wrong-root"]; ok {
		test.Errorf("wrong-root should not appear in the parent-walk result (came from a references edge)")
	}
}
