package node_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

func TestFilterReservedDrift_RemovesReserved(test *testing.T) {
	drift := []node.PropertyDrift{
		{Property: "status", Reason: "not declared on type \"ticket\""},
		{Property: "assignee", Reason: "not declared on type \"ticket\""},
	}

	reserved := map[string]map[string]struct{}{
		"ticket": {"status": {}},
	}

	got := node.FilterReservedDrift(drift, "ticket", reserved)

	if len(got) != 1 || got[0].Property != "assignee" {
		test.Errorf("got %+v, want only assignee", got)
	}
}

func TestFilterReservedDrift_NilReservedReturnsInput(test *testing.T) {
	drift := []node.PropertyDrift{{Property: "status"}}

	got := node.FilterReservedDrift(drift, "ticket", nil)

	if len(got) != 1 || got[0].Property != "status" {
		test.Errorf("got %+v, want input unchanged", got)
	}
}

func TestFilterReservedDrift_OtherTypeIgnored(test *testing.T) {
	drift := []node.PropertyDrift{{Property: "status"}}

	reserved := map[string]map[string]struct{}{
		"meeting": {"status": {}},
	}

	got := node.FilterReservedDrift(drift, "ticket", reserved)

	if len(got) != 1 {
		test.Errorf("got %+v, want drift preserved for type without reservations", got)
	}
}

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

func TestValidateProperties_ListOfStringAccepts(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"labels": []any{"auth", "security"}}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "labels", Type: "list-of", ItemType: "string"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 0 {
		test.Errorf("expected pass; got %+v", got)
	}
}

func TestValidateProperties_ListOfStringRejectsMixedElement(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"labels": []any{"auth", 42}}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "labels", Type: "list-of", ItemType: "string"}}},
	}

	got := node.ValidateProperties(parsed, decls)

	if len(got.HardErrors) != 1 {
		test.Errorf("expected one HardError for the int element; got %+v", got)
	}
}

func TestValidateProperties_ListOfEnumAccepts(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"stages": []any{"draft", "review"}}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{
			Name: "stages", Type: "list-of", ItemType: "enum", Values: []string{"draft", "review", "shipped"},
		}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 0 {
		test.Errorf("expected pass; got %+v", got)
	}
}

func TestValidateProperties_ListOfEnumRejectsOutOfValues(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"stages": []any{"draft", "shipping"}}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{
			Name: "stages", Type: "list-of", ItemType: "enum", Values: []string{"draft", "review", "shipped"},
		}}},
	}

	got := node.ValidateProperties(parsed, decls)

	if len(got.HardErrors) != 1 || got.HardErrors[0].Kind != node.ErrEnumViolation {
		test.Errorf("expected one ErrEnumViolation; got %+v", got)
	}
}

func TestValidateProperties_ListOfRejectsNonList(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"labels": "auth"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "labels", Type: "list-of", ItemType: "string"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 1 || got.HardErrors[0].Kind != node.ErrTypeMismatch {
		test.Errorf("expected ErrTypeMismatch; got %+v", got)
	}
}

func TestWhichRequiredWereUnset_NotRequired(test *testing.T) {
	before := &node.Node{Type: "ticket", Properties: map[string]any{"x": "v"}}
	after := &node.Node{Type: "ticket", Properties: map[string]any{}}

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "x", Type: "string"}}},
	}

	if got := node.WhichRequiredWereUnset(before, after, decls); len(got) != 0 {
		test.Errorf("expected empty; got %v", got)
	}
}

func TestWhichRequiredWereUnset_RequiredUnsetReturnsName(test *testing.T) {
	before := &node.Node{Type: "ticket", Properties: map[string]any{"summary": "v"}}
	after := &node.Node{Type: "ticket", Properties: map[string]any{}}

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "summary", Type: "string", Required: true}}},
	}

	got := node.WhichRequiredWereUnset(before, after, decls)

	if len(got) != 1 || got[0] != "summary" {
		test.Errorf("expected [summary]; got %v", got)
	}
}

func TestWhichRequiredWereUnset_RequiredStillPresentReturnsEmpty(test *testing.T) {
	before := &node.Node{Type: "ticket", Properties: map[string]any{"summary": "v"}}
	after := &node.Node{Type: "ticket", Properties: map[string]any{"summary": "v2"}}

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "summary", Type: "string", Required: true}}},
	}

	if got := node.WhichRequiredWereUnset(before, after, decls); len(got) != 0 {
		test.Errorf("expected empty; got %v", got)
	}
}
