package filter_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/manifest"
)

func TestValidate_AcceptsKnownEdgeType(test *testing.T) {
	expr, _ := filter.NewParser("blocks->").Parse()

	manifestObj := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"blocks": {From: []string{"*"}, To: []string{"*"}, Cardinality: manifest.CardinalityManyToMany},
		},
	}

	errs := filter.Validate(expr, manifestObj)

	if len(errs) > 0 {
		test.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_RejectsUnknownEdgeType(test *testing.T) {
	expr, _ := filter.NewParser("unknown->").Parse()

	errs := filter.Validate(expr, manifest.Manifest{EdgeTypes: map[string]manifest.EdgeType{}})

	if len(errs) == 0 {
		test.Fatalf("expected error for unknown edge type")
	}

	if !strings.Contains(errs[0].Message, "unknown") && !strings.Contains(errs[0].Message, "not declared") {
		test.Errorf("error message should mention unknown/not declared: %v", errs[0])
	}
}

func TestValidate_TraversalShortcutRequiresParentEdge(test *testing.T) {
	expr, _ := filter.NewParser("tree=tickets/foo").Parse()

	errs := filter.Validate(expr, manifest.Manifest{EdgeTypes: map[string]manifest.EdgeType{}})

	if len(errs) == 0 {
		test.Fatalf("expected error: traversal shortcut requires `parent` edge type")
	}
}

func TestValidate_TraversalShortcutOKWithParentEdge(test *testing.T) {
	expr, _ := filter.NewParser("tree=tickets/foo").Parse()

	manifestObj := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"parent": {From: []string{"*"}, To: []string{"*"}, Cardinality: manifest.CardinalityManyToOne},
		},
	}

	errs := filter.Validate(expr, manifestObj)

	if len(errs) > 0 {
		test.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_NestedEdgeChainAllValidate(test *testing.T) {
	expr, _ := filter.NewParser("parent->parent->name=auth").Parse()

	manifestObj := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"parent": {From: []string{"*"}, To: []string{"*"}, Cardinality: manifest.CardinalityManyToOne},
		},
	}

	errs := filter.Validate(expr, manifestObj)

	if len(errs) > 0 {
		test.Errorf("expected no errors, got %v", errs)
	}
}
