package filter_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/manifest"
)

// typedManifest mirrors the real workspace: a `ticket` type with an enum
// `priority` (low<medium<high), a `date` `due`, a `datetime` `started`, and an
// `int` `order`; plus the divergent `status` enum declared on both `plan` and
// `package` with different orderings.
func typedManifest() manifest.Manifest {
	return manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{
				{Name: "priority", Type: "enum", Values: []string{"low", "medium", "high"}},
				{Name: "due", Type: "date"},
				{Name: "started", Type: "datetime"},
				{Name: "order", Type: "int"},
			}},
			"plan": {Properties: []manifest.PropertyDecl{
				{Name: "status", Type: "enum", Values: []string{"draft", "in-progress", "shipped", "abandoned"}},
			}},
			"package": {Properties: []manifest.PropertyDecl{
				{Name: "status", Type: "enum", Values: []string{"stable", "in-flux", "experimental"}},
			}},
		},
	}
}

// findProperty returns the first PropertyPredicate named name found in expr.
func findProperty(expr filter.Expr, name string) *filter.PropertyPredicate {
	switch typed := expr.(type) {
	case *filter.OrExpr:
		if found := findProperty(typed.Left, name); found != nil {
			return found
		}

		return findProperty(typed.Right, name)
	case *filter.AndExpr:
		if found := findProperty(typed.Left, name); found != nil {
			return found
		}

		return findProperty(typed.Right, name)
	case *filter.NotExpr:
		return findProperty(typed.Inner, name)
	case *filter.PropertyPredicate:
		if typed.Property == name {
			return typed
		}
	}

	return nil
}

func mustParse(test *testing.T, input string) filter.Expr {
	test.Helper()

	expr, parseErrs := filter.NewParser(input).Parse()

	if len(parseErrs) != 0 {
		test.Fatalf("parse %q: %+v", input, parseErrs)
	}

	return expr
}

func TestValidate_StampsEnumWithinTypeScope(test *testing.T) {
	expr := mustParse(test, "type=ticket priority>=medium")

	errs := filter.Validate(expr, typedManifest())

	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %+v", errs)
	}

	pred := findProperty(expr, "priority")

	if pred.ResolvedType != "enum" {
		test.Errorf("ResolvedType = %q, want enum", pred.ResolvedType)
	}

	if !reflect.DeepEqual(pred.EnumValues, []string{"low", "medium", "high"}) {
		test.Errorf("EnumValues = %v, want [low medium high]", pred.EnumValues)
	}
}

func TestValidate_StampsDateWithinTypeScope(test *testing.T) {
	expr := mustParse(test, "type=ticket due>=2026-08-01")

	errs := filter.Validate(expr, typedManifest())

	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %+v", errs)
	}

	if pred := findProperty(expr, "due"); pred.ResolvedType != "date" {
		test.Errorf("ResolvedType = %q, want date", pred.ResolvedType)
	}
}

func TestValidate_StampsIntWithinTypeScope(test *testing.T) {
	expr := mustParse(test, "type=ticket order>=2")

	errs := filter.Validate(expr, typedManifest())

	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %+v", errs)
	}

	if pred := findProperty(expr, "order"); pred.ResolvedType != "int" {
		test.Errorf("ResolvedType = %q, want int", pred.ResolvedType)
	}
}

func TestValidate_RejectsInvalidEnumValue(test *testing.T) {
	expr := mustParse(test, "type=ticket priority>=urgent")

	errs := filter.Validate(expr, typedManifest())

	if len(errs) == 0 {
		test.Fatalf("expected a validation error for an undeclared enum value")
	}

	if !strings.Contains(errs[0].Message, "priority") {
		test.Errorf("message should name the property: %q", errs[0].Message)
	}

	if !strings.Contains(errs[0].Hint, "low") {
		test.Errorf("hint should list valid values: %q", errs[0].Hint)
	}
}

func TestValidate_RejectsEnumIndexOutOfRange(test *testing.T) {
	expr := mustParse(test, "type=ticket priority>=9")

	errs := filter.Validate(expr, typedManifest())

	if len(errs) == 0 {
		test.Fatalf("expected a validation error for an out-of-range enum index")
	}
}

func TestValidate_RejectsUnparseableDate(test *testing.T) {
	expr := mustParse(test, "type=ticket due>=notadate")

	errs := filter.Validate(expr, typedManifest())

	if len(errs) == 0 {
		test.Fatalf("expected a validation error for a malformed date")
	}
}

func TestValidate_AcceptsValidDatetime(test *testing.T) {
	expr := mustParse(test, "type=ticket started<2026-08-01T00:00:00Z")

	errs := filter.Validate(expr, typedManifest())

	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %+v", errs)
	}

	if pred := findProperty(expr, "started"); pred.ResolvedType != "datetime" {
		test.Errorf("ResolvedType = %q, want datetime", pred.ResolvedType)
	}
}

