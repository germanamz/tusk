package filter_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/filter"
)

// The compiler chooses its comparison strategy from PropertyPredicate.ResolvedType
// (stamped by Validate). These tests hand-build the resolved AST and assert the
// SQL shape per type, independent of the validate-time resolution.

func TestCompile_DateOrderingComparesAsText(test *testing.T) {
	expr := &filter.PropertyPredicate{
		Property:     "due",
		Op:           filter.OpGE,
		Value:        filter.StringValue{V: "2026-08-01"},
		ResolvedType: "date",
	}

	sql, params, err := filter.Compile(expr, filter.CompileOptions{})

	if err != nil {
		test.Fatalf("Compile: %v", err)
	}

	if strings.Contains(sql, "CAST(") {
		test.Errorf("date ordering must not CAST to integer: %s", sql)
	}

	if !strings.Contains(sql, `json_extract(properties_json, '$."due"') >= ?`) {
		test.Errorf("expected quoted text comparison: %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"2026-08-01"}) {
		test.Errorf("params = %v, want [2026-08-01]", params)
	}
}

func TestCompile_DateRangeComparesAsText(test *testing.T) {
	expr := &filter.PropertyPredicate{
		Property:     "due",
		Op:           filter.OpRange,
		Value:        filter.RangeValue{Min: "2026-08-01", Max: "2026-12-31"},
		ResolvedType: "date",
	}

	sql, params, err := filter.Compile(expr, filter.CompileOptions{})

	if err != nil {
		test.Fatalf("Compile: %v", err)
	}

	if strings.Contains(sql, "CAST(") {
		test.Errorf("date range must not CAST to integer: %s", sql)
	}

	if !strings.Contains(sql, `json_extract(properties_json, '$."due"') BETWEEN ? AND ?`) {
		test.Errorf("expected quoted text BETWEEN: %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"2026-08-01", "2026-12-31"}) {
		test.Errorf("params = %v, want [2026-08-01 2026-12-31]", params)
	}
}

func TestCompile_EnumOrderingExpandsToInSetByName(test *testing.T) {
	expr := &filter.PropertyPredicate{
		Property:     "priority",
		Op:           filter.OpGE,
		Value:        filter.StringValue{V: "medium"},
		ResolvedType: "enum",
		EnumValues:   []string{"low", "medium", "high"},
	}

	sql, params, err := filter.Compile(expr, filter.CompileOptions{})

	if err != nil {
		test.Fatalf("Compile: %v", err)
	}

	if !strings.Contains(sql, `json_extract(properties_json, '$."priority"') IN (?, ?)`) {
		test.Errorf("expected IN-set of two names: %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"medium", "high"}) {
		test.Errorf("params = %v, want [medium high]", params)
	}
}

func TestCompile_EnumOrderingAcceptsIndex(test *testing.T) {
	expr := &filter.PropertyPredicate{
		Property:     "priority",
		Op:           filter.OpGE,
		Value:        filter.StringValue{V: "2"},
		ResolvedType: "enum",
		EnumValues:   []string{"low", "medium", "high"},
	}

	sql, params, err := filter.Compile(expr, filter.CompileOptions{})

	if err != nil {
		test.Fatalf("Compile: %v", err)
	}

	if !strings.Contains(sql, `json_extract(properties_json, '$."priority"') IN (?)`) {
		test.Errorf("expected IN-set of one name: %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"high"}) {
		test.Errorf("params = %v, want [high]", params)
	}
}

func TestCompile_EnumOrderingEmptySetMatchesNothing(test *testing.T) {
	expr := &filter.PropertyPredicate{
		Property:     "priority",
		Op:           filter.OpGT,
		Value:        filter.StringValue{V: "high"},
		ResolvedType: "enum",
		EnumValues:   []string{"low", "medium", "high"},
	}

	sql, params, err := filter.Compile(expr, filter.CompileOptions{})

	if err != nil {
		test.Fatalf("Compile: %v", err)
	}

	if !strings.Contains(sql, "0 = 1") {
		test.Errorf("expected constant-false predicate for empty satisfying set: %s", sql)
	}

	if len(params) != 0 {
		test.Errorf("params = %v, want empty", params)
	}
}

func TestCompile_EnumRangeExpandsInclusive(test *testing.T) {
	expr := &filter.PropertyPredicate{
		Property:     "priority",
		Op:           filter.OpRange,
		Value:        filter.RangeValue{Min: "low", Max: "high"},
		ResolvedType: "enum",
		EnumValues:   []string{"low", "medium", "high"},
	}

	sql, params, err := filter.Compile(expr, filter.CompileOptions{})

	if err != nil {
		test.Fatalf("Compile: %v", err)
	}

	if !strings.Contains(sql, `json_extract(properties_json, '$."priority"') IN (?, ?, ?)`) {
		test.Errorf("expected IN-set of three names: %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"low", "medium", "high"}) {
		test.Errorf("params = %v, want [low medium high]", params)
	}
}

// Enum equality stays on the legacy name string-compare path (unchanged).
func TestCompile_EnumEqualityUnchanged(test *testing.T) {
	expr := &filter.PropertyPredicate{
		Property:     "priority",
		Op:           filter.OpEQ,
		Value:        filter.StringValue{V: "high"},
		ResolvedType: "enum",
		EnumValues:   []string{"low", "medium", "high"},
	}

	sql, params, err := filter.Compile(expr, filter.CompileOptions{})

	if err != nil {
		test.Fatalf("Compile: %v", err)
	}

	if strings.Contains(sql, " IN (") {
		test.Errorf("enum equality must not expand to IN-set: %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"high"}) {
		test.Errorf("params = %v, want [high]", params)
	}
}

// Int equality now casts to INTEGER so a bound string matches an integer-stored
// value (previously it TEXT-compared and never matched — issue #664 audit H1).
func TestCompile_IntEqualityCastsToInteger(test *testing.T) {
	expr := &filter.PropertyPredicate{
		Property:     "order",
		Op:           filter.OpEQ,
		Value:        filter.StringValue{V: "2"},
		ResolvedType: "int",
	}

	sql, params, err := filter.Compile(expr, filter.CompileOptions{})

	if err != nil {
		test.Fatalf("Compile: %v", err)
	}

	if !strings.Contains(sql, `CAST(json_extract(properties_json, '$."order"') AS INTEGER) = ?`) {
		test.Errorf("expected integer-affinity equality: %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"2"}) {
		test.Errorf("params = %v, want [2]", params)
	}
}

func TestCompile_FloatOrderingCastsToReal(test *testing.T) {
	expr := &filter.PropertyPredicate{
		Property:     "score",
		Op:           filter.OpGT,
		Value:        filter.StringValue{V: "9.5"},
		ResolvedType: "float",
	}

	sql, params, err := filter.Compile(expr, filter.CompileOptions{})

	if err != nil {
		test.Fatalf("Compile: %v", err)
	}

	if !strings.Contains(sql, `CAST(json_extract(properties_json, '$."score"') AS REAL) > ?`) {
		test.Errorf("expected REAL-affinity ordering (no fractional truncation): %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"9.5"}) {
		test.Errorf("params = %v, want [9.5]", params)
	}
}

func TestCompile_FloatRangeCastsToReal(test *testing.T) {
	expr := &filter.PropertyPredicate{
		Property:     "score",
		Op:           filter.OpRange,
		Value:        filter.RangeValue{Min: "1.5", Max: "2.5"},
		ResolvedType: "float",
	}

	sql, params, err := filter.Compile(expr, filter.CompileOptions{})

	if err != nil {
		test.Fatalf("Compile: %v", err)
	}

	if !strings.Contains(sql, `CAST(json_extract(properties_json, '$."score"') AS REAL) BETWEEN ? AND ?`) {
		test.Errorf("expected REAL-affinity BETWEEN: %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"1.5", "2.5"}) {
		test.Errorf("params = %v, want [1.5 2.5]", params)
	}
}

// String ordering compares lexically as TEXT, never integer-coerced (which would
// collapse every string to 0 and match everything — issue #664 audit M2).
func TestCompile_StringOrderingComparesAsText(test *testing.T) {
	expr := &filter.PropertyPredicate{
		Property:     "code",
		Op:           filter.OpLT,
		Value:        filter.StringValue{V: "m"},
		ResolvedType: "string",
	}

	sql, _, err := filter.Compile(expr, filter.CompileOptions{})

	if err != nil {
		test.Fatalf("Compile: %v", err)
	}

	if strings.Contains(sql, "CAST(") {
		test.Errorf("string ordering must not CAST: %s", sql)
	}

	if !strings.Contains(sql, `json_extract(properties_json, '$."code"') < ?`) {
		test.Errorf("expected lexical text comparison: %s", sql)
	}
}

// A resolved enum sort key orders by declared position (a CASE over the member
// names), not lexically by stored name — issue #664 audit M3.
func TestCompile_EnumSortOrdersByDeclaredPosition(test *testing.T) {
	sql, _, err := filter.Compile(nil, filter.CompileOptions{
		SortKeys: []filter.SortKey{{
			Property:     "priority",
			Descending:   true,
			ResolvedType: "enum",
			EnumValues:   []string{"low", "medium", "high"},
		}},
	})

	if err != nil {
		test.Fatalf("Compile: %v", err)
	}

	want := `CASE json_extract(properties_json, '$."priority"') WHEN 'low' THEN 0 WHEN 'medium' THEN 1 WHEN 'high' THEN 2 ELSE 3 END DESC`

	if !strings.Contains(sql, want) {
		test.Errorf("expected declared-order CASE sort, got: %s", sql)
	}
}

// An unresolved sort key (no manifest resolution) keeps the legacy lexical
// ORDER BY, so ad-hoc sorts and callers that never resolve are unaffected.
func TestCompile_UnresolvedEnumSortStaysLexical(test *testing.T) {
	sql, _, err := filter.Compile(nil, filter.CompileOptions{
		SortKeys: []filter.SortKey{{Property: "priority", Descending: true}},
	})

	if err != nil {
		test.Fatalf("Compile: %v", err)
	}

	if strings.Contains(sql, "CASE") {
		test.Errorf("unresolved sort must not emit a CASE: %s", sql)
	}

	if !strings.Contains(sql, `json_extract(properties_json, '$."priority"') DESC`) {
		test.Errorf("expected lexical json_extract sort: %s", sql)
	}
}

// Datetime ordering compares as text, like date.
func TestCompile_DatetimeOrderingComparesAsText(test *testing.T) {
	expr := &filter.PropertyPredicate{
		Property:     "started",
		Op:           filter.OpLT,
		Value:        filter.StringValue{V: "2026-08-01T00:00:00Z"},
		ResolvedType: "datetime",
	}

	sql, _, err := filter.Compile(expr, filter.CompileOptions{})

	if err != nil {
		test.Fatalf("Compile: %v", err)
	}

	if strings.Contains(sql, "CAST(") {
		test.Errorf("datetime ordering must not CAST: %s", sql)
	}

	if !strings.Contains(sql, `json_extract(properties_json, '$."started"') < ?`) {
		test.Errorf("expected quoted text comparison: %s", sql)
	}
}
