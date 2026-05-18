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

	// Reflects the shape synthesizeHierarchyBackCompat produces for a bare
	// "parent" edge: Hierarchy = "parent", HierarchyDefault = true.
	manifestObj := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"parent": {
				From:             []string{"*"},
				To:               []string{"*"},
				Cardinality:      manifest.CardinalityManyToOne,
				Hierarchy:        "parent",
				HierarchyDefault: true,
			},
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

func TestValidate_QualifiedShortcutResolvesAlias(test *testing.T) {
	parser := filter.NewParser("tree:wbs=wbs/root")
	expr, _ := parser.Parse()

	manifestObj := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"wbs-parent": {
				Cardinality: manifest.CardinalityManyToOne,
				Hierarchy:   "wbs",
			},
		},
	}

	errs := filter.Validate(expr, manifestObj)

	if len(errs) != 0 {
		test.Fatalf("unexpected validation errors: %+v", errs)
	}

	shortcut := expr.(*filter.TraversalShortcut)

	if shortcut.EdgeType != "wbs-parent" {
		test.Errorf("EdgeType = %q, want %q", shortcut.EdgeType, "wbs-parent")
	}
}

func TestValidate_QualifiedShortcutUnknownAlias(test *testing.T) {
	parser := filter.NewParser("tree:nope=x")
	expr, _ := parser.Parse()

	manifestObj := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"wbs-parent": {Hierarchy: "wbs"},
		},
	}

	errs := filter.Validate(expr, manifestObj)

	if len(errs) == 0 {
		test.Fatalf("expected validation error")
	}

	if !strings.Contains(errs[0].Message, "unknown hierarchy alias") {
		test.Errorf("Message = %q, want substring %q", errs[0].Message, "unknown hierarchy alias")
	}

	if !strings.Contains(errs[0].Hint, "wbs") {
		test.Errorf("Hint = %q, want it to list declared aliases", errs[0].Hint)
	}
}

func TestValidate_UnqualifiedResolvesToDefault(test *testing.T) {
	parser := filter.NewParser("tree=x")
	expr, _ := parser.Parse()

	manifestObj := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"wbs-parent":    {Hierarchy: "wbs", HierarchyDefault: true},
			"kanban-parent": {Hierarchy: "kanban"},
		},
	}

	errs := filter.Validate(expr, manifestObj)

	if len(errs) != 0 {
		test.Fatalf("unexpected validation errors: %+v", errs)
	}

	if expr.(*filter.TraversalShortcut).EdgeType != "wbs-parent" {
		test.Errorf("expected default to resolve to wbs-parent")
	}
}

func TestValidate_UnqualifiedResolvesToSoleHierarchy(test *testing.T) {
	parser := filter.NewParser("tree=x")
	expr, _ := parser.Parse()

	manifestObj := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"wbs-parent": {Hierarchy: "wbs"},
		},
	}

	errs := filter.Validate(expr, manifestObj)

	if len(errs) != 0 {
		test.Fatalf("unexpected validation errors: %+v", errs)
	}

	if expr.(*filter.TraversalShortcut).EdgeType != "wbs-parent" {
		test.Errorf("expected sole hierarchy to be picked")
	}
}

func TestValidate_UnqualifiedBareParentBackCompat(test *testing.T) {
	parser := filter.NewParser("tree=x")
	expr, _ := parser.Parse()

	// Simulates a manifest after synthesizeHierarchyBackCompat ran on a
	// v1.2-style workspace whose edge is literally named "parent".
	manifestObj := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"parent": {Hierarchy: "parent", HierarchyDefault: true},
		},
	}

	errs := filter.Validate(expr, manifestObj)

	if len(errs) != 0 {
		test.Fatalf("unexpected validation errors: %+v", errs)
	}

	if expr.(*filter.TraversalShortcut).EdgeType != "parent" {
		test.Errorf("EdgeType = %q, want %q", expr.(*filter.TraversalShortcut).EdgeType, "parent")
	}
}

func TestValidate_UnqualifiedMultipleHierarchiesNoDefault(test *testing.T) {
	parser := filter.NewParser("tree=x")
	expr, _ := parser.Parse()

	manifestObj := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"wbs-parent":    {Hierarchy: "wbs"},
			"kanban-parent": {Hierarchy: "kanban"},
		},
	}

	errs := filter.Validate(expr, manifestObj)

	if len(errs) == 0 {
		test.Fatalf("expected error for ambiguous default")
	}

	if !strings.Contains(errs[0].Message, "no default hierarchy") {
		test.Errorf("Message = %q, want substring %q", errs[0].Message, "no default hierarchy")
	}
}

func TestValidate_UnqualifiedNoHierarchiesAtAll(test *testing.T) {
	parser := filter.NewParser("tree=x")
	expr, _ := parser.Parse()

	manifestObj := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"references": {},
		},
	}

	errs := filter.Validate(expr, manifestObj)

	if len(errs) == 0 {
		test.Fatalf("expected error for no hierarchy")
	}

	if !strings.Contains(errs[0].Message, "no hierarchy edges declared") {
		test.Errorf("Message = %q, want substring %q", errs[0].Message, "no hierarchy edges declared")
	}
}
