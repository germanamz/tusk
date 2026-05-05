package node_test

import (
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

func testEdgeRegistry() manifest.EdgeTypes {
	return manifest.EdgeTypes{
		"parent": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket", "project"},
			Cardinality: manifest.CardinalityManyToOne,
		},
		"blocks": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket"},
			Cardinality: manifest.CardinalityManyToMany,
			Acyclic:     true,
		},
		"references": manifest.EdgeType{
			From: []string{"*"}, To: []string{"*"},
			Cardinality: manifest.CardinalityManyToMany,
		},
	}
}

func TestResolveEdges_MovesEdgeKeysFromPropertiesToEdges(test *testing.T) {
	parsed := &node.Node{
		ID:   "tickets/foo",
		Type: "ticket",
		Properties: map[string]any{
			"type":     "ticket",
			"title":    "Foo",
			"priority": 3,
			"parent":   "tickets/epic",
			"blocks":   []any{"tickets/bar", "tickets/baz"},
		},
		Body: []byte(""),
	}

	if resolveErr := node.ResolveEdges(parsed, testEdgeRegistry()); resolveErr != nil {
		test.Fatalf("ResolveEdges: %v", resolveErr)
	}

	if _, lingering := parsed.Properties["parent"]; lingering {
		test.Errorf("parent should have been moved out of Properties")
	}

	if _, lingering := parsed.Properties["blocks"]; lingering {
		test.Errorf("blocks should have been moved out of Properties")
	}

	if priorityRaw, kept := parsed.Properties["priority"]; !kept || priorityRaw != 3 {
		test.Errorf("priority should remain as a property")
	}

	if !reflect.DeepEqual(parsed.Edges["parent"], []string{"tickets/epic"}) {
		test.Errorf("Edges[parent] = %v", parsed.Edges["parent"])
	}

	if !reflect.DeepEqual(parsed.Edges["blocks"], []string{"tickets/bar", "tickets/baz"}) {
		test.Errorf("Edges[blocks] = %v", parsed.Edges["blocks"])
	}
}

func TestResolveEdges_AcceptsScalarStringForEdgeKey(test *testing.T) {
	parsed := &node.Node{
		ID:   "tickets/foo",
		Type: "ticket",
		Properties: map[string]any{
			"type":   "ticket",
			"parent": "tickets/epic",
		},
	}

	if resolveErr := node.ResolveEdges(parsed, testEdgeRegistry()); resolveErr != nil {
		test.Fatalf("ResolveEdges: %v", resolveErr)
	}

	if !reflect.DeepEqual(parsed.Edges["parent"], []string{"tickets/epic"}) {
		test.Errorf("Edges[parent] = %v", parsed.Edges["parent"])
	}
}

func TestResolveEdges_RejectsMapShapedEdgeValue(test *testing.T) {
	parsed := &node.Node{
		ID:   "tickets/foo",
		Type: "ticket",
		Properties: map[string]any{
			"type":   "ticket",
			"parent": map[string]any{"id": "x"},
		},
	}

	resolveErr := node.ResolveEdges(parsed, testEdgeRegistry())

	if resolveErr == nil {
		test.Fatalf("expected error for map-shaped edge value")
	}
}

func TestResolveEdges_LeavesNonEdgeKeysAlone(test *testing.T) {
	parsed := &node.Node{
		ID:   "n",
		Type: "ticket",
		Properties: map[string]any{
			"type":     "ticket",
			"title":    "T",
			"priority": 4,
		},
	}

	if resolveErr := node.ResolveEdges(parsed, testEdgeRegistry()); resolveErr != nil {
		test.Fatalf("ResolveEdges: %v", resolveErr)
	}

	if len(parsed.Edges) != 0 {
		test.Errorf("Edges = %v, want empty", parsed.Edges)
	}
}

func TestValidateEdges_RejectsUnknownEdgeType(test *testing.T) {
	parsed := &node.Node{
		ID:    "x",
		Type:  "ticket",
		Edges: map[string][]string{"unknown": {"y"}},
	}

	validateErr := node.ValidateEdges(parsed, testEdgeRegistry(), node.EdgeContext{
		ResolveTargetType: func(targetID string) (string, bool) { return "ticket", true },
	})

	if validateErr == nil {
		test.Fatalf("expected error for unknown edge type")
	}
}

func TestValidateEdges_RejectsSourceTypeMismatch(test *testing.T) {
	parsed := &node.Node{
		ID:    "x",
		Type:  "note",
		Edges: map[string][]string{"parent": {"y"}}, // parent only allows ticket source
	}

	validateErr := node.ValidateEdges(parsed, testEdgeRegistry(), node.EdgeContext{
		ResolveTargetType: func(targetID string) (string, bool) { return "ticket", true },
	})

	if validateErr == nil {
		test.Fatalf("expected error for source type mismatch")
	}
}

func TestValidateEdges_RejectsTargetTypeMismatch(test *testing.T) {
	parsed := &node.Node{
		ID:    "x",
		Type:  "ticket",
		Edges: map[string][]string{"parent": {"unknown-target"}},
	}

	validateErr := node.ValidateEdges(parsed, testEdgeRegistry(), node.EdgeContext{
		ResolveTargetType: func(targetID string) (string, bool) { return "note", true }, // not in parent.To
	})

	if validateErr == nil {
		test.Fatalf("expected error for target type mismatch")
	}
}

func TestValidateEdges_AllowsUnresolvedTargetWhenContextSaysFalse(test *testing.T) {
	parsed := &node.Node{
		ID:    "x",
		Type:  "ticket",
		Edges: map[string][]string{"references": {"missing/target"}},
	}

	validateErr := node.ValidateEdges(parsed, testEdgeRegistry(), node.EdgeContext{
		ResolveTargetType: func(targetID string) (string, bool) { return "", false },
	})

	if validateErr != nil {
		test.Errorf("expected nil error for unresolved target on `references`, got %v", validateErr)
	}
}

func TestDetectCycle_ReturnsNilOnAcyclicGraph(test *testing.T) {
	existing := map[string][]string{
		"a": {"b"},
		"b": {"c"},
	}

	candidate := node.CycleProbe{
		EdgeType: "blocks",
		Source:   "c",
		Target:   "d",
	}

	cycleErr := node.DetectCycle(candidate, existing)

	if cycleErr != nil {
		test.Errorf("got %v, want nil (no cycle)", cycleErr)
	}
}

func TestDetectCycle_ReturnsErrorWhenAddingEdgeCreatesCycle(test *testing.T) {
	// existing: a -blocks-> b -blocks-> c
	// adding:   c -blocks-> a  (would create a→b→c→a cycle)
	existing := map[string][]string{
		"a": {"b"},
		"b": {"c"},
	}

	candidate := node.CycleProbe{
		EdgeType: "blocks",
		Source:   "c",
		Target:   "a",
	}

	cycleErr := node.DetectCycle(candidate, existing)

	if cycleErr == nil {
		test.Fatalf("expected cycle error")
	}
}

func TestDetectCycle_AllowsSelfLoopOnNonAcyclic(test *testing.T) {
	existing := map[string][]string{}
	candidate := node.CycleProbe{
		EdgeType: "blocks",
		Source:   "x",
		Target:   "x",
	}

	cycleErr := node.DetectCycle(candidate, existing)

	if cycleErr == nil {
		test.Fatalf("expected cycle error on self-loop")
	}
}
