package node_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

func TestValidateProperties_UntypedNodePassThrough(test *testing.T) {
	parsed := &node.Node{Type: "unknown", Properties: map[string]any{"any-key": "any-value"}}

	result := node.ValidateProperties(parsed, map[string]manifest.NodeType{})

	if len(result.HardErrors) != 0 || len(result.Drift) != 0 {
		test.Errorf("untyped node should pass through, got %+v", result)
	}
}

func TestValidateProperties_RequiredPresent(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"summary": "ok"}}

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "summary", Type: "string", Required: true}}},
	}

	result := node.ValidateProperties(parsed, decls)

	if len(result.HardErrors) != 0 || len(result.Drift) != 0 {
		test.Errorf("present required should pass, got %+v", result)
	}
}

func TestValidateProperties_RequiredMissing(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{}}

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{
			{Name: "summary", Type: "string", Required: true},
			{Name: "due", Type: "date", Required: true},
		}},
	}

	result := node.ValidateProperties(parsed, decls)

	if len(result.HardErrors) != 2 {
		test.Fatalf("HardErrors count = %d, want 2; got %+v", len(result.HardErrors), result.HardErrors)
	}

	if result.HardErrors[0].Kind != node.ErrRequiredMissing || result.HardErrors[0].Property != "summary" {
		test.Errorf("HardErrors[0] = %+v", result.HardErrors[0])
	}

	if result.HardErrors[1].Property != "due" {
		test.Errorf("HardErrors[1] = %+v", result.HardErrors[1])
	}
}