func TestValidate_AmbiguousEnumOrderingErrors(test *testing.T) {
	// `status` is enum on plan and package with divergent value sets; with no
	// `type=` to disambiguate, an ordering comparison is ambiguous.
	expr := mustParse(test, "status>=shipped")

	errs := filter.Validate(expr, typedManifest())

	if len(errs) == 0 {
		test.Fatalf("expected an ambiguity error for status>=shipped")
	}

	if !strings.Contains(errs[0].Message, "status") {
		test.Errorf("message should name the property: %q", errs[0].Message)
	}
}

func TestValidate_TypeScopeDisambiguatesEnum(test *testing.T) {
	expr := mustParse(test, "type=plan status>=shipped")

	errs := filter.Validate(expr, typedManifest())

	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %+v", errs)
	}

	pred := findProperty(expr, "status")

	if !reflect.DeepEqual(pred.EnumValues, []string{"draft", "in-progress", "shipped", "abandoned"}) {
		test.Errorf("EnumValues = %v, want plan's status set", pred.EnumValues)
	}
}

func TestValidate_AmbiguousEnumEqualityIsNotAnError(test *testing.T) {
	// Equality compares by name regardless of which type's domain applies, so
	// ambiguity must not error for `=`/`!=`.
	expr := mustParse(test, "status=shipped")

	errs := filter.Validate(expr, typedManifest())

	if len(errs) != 0 {
		test.Errorf("equality on an ambiguous enum must not error: %+v", errs)
	}
}

func TestValidate_UndeclaredPropertyStaysLegacy(test *testing.T) {
	expr := mustParse(test, "whatever>=5")

	errs := filter.Validate(expr, typedManifest())

	if len(errs) != 0 {
		test.Errorf("undeclared property must not error: %+v", errs)
	}

	if pred := findProperty(expr, "whatever"); pred.ResolvedType != "" {
		test.Errorf("ResolvedType = %q, want empty (legacy)", pred.ResolvedType)
	}
}

func TestValidate_OrOfTypedConjunctsResolvesEachBranch(test *testing.T) {
	expr := mustParse(test, "(type=plan AND status>=shipped) OR (type=package AND status>=stable)")

	errs := filter.Validate(expr, typedManifest())

	if len(errs) != 0 {
		test.Errorf("each typed conjunct should resolve without error: %+v", errs)
	}
}

// TestResolveSortKeys stamps the declared type onto sort keys so the compiler
// can order an enum by declared position. Core columns, undeclared properties,
// and names that are ambiguous across the query's type scope are left bare —
// issue #664 audit M3.
func TestResolveSortKeys(test *testing.T) {
	loaded := typedManifest()

	// enum in a single-type scope resolves with its member list.
	keys := []filter.SortKey{{Property: "priority", Descending: true}}
	filter.ResolveSortKeys(mustParse(test, "type=ticket"), loaded, keys)

	if keys[0].ResolvedType != "enum" {
		test.Errorf("priority ResolvedType = %q, want enum", keys[0].ResolvedType)
	}

	if !reflect.DeepEqual(keys[0].EnumValues, []string{"low", "medium", "high"}) {
		test.Errorf("priority EnumValues = %v, want [low medium high]", keys[0].EnumValues)
	}

	// int resolves its type but carries no enum values.
	intKeys := []filter.SortKey{{Property: "order"}}
	filter.ResolveSortKeys(mustParse(test, "type=ticket"), loaded, intKeys)

	if intKeys[0].ResolvedType != "int" || intKeys[0].EnumValues != nil {
		test.Errorf("order = {%q, %v}, want {int, []}", intKeys[0].ResolvedType, intKeys[0].EnumValues)
	}

	// a core column is never resolved.
	coreKeys := []filter.SortKey{{Property: "id"}}
	filter.ResolveSortKeys(mustParse(test, "type=ticket"), loaded, coreKeys)

	if coreKeys[0].ResolvedType != "" {
		test.Errorf("core column id ResolvedType = %q, want empty", coreKeys[0].ResolvedType)
	}

	// an undeclared property is left bare (legacy lexical sort).
	adhocKeys := []filter.SortKey{{Property: "whatever"}}
	filter.ResolveSortKeys(mustParse(test, "type=ticket"), loaded, adhocKeys)

	if adhocKeys[0].ResolvedType != "" {
		test.Errorf("undeclared whatever ResolvedType = %q, want empty", adhocKeys[0].ResolvedType)
	}

	// `status` is a divergent enum on plan and package; with no type scope it is
	// ambiguous and must be left bare rather than picking one arbitrarily.
	ambiguousKeys := []filter.SortKey{{Property: "status", Descending: true}}
	filter.ResolveSortKeys(mustParse(test, ""), loaded, ambiguousKeys)

	if ambiguousKeys[0].ResolvedType != "" {
		test.Errorf("ambiguous status ResolvedType = %q, want empty", ambiguousKeys[0].ResolvedType)
	}

	// narrowing the scope to one type resolves it unambiguously.
	scopedKeys := []filter.SortKey{{Property: "status", Descending: true}}
	filter.ResolveSortKeys(mustParse(test, "type=plan"), loaded, scopedKeys)

	if !reflect.DeepEqual(scopedKeys[0].EnumValues, []string{"draft", "in-progress", "shipped", "abandoned"}) {
		test.Errorf("plan status EnumValues = %v, want plan's ordering", scopedKeys[0].EnumValues)
	}
}
