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
