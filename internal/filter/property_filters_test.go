package filter_test

import (
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/filter"
)

func TestParser_ContainsOperator(test *testing.T) {
	expr, errs := filter.NewParser("products~=a").Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	pred := expr.(*filter.PropertyPredicate)

	if pred.Op != filter.OpContains || pred.Value.(filter.StringValue).V != "a" {
		test.Fatalf("pred = %+v, want products ~= a", pred)
	}

	if filter.OpContains.String() != "~=" {
		test.Errorf("OpContains.String() = %q, want ~=", filter.OpContains.String())
	}
}

func TestParser_AnyValue(test *testing.T) {
	for _, input := range []string{"domain=*", "domain:*", "domain!=*", "title=*"} {
		expr, errs := filter.NewParser(input).Parse()

		if len(errs) > 0 {
			test.Fatalf("%q: errors: %v", input, errs)
		}

		if _, isAny := expr.(*filter.PropertyPredicate).Value.(filter.AnyValue); !isAny {
			test.Errorf("%q: value = %#v, want AnyValue", input, expr.(*filter.PropertyPredicate).Value)
		}
	}

	// path and id keep the glob reading, so path=* is still the top-level files.
	expr, _ := filter.NewParser("path=*").Parse()

	if value, isString := expr.(*filter.PropertyPredicate).Value.(filter.StringValue); !isString || value.V != "*" {
		test.Errorf("path=* value = %#v, want the glob StringValue", expr.(*filter.PropertyPredicate).Value)
	}
}

func TestParser_ListValue(test *testing.T) {
	expr, errs := filter.NewParser(`products=[a, "b c" ,today-1d]`).Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	list, isList := expr.(*filter.PropertyPredicate).Value.(filter.ListValue)

	if !isList {
		test.Fatalf("value = %#v, want ListValue", expr.(*filter.PropertyPredicate).Value)
	}

	want := []filter.StringValue{{V: "a", Bareword: true}, {V: "b c"}, {V: "today-1d", Bareword: true}}

	if len(list.Items) != len(want) {
		test.Fatalf("items = %+v, want %+v", list.Items, want)
	}

	for index := range want {
		if list.Items[index] != want[index] {
			test.Errorf("item %d = %+v, want %+v", index, list.Items[index], want[index])
		}
	}
}

// A list can be followed by more terms, the way any value can.
func TestParser_ListValueThenMoreTerms(test *testing.T) {
	expr, errs := filter.NewParser("products=[a,b] AND type=service").Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	if _, isAnd := expr.(*filter.AndExpr); !isAnd {
		test.Fatalf("expr = %#v, want AndExpr", expr)
	}
}

func TestParser_RelativeDateWithPlus(test *testing.T) {
	expr, errs := filter.NewParser("due<today+2w").Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	if value := expr.(*filter.PropertyPredicate).Value.(filter.StringValue); value.V != "today+2w" || !value.Bareword {
		test.Errorf("value = %+v, want bareword today+2w", value)
	}
}

func TestParser_PropertyValueErrors(test *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"domain>*", "`*` (any value) only works with = or !="},
		{"products~=*", "`*` (any value) only works with = or !="},
		{"domain=*..b", "a range bound can't be a pattern"},
		{"products~=[a]", "a list only works with = or !="},
		{"products<[a]", "a list only works with = or !="},
		{"products=[]", "a list needs at least one item"},
		{"products=[a,]", "expected a list item"},
		{"products=[a b]", "expected , or ]"},
		{"products=[a", "expected , or ]"},
		{"products=[a*]", "wildcards (* and ?) only match path and id; quote the value to compare it literally"},
		{"products=[a]..[b]", "a list can't be a range bound"},
		{"products=[[a]]", "expected a list item"},
	}

	for _, testCase := range cases {
		test.Run(testCase.input, func(test *testing.T) {
			_, errs := filter.NewParser(testCase.input).Parse()

			if len(errs) == 0 {
				test.Fatalf("Parse(%q) = no errors, want %q", testCase.input, testCase.want)
			}

			if errs[0].Message != testCase.want {
				test.Errorf("Parse(%q) error = %q, want %q", testCase.input, errs[0].Message, testCase.want)
			}
		})
	}
}

