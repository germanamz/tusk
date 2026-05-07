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

func TestValidateProperties_StringAccepts(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"name": "hi"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "name", Type: "string"}}},
	}

	result := node.ValidateProperties(parsed, decls)

	if len(result.HardErrors) != 0 || len(result.Drift) != 0 {
		test.Errorf("expected pass; got %+v", result)
	}
}

func TestValidateProperties_StringRejectsNonString(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"name": 42}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "name", Type: "string"}}},
	}

	result := node.ValidateProperties(parsed, decls)

	if len(result.HardErrors) != 1 || result.HardErrors[0].Kind != node.ErrTypeMismatch {
		test.Errorf("expected ErrTypeMismatch; got %+v", result)
	}
}

func TestValidateProperties_IntAccepts(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"n": 7}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "n", Type: "int"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 0 {
		test.Errorf("expected pass; got %+v", got)
	}
}

func TestValidateProperties_IntRejectsFloat(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"n": 1.5}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "n", Type: "int"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 1 || got.HardErrors[0].Kind != node.ErrTypeMismatch {
		test.Errorf("expected ErrTypeMismatch; got %+v", got)
	}
}

func TestValidateProperties_FloatAcceptsInt(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"f": 7}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "f", Type: "float"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 0 {
		test.Errorf("expected pass (int promotes to float); got %+v", got)
	}
}

func TestValidateProperties_FloatRejectsString(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"f": "1.5"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "f", Type: "float"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 1 {
		test.Errorf("expected error; got %+v", got)
	}
}

func TestValidateProperties_BoolAcceptsBool(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"b": true}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "b", Type: "bool"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 0 {
		test.Errorf("expected pass; got %+v", got)
	}
}

func TestValidateProperties_BoolRejectsStringTrue(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"b": "true"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "b", Type: "bool"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 1 {
		test.Errorf("expected error; got %+v", got)
	}
}

func TestValidateProperties_DateAcceptsISO(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"d": "2026-05-15"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "d", Type: "date"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 0 {
		test.Errorf("expected pass; got %+v", got)
	}
}

func TestValidateProperties_DateRejectsBadString(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"d": "tomorrow"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "d", Type: "date"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 1 {
		test.Errorf("expected error; got %+v", got)
	}
}

func TestValidateProperties_DatetimeAcceptsRFC3339(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"dt": "2026-05-15T10:00:00Z"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "dt", Type: "datetime"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 0 {
		test.Errorf("expected pass; got %+v", got)
	}
}

func TestValidateProperties_EnumInValuesAccepts(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"stage": "active"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{
			Name: "stage", Type: "enum", Values: []string{"pending", "active", "completed"},
		}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 0 {
		test.Errorf("expected pass; got %+v", got)
	}
}

func TestValidateProperties_EnumOutOfValuesRejects(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"stage": "shipping"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{
			Name: "stage", Type: "enum", Values: []string{"pending", "active", "completed"},
		}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 1 || got.HardErrors[0].Kind != node.ErrEnumViolation {
		test.Errorf("expected ErrEnumViolation; got %+v", got)
	}
}

func TestValidateProperties_MarkdownAcceptsString(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"m": "# heading"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "m", Type: "markdown"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 0 {
		test.Errorf("expected pass; got %+v", got)
	}
}

func TestValidateProperties_UndeclaredPropertyAppearsInDrift(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"assignee": "bob"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "summary", Type: "string"}}},
	}

	got := node.ValidateProperties(parsed, decls)

	if len(got.HardErrors) != 0 {
		test.Errorf("undeclared should not be HardError; got %+v", got.HardErrors)
	}

	if len(got.Drift) != 1 || got.Drift[0].Property != "assignee" {
		test.Errorf("expected one Drift entry for assignee; got %+v", got.Drift)
	}
}
