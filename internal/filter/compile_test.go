package filter_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/filter"
)

func TestCompile_CorePropertyEquality(test *testing.T) {
	expr, _ := filter.NewParser("type=ticket").Parse()

	sql, params, err := filter.Compile(expr, filter.CompileOptions{})

	if err != nil {
		test.Fatalf("Compile: %v", err)
	}

	if !strings.Contains(sql, "type = ?") {
		test.Errorf("sql missing core column comparison: %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"ticket"}) {
		test.Errorf("params = %v, want [ticket]", params)
	}
}

func TestCompile_NonCorePropertyUsesJSONExtract(test *testing.T) {
	expr, _ := filter.NewParser("priority=3").Parse()

	sql, params, _ := filter.Compile(expr, filter.CompileOptions{})

	if !strings.Contains(sql, `json_extract(properties_json, '$.priority')`) {
		test.Errorf("sql missing json_extract: %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"3"}) {
		test.Errorf("params = %v, want [3]", params)
	}
}

func TestCompile_NumericComparatorCasts(test *testing.T) {
	expr, _ := filter.NewParser("priority>=3").Parse()

	sql, _, _ := filter.Compile(expr, filter.CompileOptions{})

	if !strings.Contains(sql, "CAST(json_extract") {
		test.Errorf("sql missing CAST for numeric comparator: %s", sql)
	}

	if !strings.Contains(sql, ">= ?") {
		test.Errorf("sql missing >= operator: %s", sql)
	}
}

func TestCompile_RangeProducesBetween(test *testing.T) {
	expr, _ := filter.NewParser("priority=2..4").Parse()

	sql, params, _ := filter.Compile(expr, filter.CompileOptions{})

	if !strings.Contains(sql, "BETWEEN ? AND ?") {
		test.Errorf("sql missing BETWEEN: %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"2", "4"}) {
		test.Errorf("params = %v, want [2 4]", params)
	}
}

func TestCompile_BooleanComposition(test *testing.T) {
	expr, _ := filter.NewParser("type=ticket AND status=active").Parse()

	sql, _, _ := filter.Compile(expr, filter.CompileOptions{})

	if !strings.Contains(sql, " AND ") {
		test.Errorf("sql missing AND: %s", sql)
	}
}

func TestCompile_NotWraps(test *testing.T) {
	expr, _ := filter.NewParser("NOT status=completed").Parse()

	sql, _, _ := filter.Compile(expr, filter.CompileOptions{})

	if !strings.Contains(sql, "NOT (") {
		test.Errorf("sql missing NOT wrap: %s", sql)
	}
}

func TestCompile_NilExprMatchesAll(test *testing.T) {
	sql, params, _ := filter.Compile(nil, filter.CompileOptions{})

	if !strings.Contains(sql, "WHERE 1 = 1") {
		test.Errorf("sql for nil expr should match all: %s", sql)
	}

	if len(params) != 0 {
		test.Errorf("params = %v, want empty", params)
	}
}

func TestCompile_SortKeysAppendOrderBy(test *testing.T) {
	expr, _ := filter.NewParser("type=ticket").Parse()

	sql, _, _ := filter.Compile(expr, filter.CompileOptions{
		SortKeys: []filter.SortKey{
			{Property: "priority", Descending: true},
			{Property: "title", Descending: false},
		},
	})

	if !strings.Contains(sql, "ORDER BY") {
		test.Errorf("sql missing ORDER BY: %s", sql)
	}

	if !strings.Contains(sql, "DESC") {
		test.Errorf("sql missing DESC: %s", sql)
	}
}

func TestCompile_TakeAndSkip(test *testing.T) {
	expr, _ := filter.NewParser("type=ticket").Parse()

	sql, _, _ := filter.Compile(expr, filter.CompileOptions{Take: 25, Skip: 50})

	if !strings.Contains(sql, "LIMIT 25") || !strings.Contains(sql, "OFFSET 50") {
		test.Errorf("sql missing LIMIT/OFFSET: %s", sql)
	}
}

func TestCompile_EdgeProbe(test *testing.T) {
	expr, _ := filter.NewParser("blocks->").Parse()

	sql, params, _ := filter.Compile(expr, filter.CompileOptions{})

	if !strings.Contains(sql, "EXISTS") || !strings.Contains(sql, "edges") {
		test.Errorf("expected EXISTS over edges: %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"blocks"}) {
		test.Errorf("params = %v, want [blocks]", params)
	}
}

func TestCompile_EdgeIncomingProbe(test *testing.T) {
	expr, _ := filter.NewParser("blocks<-").Parse()

	sql, _, _ := filter.Compile(expr, filter.CompileOptions{})

	if !strings.Contains(sql, "target_id = nodes.id") {
		test.Errorf("expected target_id = nodes.id for incoming probe: %s", sql)
	}
}

func TestCompile_EdgePredicate(test *testing.T) {
	expr, _ := filter.NewParser("blocks->status=active").Parse()

	sql, params, _ := filter.Compile(expr, filter.CompileOptions{})

	if !strings.Contains(sql, "JOIN nodes") {
		test.Errorf("expected JOIN nodes for edge predicate: %s", sql)
	}

	wantParams := []any{"blocks", "active"}
	if !reflect.DeepEqual(params, wantParams) {
		test.Errorf("params = %v, want %v", params, wantParams)
	}
}

func TestCompile_MultiHopChain(test *testing.T) {
	expr, _ := filter.NewParser(`parent->parent->name="auth"`).Parse()

	sql, params, _ := filter.Compile(expr, filter.CompileOptions{})

	if strings.Count(sql, "EXISTS") < 2 {
		test.Errorf("expected ≥2 EXISTS for 2-hop chain: %s", sql)
	}

	wantParams := []any{"parent", "parent", "auth"}
	if !reflect.DeepEqual(params, wantParams) {
		test.Errorf("params = %v, want %v", params, wantParams)
	}
}

func TestCompile_TraversalShortcutParent(test *testing.T) {
	expr := &filter.TraversalShortcut{Kind: filter.ShortcutParentOf, NodeID: "tickets/foo", EdgeType: "parent"}

	sql, params, _ := filter.Compile(expr, filter.CompileOptions{})

	if !strings.Contains(sql, "EXISTS") || !strings.Contains(sql, "type = ?") {
		test.Errorf("sql for parent= shortcut missing EXISTS or parameterized type: %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"parent", "tickets/foo"}) {
		test.Errorf("params = %v, want [parent tickets/foo]", params)
	}
}

func TestCompile_TraversalShortcutTreeUsesRecursiveCTE(test *testing.T) {
	expr := &filter.TraversalShortcut{Kind: filter.ShortcutTree, NodeID: "tickets/foo", EdgeType: "parent"}

	sql, params, _ := filter.Compile(expr, filter.CompileOptions{})

	if !strings.Contains(sql, "WITH RECURSIVE") {
		test.Errorf("expected recursive CTE for tree=: %s", sql)
	}

	if !strings.Contains(sql, "depth < 5") {
		test.Errorf("expected depth bound of 5: %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"tickets/foo", "parent", "parent"}) {
		test.Errorf("params = %v, want [tickets/foo parent parent]", params)
	}
}

func TestCompile_TraversalShortcutRoot(test *testing.T) {
	expr := &filter.TraversalShortcut{Kind: filter.ShortcutRoot, NodeID: "tickets/foo", EdgeType: "parent"}

	sql, _, _ := filter.Compile(expr, filter.CompileOptions{})

	if strings.Count(sql, "WITH RECURSIVE") == 0 && strings.Count(sql, "ascendants") == 0 {
		test.Errorf("expected recursive CTE structure for root=: %s", sql)
	}
}

func TestCompile_TreeShortcutUsesResolvedEdgeType(test *testing.T) {
	expr := &filter.TraversalShortcut{
		Kind:     filter.ShortcutTree,
		NodeID:   "wbs/root",
		EdgeType: "wbs-parent",
	}

	sql, params, err := filter.Compile(expr, filter.CompileOptions{})

	if err != nil {
		test.Fatalf("Compile: %v", err)
	}

	if strings.Contains(sql, "type = 'parent'") {
		test.Errorf("SQL still hardcodes 'parent': %s", sql)
	}

	if !strings.Contains(sql, "type = ?") {
		test.Errorf("SQL missing parameterized edge type: %s", sql)
	}

	foundEdgeParam := false

	for _, param := range params {
		if str, ok := param.(string); ok && str == "wbs-parent" {
			foundEdgeParam = true

			break
		}
	}

	if !foundEdgeParam {
		test.Errorf("params %v missing edge type 'wbs-parent'", params)
	}
}

func TestCompile_ShortcutWithEmptyEdgeTypeErrors(test *testing.T) {
	expr := &filter.TraversalShortcut{
		Kind:   filter.ShortcutTree,
		NodeID: "x",
		// EdgeType deliberately empty
	}

	_, _, err := filter.Compile(expr, filter.CompileOptions{})

	if err == nil {
		test.Fatalf("expected error for shortcut without resolved edge type")
	}

	if !strings.Contains(err.Error(), "unresolved") {
		test.Errorf("error = %q, want substring %q", err.Error(), "unresolved")
	}
}