func TestValidate_PropertyFilterErrors(test *testing.T) {
	cases := []struct {
		input       string
		wantMessage string
		wantHint    string
	}{
		{"type=service AND products=a", `property "products" is a list`, "use products~=a for one item, or products=[a, ...] for several"},
		{"type=service AND products!=a", `property "products" is a list`, "use products~=a for one item, or products=[a, ...] for several"},
		{"type=service AND products<b", `property "products" is a list; < compares single values`, "use products~=b to test an item"},
		{"type=service AND products=a..b", `property "products" is a list; .. compares single values`, "use products~=a to test an item"},
		{"type=service AND domain~=billing", `property "domain" is declared enum, not a list`, "use domain=billing"},
		{"type=service AND domain=[billing]", `property "domain" is declared enum, not a list`, "use domain=billing"},
		{"title~=x", `~= tests list items, and "title" is not a list`, "use title=x"},
		{"id=[a]", `"id" is not a list`, ""},
		{"type=service AND mood<today-1d", `"today-1d" is a relative date, but "mood" is not declared as a date or datetime`, "declare it with type = \"date\" or \"datetime\""},
		{"type=service AND checked<today-5x", `"today-5x" is not a valid value for "checked"`, "expected ISO date (YYYY-MM-DD) or a relative date like today-90d"},
		{`type=service AND checked<"today"`, `"today" is not a valid value for "checked"`, "expected ISO date (YYYY-MM-DD) or a relative date like today-90d"},
		{"type=service AND checked=today-99999999d", `"today-99999999d" is not a valid value for "checked"`, "expected ISO date (YYYY-MM-DD) or a relative date like today-90d"},
	}

	loaded := propertyFiltersManifest()

	for _, testCase := range cases {
		test.Run(testCase.input, func(test *testing.T) {
			expr, parseErrs := filter.NewParser(testCase.input).Parse()

			if len(parseErrs) > 0 {
				test.Fatalf("parse %q: %v", testCase.input, parseErrs)
			}

			errs := filter.Validate(expr, loaded)

			if len(errs) != 1 {
				test.Fatalf("Validate(%q) = %v, want exactly one error", testCase.input, errs)
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

// An undeclared property may hold a list or a scalar, so ~= and [a, b] are
// left to match whichever it holds.
func TestValidate_UndeclaredListOperatorsPass(test *testing.T) {
	for _, input := range []string{"tags~=x", "tags=[x, y]", "tags!=[x]", "tags=*", "type=service AND mood=today"} {
		expr, _ := filter.NewParser(input).Parse()

		if errs := filter.Validate(expr, propertyFiltersManifest()); len(errs) > 0 {
			test.Errorf("Validate(%q) = %v, want no errors", input, errs)
		}
	}
}

// A relative date resolves on the compile clock, months and years clamping to
// the end of a shorter month.
func TestCompile_RelativeDateParams(test *testing.T) {
	endOfMarch := time.Date(2026, 3, 31, 23, 30, 0, 0, time.FixedZone("test", 2*60*60))

	cases := []struct {
		input string
		now   time.Time
		want  string
	}{
		{"type=service AND checked<today", endOfMarch, "2026-03-31"},
		{"type=service AND checked<today-1m", endOfMarch, "2026-02-28"},
		{"type=service AND checked<today+1m", endOfMarch, "2026-04-30"},
		{"type=service AND checked<today-2w", endOfMarch, "2026-03-17"},
		{"type=service AND checked<today-1d", endOfMarch, "2026-03-30"},
		{"type=service AND checked<today-2y", time.Date(2028, 2, 29, 12, 0, 0, 0, time.UTC), "2026-02-28"},
		{"type=service AND checked<now-48h", endOfMarch, "2026-03-29"},
		{"type=service AND seen<now", endOfMarch, "2026-03-31T21:30:00Z"},
		{"type=service AND seen<today", endOfMarch, "2026-03-30T22:00:00Z"},
		{"type=service AND seen<now-1h", endOfMarch, "2026-03-31T20:30:00Z"},
	}

	for _, testCase := range cases {
		test.Run(testCase.input, func(test *testing.T) {
			expr, _ := filter.NewParser(testCase.input).Parse()

			if errs := filter.Validate(expr, propertyFiltersManifest()); len(errs) > 0 {
				test.Fatalf("validate: %v", errs)
			}

			_, params, compileErr := filter.Compile(expr, filter.CompileOptions{Now: testCase.now})

			if compileErr != nil {
				test.Fatalf("compile: %v", compileErr)
			}

			if len(params) != 2 || params[1] != testCase.want {
				test.Errorf("params = %v, want [service %s]", params, testCase.want)
			}
		})
	}
}

// Quoting a relative literal keeps it literal on an equality, the way quoting
// keeps a glob literal.
func TestCompile_QuotedRelativeDateStaysLiteral(test *testing.T) {
	expr, _ := filter.NewParser(`type=service AND checked="today"`).Parse()

	if errs := filter.Validate(expr, propertyFiltersManifest()); len(errs) > 0 {
		test.Fatalf("validate: %v", errs)
	}

	_, params, compileErr := filter.Compile(expr, filter.CompileOptions{Now: propertyFiltersNow})

	if compileErr != nil {
		test.Fatalf("compile: %v", compileErr)
	}

	if params[1] != "today" {
		test.Errorf("params = %v, want the literal \"today\"", params)
	}
}

// Without a clock, a filter that needs one fails instead of guessing.
func TestCompile_ClockRequired(test *testing.T) {
	for _, input := range []string{"type=service AND checked<today-90d", "modified-since:7d", "type=service AND seen>=now"} {
		expr, _ := filter.NewParser(input).Parse()

		if errs := filter.Validate(expr, propertyFiltersManifest()); len(errs) > 0 {
			test.Fatalf("validate %q: %v", input, errs)
		}

		_, _, compileErr := filter.Compile(expr, filter.CompileOptions{})

		if compileErr == nil || !strings.Contains(compileErr.Error(), "CompileOptions.Now") {
			test.Errorf("Compile(%q) error = %v, want one naming CompileOptions.Now", input, compileErr)
		}
	}

	// An absolute date needs no clock.
	expr, _ := filter.NewParser("modified-since:2026-05-01").Parse()
	filter.Validate(expr, propertyFiltersManifest())

	if _, _, compileErr := filter.Compile(expr, filter.CompileOptions{}); compileErr != nil {
		test.Errorf("Compile(modified-since:2026-05-01) = %v, want no error", compileErr)
	}
}

func TestValidateStrict_ListItemsAreEnumChecked(test *testing.T) {
	for _, input := range []string{"type=ticket AND labels~=bug", "type=ticket AND labels=[bug, chore]", "type=ticket AND labels!=*"} {
		if errs := strictErrors(test, input); len(errs) > 0 {
			test.Errorf("ValidateStrict(%q) = %v, want no errors", input, errs)
		}
	}

	for _, input := range []string{"type=ticket AND labels~=bugg", "type=ticket AND labels=[bug, bugg]"} {
		errs := strictErrors(test, input)

		if len(errs) != 1 || errs[0].Message != `"bugg" is not a value of enum property "labels"` {
			test.Errorf("ValidateStrict(%q) = %v, want the one enum-value error", input, errs)
		}
	}
}

// A qualified type != is the complement of its =: rows from another source,
// or with none, pass.
func TestCompile_QualifiedTypeNotEqualIsComplement(test *testing.T) {
	expr, _ := filter.NewParser("type!=tusk:section").Parse()

	sqlText, params, compileErr := filter.Compile(expr, filter.CompileOptions{})

	if compileErr != nil {
		test.Fatalf("compile: %v", compileErr)
	}

	if !strings.Contains(sqlText, "NOT ((source = ? AND type = ?) IS TRUE)") {
		test.Errorf("sql = %s, want the negated qualified equality", sqlText)
	}

	if len(params) != 2 || params[0] != "tusk" || params[1] != "section" {
		test.Errorf("params = %v, want [tusk section]", params)
	}
}

// After an edge arrow, the list operators read the target row's properties.
func TestCompile_ListOperatorsAfterEdgeArrow(test *testing.T) {
	for _, input := range []string{"references-> products~=a", "references-> products=[a, b]", "references-> products=*"} {
		expr, _ := filter.NewParser(input).Parse()

		sqlText, _, compileErr := filter.Compile(expr, filter.CompileOptions{})

		if compileErr != nil {
			test.Fatalf("compile %q: %v", input, compileErr)
		}

		if !strings.Contains(sqlText, `json_each(n0.properties_json, '$."products"') AS item`) {
			test.Errorf("%q: sql = %s, want json_each over the target alias n0", input, sqlText)
		}
	}
}
