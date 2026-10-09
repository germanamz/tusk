package filter_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/manifest"
)

// strictManifest is a small schema'd vault: notes carry an enum `domain`,
// tickets an enum `priority` and a list-of(enum) `labels`, and the built-in
// `section` sub-unit type is merged in.
func strictManifest() manifest.Manifest {
	return manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"note": {Properties: []manifest.PropertyDecl{
				{Name: "domain", Type: "enum", Values: []string{"product", "engineering"}},
				{Name: "summary", Type: "string"},
			}},
			"ticket": {Properties: []manifest.PropertyDecl{
				{Name: "priority", Type: "enum", Values: []string{"low", "medium", "high"}},
				{Name: "labels", Type: "list-of", ItemType: "enum", Values: []string{"bug", "chore"}},
			}},
			"section": {Properties: []manifest.PropertyDecl{
				{Name: "heading-level", Type: "int"},
			}},
		},
		EdgeTypes: manifest.EdgeTypes{
			"references": {From: []string{"*"}, To: []string{"*"}, Cardinality: manifest.CardinalityManyToMany},
		},
	}
}

func strictErrors(test *testing.T, input string) []filter.ValidationError {
	test.Helper()

	expr, parseErrs := filter.NewParser(input).Parse()

	if len(parseErrs) > 0 {
		test.Fatalf("parse %q: %v", input, parseErrs)
	}

	return filter.ValidateStrict(expr, strictManifest())
}

func TestValidateStrict_AcceptsDeclaredNames(test *testing.T) {
	for _, input := range []string{
		"type=note AND domain=product AND NOT path=docs/product/**",
		"domain=product AND references-> (type=note AND NOT domain=product AND NOT id=docs/glossary)",
		"type=note OR domain=engineering",
		"id=docs/glossary OR title=Glossary",
		"NOT type=section",
		"type=section AND heading-level=1",
		"type=ticket AND labels=bug",
		"type=:note",
		"modified-since:7d",
	} {
		test.Run(input, func(test *testing.T) {
			if errs := strictErrors(test, input); len(errs) > 0 {
				test.Fatalf("ValidateStrict(%q) = %v, want no errors", input, errs)
			}
		})
	}
}

func TestValidateStrict_RejectsTypos(test *testing.T) {
	cases := []struct {
		input       string
		wantMessage string
		wantHint    string
	}{
		{"type=notee", `node type "notee" not declared in manifest`, `did you mean "note"?`},
		{"type!=notee", `node type "notee" not declared in manifest`, `did you mean "note"?`},
		{"references-> (type=notee)", `node type "notee" not declared in manifest`, `did you mean "note"?`},
		{"domin=product", `property "domin" is not declared on any node type`, `did you mean "domain"?`},
		{"references-> domin=product", `property "domin" is not declared on any node type`, `did you mean "domain"?`},
		{"type=ticket AND domain=product", `property "domain" is not declared on node type "ticket"`, ""},
		{"(type=ticket OR type=note) AND type=ticket AND summary=x", `property "summary" is not declared on node type "ticket"`, ""},
		{"domain=prodcut", `"prodcut" is not a value of enum property "domain"`, "valid values: product, engineering"},
		{"domain!=prodcut", `"prodcut" is not a value of enum property "domain"`, "valid values: product, engineering"},
		{"type=ticket AND labels=bugg", `"bugg" is not a value of enum property "labels"`, "valid values: bug, chore"},
	}

	for _, testCase := range cases {
		test.Run(testCase.input, func(test *testing.T) {
			errs := strictErrors(test, testCase.input)

			if len(errs) != 1 {
				test.Fatalf("ValidateStrict(%q) = %v, want exactly one error", testCase.input, errs)
			}

			if errs[0].Message != testCase.wantMessage {
				test.Errorf("Message = %q, want %q", errs[0].Message, testCase.wantMessage)
			}

			if errs[0].Hint != testCase.wantHint {
				test.Errorf("Hint = %q, want %q", errs[0].Hint, testCase.wantHint)
			}
		})
	}
}

// A misspelled type already gets its own error; the properties scoped to it
// must not pile "not declared on node type" noise on top.
func TestValidateStrict_UndeclaredScopeReportsOnlyTheType(test *testing.T) {
	errs := strictErrors(test, "type=notee AND domain=product")

	if len(errs) != 1 || !strings.Contains(errs[0].Message, `node type "notee"`) {
		test.Fatalf("errors = %v, want only the node-type error", errs)
	}
}

// The strict checks add to Validate; they don't replace it.
func TestValidateStrict_KeepsValidateErrors(test *testing.T) {
	errs := strictErrors(test, "type=ticket AND priority>=urgent")

	if len(errs) != 1 || !strings.Contains(errs[0].Message, `"urgent" is not a value or 0-based index`) {
		test.Fatalf("errors = %v, want the ordering-operand error", errs)
	}
}

// tusk query keeps accepting ad-hoc names: Validate stays lenient.
func TestValidate_StaysLenientOnUndeclaredNames(test *testing.T) {
	expr, parseErrs := filter.NewParser("type=notee AND domian=x AND domain=prodcut").Parse()

	if len(parseErrs) > 0 {
		test.Fatalf("parse: %v", parseErrs)
	}

	if errs := filter.Validate(expr, strictManifest()); len(errs) > 0 {
		test.Fatalf("Validate = %v, want no errors", errs)
	}
}
