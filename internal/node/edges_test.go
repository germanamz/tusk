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
