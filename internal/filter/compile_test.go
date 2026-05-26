package filter_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/manifest"
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

func TestPipeline_TwoHierarchiesProduceDistinctSQL(test *testing.T) {
	manifestObj := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"parent": {
				Cardinality:      manifest.CardinalityManyToOne,
				Acyclic:          true,
				Hierarchy:        "kanban",
				HierarchyDefault: true,
			},
			"wbs-parent": {
				Cardinality: manifest.CardinalityManyToOne,
				Acyclic:     true,
				Hierarchy:   "wbs",
			},
		},
	}

	cases := []struct {
		name            string
		query           string
		wantEdgeInParam string
		notEdgeInParam  string
	}{
		{"qualified_kanban", "tree:kanban=epic", "parent", "wbs-parent"},
		{"qualified_wbs", "tree:wbs=initiative", "wbs-parent", "parent"},
		{"unqualified_uses_default", "tree=epic", "parent", "wbs-parent"},
		{"parent_qualified_wbs", "parent:wbs=initiative", "wbs-parent", "parent"},
		{"root_qualified_kanban", "root:kanban=story", "parent", "wbs-parent"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			parser := filter.NewParser(testCase.query)
			expr, parseErrs := parser.Parse()

			if len(parseErrs) != 0 {
				test.Fatalf("parse %q: %+v", testCase.query, parseErrs)
			}

			validateErrs := filter.Validate(expr, manifestObj)

			if len(validateErrs) != 0 {
				test.Fatalf("validate %q: %+v", testCase.query, validateErrs)
			}

			sql, params, err := filter.Compile(expr, filter.CompileOptions{})

			if err != nil {
				test.Fatalf("compile %q: %v", testCase.query, err)
			}

			if strings.Contains(sql, "type = 'parent'") {
				test.Errorf("query %q: SQL still hardcodes 'parent': %s", testCase.query, sql)
			}

			foundWanted := false
			foundUnwanted := false

			for _, param := range params {
				str, isStr := param.(string)

				if !isStr {
					continue
				}

				if str == testCase.wantEdgeInParam {
					foundWanted = true
				}

				if str == testCase.notEdgeInParam {
					foundUnwanted = true
				}
			}

			if !foundWanted {
				test.Errorf("query %q: expected %q in params, got %v", testCase.query, testCase.wantEdgeInParam, params)
			}

			if foundUnwanted {
				test.Errorf("query %q: did not expect %q in params, got %v", testCase.query, testCase.notEdgeInParam, params)
			}
		})
	}
}

func TestCompile_TraversalSortsByOrderedByProperty(test *testing.T) {
	manifestObj := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"wbs-parent": {
				Cardinality: manifest.CardinalityManyToOne,
				Acyclic:     true,
				Hierarchy:   "wbs",
				Ordered:     true,
				OrderedBy:   "order",
			},
		},
	}

	ast := &filter.TraversalShortcut{
		Kind:   filter.ShortcutParentOf,
		Alias:  "wbs",
		NodeID: "wbs/proj",
	}

	validateErrs := filter.Validate(ast, manifestObj)

	if len(validateErrs) != 0 {
		test.Fatalf("validate: %+v", validateErrs)
	}

	if ast.EdgeType != "wbs-parent" {
		test.Fatalf("expected EdgeType=wbs-parent, got %q", ast.EdgeType)
	}

	if ast.OrderedBy != "order" {
		test.Fatalf("expected OrderedBy=order, got %q", ast.OrderedBy)
	}

	sql, _, compileErr := filter.Compile(ast, filter.CompileOptions{})

	if compileErr != nil {
		test.Fatalf("compile: %v", compileErr)
	}

	if !strings.Contains(sql, "ORDER BY") {
		test.Errorf("expected ORDER BY clause, got: %s", sql)
	}

	if !strings.Contains(sql, `COALESCE(json_extract(nodes.properties_json, '$."order"'), 0)`) {
		test.Errorf("expected COALESCE json_extract sort on order property, got: %s", sql)
	}

	if !strings.Contains(sql, "nodes.id") {
		test.Errorf("expected tiebreak on nodes.id, got: %s", sql)
	}
}

func TestCompile_TraversalDefaultSortLosesToExplicitSortKeys(test *testing.T) {
	manifestObj := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"wbs-parent": {
				Cardinality: manifest.CardinalityManyToOne,
				Acyclic:     true,
				Hierarchy:   "wbs",
				Ordered:     true,
				OrderedBy:   "order",
			},
		},
	}

	ast := &filter.TraversalShortcut{
		Kind:   filter.ShortcutParentOf,
		Alias:  "wbs",
		NodeID: "wbs/proj",
	}

	validateErrs := filter.Validate(ast, manifestObj)

	if len(validateErrs) != 0 {
		test.Fatalf("validate: %+v", validateErrs)
	}

	sql, _, compileErr := filter.Compile(ast, filter.CompileOptions{
		SortKeys: []filter.SortKey{{Property: "title"}},
	})

	if compileErr != nil {
		test.Fatalf("compile: %v", compileErr)
	}

	if strings.Contains(sql, "COALESCE") {
		test.Errorf("explicit --sort should suppress traversal default ORDER BY, got: %s", sql)
	}

	if !strings.Contains(sql, "ORDER BY title ASC") {
		test.Errorf("expected explicit ORDER BY title ASC, got: %s", sql)
	}
}

func TestCompile_TraversalWithoutOrderedByHasNoDefaultSort(test *testing.T) {
	manifestObj := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"wbs-parent": {
				Cardinality: manifest.CardinalityManyToOne,
				Acyclic:     true,
				Hierarchy:   "wbs",
			},
		},
	}

	ast := &filter.TraversalShortcut{
		Kind:   filter.ShortcutParentOf,
		Alias:  "wbs",
		NodeID: "wbs/proj",
	}

	validateErrs := filter.Validate(ast, manifestObj)

	if len(validateErrs) != 0 {
		test.Fatalf("validate: %+v", validateErrs)
	}

	if ast.OrderedBy != "" {
		test.Errorf("expected empty OrderedBy, got %q", ast.OrderedBy)
	}

	sql, _, compileErr := filter.Compile(ast, filter.CompileOptions{})

	if compileErr != nil {
		test.Fatalf("compile: %v", compileErr)
	}

	if strings.Contains(sql, "ORDER BY") {
		test.Errorf("expected no ORDER BY when OrderedBy is empty and no --sort, got: %s", sql)
	}
}

func TestCompile_TraversalMultiShortcutLeftmostOrderedByWins(test *testing.T) {
	// Two hierarchy edge types, each with its own OrderedBy:
	//   wbs-parent    → ordered = "wbs-order"
	//   kanban-parent → ordered = "kanban-order"
	// Compile: tree:wbs=X AND tree:kanban=Y
	// Assert SQL contains "wbs-order" (the leftmost), not "kanban-order".
	manifestObj := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"wbs-parent": {
				Cardinality: manifest.CardinalityManyToOne,
				Acyclic:     true,
				Hierarchy:   "wbs",
				Ordered:     true,
				OrderedBy:   "wbs-order",
			},
			"kanban-parent": {
				Cardinality: manifest.CardinalityManyToOne,
				Acyclic:     true,
				Hierarchy:   "kanban",
				Ordered:     true,
				OrderedBy:   "kanban-order",
			},
		},
	}

	ast := &filter.AndExpr{
		Left: &filter.TraversalShortcut{
			Kind:   filter.ShortcutTree,
			Alias:  "wbs",
			NodeID: "things/x",
		},
		Right: &filter.TraversalShortcut{
			Kind:   filter.ShortcutTree,
			Alias:  "kanban",
			NodeID: "things/y",
		},
	}

	if validateErrs := filter.Validate(ast, manifestObj); len(validateErrs) != 0 {
		test.Fatalf("validate: %+v", validateErrs)
	}

	sql, _, compileErr := filter.Compile(ast, filter.CompileOptions{})

	if compileErr != nil {
		test.Fatalf("compile: %v", compileErr)
	}

	if !strings.Contains(sql, "wbs-order") {
		test.Errorf("expected wbs-order (leftmost) in SQL, got:\n%s", sql)
	}

	if strings.Contains(sql, "kanban-order") {
		test.Errorf("kanban-order should NOT appear; wbs-order wins as leftmost. SQL:\n%s", sql)
	}
}

func TestPipeline_AmbiguousUnqualifiedFailsValidation(test *testing.T) {
	manifestObj := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"parent": {
				Cardinality: manifest.CardinalityManyToOne,
				Hierarchy:   "kanban",
			},
			"wbs-parent": {
				Cardinality: manifest.CardinalityManyToOne,
				Hierarchy:   "wbs",
			},
		},
	}

	parser := filter.NewParser("tree=epic")
	expr, parseErrs := parser.Parse()

	if len(parseErrs) != 0 {
		test.Fatalf("unexpected parse errors: %+v", parseErrs)
	}

	validateErrs := filter.Validate(expr, manifestObj)

	if len(validateErrs) == 0 {
		test.Fatalf("expected validation error for ambiguous unqualified shortcut")
	}

	if !strings.Contains(validateErrs[0].Message, "no default hierarchy") {
		test.Errorf("unexpected message: %q", validateErrs[0].Message)
	}
}

func TestCompile_ModifiedSinceDurationEmitsLastMtimeBound(test *testing.T) {
	expr, parseErrs := filter.NewParser("modified-since:7d").Parse()

	if len(parseErrs) != 0 {
		test.Fatalf("parse: %+v", parseErrs)
	}

	validateErrs := filter.Validate(expr, manifest.Manifest{})

	if len(validateErrs) != 0 {
		test.Fatalf("validate: %+v", validateErrs)
	}

	before := time.Now().Add(-7 * 24 * time.Hour).UnixNano()

	sql, params, err := filter.Compile(expr, filter.CompileOptions{})

	after := time.Now().Add(-7 * 24 * time.Hour).UnixNano()

	if err != nil {
		test.Fatalf("compile: %v", err)
	}

	if !strings.Contains(sql, "last_mtime >= ?") {
		test.Errorf("sql missing last_mtime >= ?: %s", sql)
	}

	if len(params) != 1 {
		test.Fatalf("params = %v, want exactly one element", params)
	}

	got, ok := params[0].(int64)

	if !ok {
		test.Fatalf("param[0] = %T %v, want int64", params[0], params[0])
	}

	if got < before || got > after {
		test.Errorf("param[0] = %d, want within [%d, %d]", got, before, after)
	}
}

func TestCompile_ModifiedSinceAbsoluteDateExactNanos(test *testing.T) {
	expr, parseErrs := filter.NewParser("modified-since:2026-05-23T12:00:00Z").Parse()

	if len(parseErrs) != 0 {
		test.Fatalf("parse: %+v", parseErrs)
	}

	if validateErrs := filter.Validate(expr, manifest.Manifest{}); len(validateErrs) != 0 {
		test.Fatalf("validate: %+v", validateErrs)
	}

	sql, params, err := filter.Compile(expr, filter.CompileOptions{})

	if err != nil {
		test.Fatalf("compile: %v", err)
	}

	if !strings.Contains(sql, "last_mtime >= ?") {
		test.Errorf("sql missing last_mtime >= ?: %s", sql)
	}

	want := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC).UnixNano()

	if len(params) != 1 || params[0] != want {
		test.Errorf("params = %v, want [%d]", params, want)
	}
}

func TestCompile_ModifiedSinceUnresolvedErrors(test *testing.T) {
	// Hand-construct an AST with neither Duration nor Since set, mirroring
	// the TraversalShortcut.EdgeType empty-value guard.
	expr := &filter.ModifiedSincePredicate{Raw: "7d"}

	_, _, err := filter.Compile(expr, filter.CompileOptions{})

	if err == nil {
		test.Fatalf("expected error for unresolved modified-since")
	}

	if !strings.Contains(err.Error(), "unresolved") {
		test.Errorf("error = %q, want substring %q", err.Error(), "unresolved")
	}
}

func TestCompile_ModifiedSinceComposesWithOtherPredicates(test *testing.T) {
	expr, parseErrs := filter.NewParser("type=ticket AND modified-since:7d").Parse()

	if len(parseErrs) != 0 {
		test.Fatalf("parse: %+v", parseErrs)
	}

	if validateErrs := filter.Validate(expr, manifest.Manifest{}); len(validateErrs) != 0 {
		test.Fatalf("validate: %+v", validateErrs)
	}

	sql, params, err := filter.Compile(expr, filter.CompileOptions{})

	if err != nil {
		test.Fatalf("compile: %v", err)
	}

	if !strings.Contains(sql, "type = ?") || !strings.Contains(sql, "last_mtime >= ?") {
		test.Errorf("sql missing both predicates: %s", sql)
	}

	if !strings.Contains(sql, " AND ") {
		test.Errorf("sql missing AND join: %s", sql)
	}

	if len(params) != 2 {
		test.Fatalf("params = %v, want 2 entries", params)
	}

	if params[0] != "ticket" {
		test.Errorf("params[0] = %v, want \"ticket\"", params[0])
	}

	if _, ok := params[1].(int64); !ok {
		test.Errorf("params[1] = %T, want int64", params[1])
	}
}

// TestCompile_SubUnitTypePredicate confirms that a `type=<sub-unit>`
// predicate parses and compiles to the same SQL shape as any other core
// column equality. Sub-unit type names are registered by the
// sub-document pack (Task 1); the compiler doesn't care about the
// taxonomy. Regression test for Task 5 (Phase 2).
func TestCompile_SubUnitTypePredicate(test *testing.T) {
	for _, typeName := range []string{"section", "paragraph", "list-item", "code-block", "blockquote", "table-cell"} {
		expr, parseErrs := filter.NewParser("type=" + typeName).Parse()

		if len(parseErrs) != 0 {
			test.Fatalf("parse %s: %v", typeName, parseErrs)
		}

		sqlText, params, compileErr := filter.Compile(expr, filter.CompileOptions{})

		if compileErr != nil {
			test.Fatalf("compile %s: %v", typeName, compileErr)
		}

		if !strings.Contains(sqlText, "type = ?") {
			test.Errorf("%s: sql missing core column compare: %s", typeName, sqlText)
		}

		if !reflect.DeepEqual(params, []any{typeName}) {
			test.Errorf("%s: params = %v, want [%s]", typeName, params, typeName)
		}
	}
}

// TestCompile_BoolLiteralCoercedToInt confirms that bareword `true`/
// `false` values on a property predicate compile to a comparison
// against the integer 1/0, matching SQLite's json_extract surface for
// JSON booleans. Regression for Phase 2 P2 acceptance gap: `checkbox=
// false` previously compiled to `... = 'false'` and matched nothing.
func TestCompile_BoolLiteralCoercedToInt(test *testing.T) {
	cases := []struct {
		query   string
		wantInt int
	}{
		{"checkbox=false", 0},
		{"checkbox=true", 1},
		{"checkbox!=false", 0},
		{"checkbox!=true", 1},
	}

	for _, testCase := range cases {
		test.Run(testCase.query, func(test *testing.T) {
			expr, parseErrs := filter.NewParser(testCase.query).Parse()

			if len(parseErrs) != 0 {
				test.Fatalf("parse: %+v", parseErrs)
			}

			sqlText, params, compileErr := filter.Compile(expr, filter.CompileOptions{})

			if compileErr != nil {
				test.Fatalf("compile: %v", compileErr)
			}

			if !strings.Contains(sqlText, "json_extract(properties_json, '$.checkbox')") {
				test.Errorf("missing json_extract on checkbox: %s", sqlText)
			}

			if strings.Contains(sqlText, "CAST(") {
				test.Errorf("bool literal must not wrap json_extract in CAST: %s", sqlText)
			}

			if !reflect.DeepEqual(params, []any{testCase.wantInt}) {
				test.Errorf("params = %v, want [%d]", params, testCase.wantInt)
			}
		})
	}
}

// TestCompile_QuotedFalseIsStringLiteral verifies that quoting opts out
// of the bool coercion: `flag="false"` compares against the string
// "false", preserving the legacy behaviour for properties whose value
// is the literal word "false".
func TestCompile_QuotedFalseIsStringLiteral(test *testing.T) {
	expr, parseErrs := filter.NewParser(`flag="false"`).Parse()

	if len(parseErrs) != 0 {
		test.Fatalf("parse: %+v", parseErrs)
	}

	_, params, compileErr := filter.Compile(expr, filter.CompileOptions{})

	if compileErr != nil {
		test.Fatalf("compile: %v", compileErr)
	}

	if !reflect.DeepEqual(params, []any{"false"}) {
		test.Errorf("params = %v, want [\"false\"]", params)
	}
}

// TestCompile_BoolLiteralOnlyForEqualityOps confirms that bareword bool
// literals on non-`=`/`!=` operators fall through to the legacy string
// comparison path (numeric comparators on booleans are nonsensical, but
// must not panic or silently rewrite).
func TestCompile_BoolLiteralOnlyForEqualityOps(test *testing.T) {
	expr, parseErrs := filter.NewParser("checkbox>=false").Parse()

	if len(parseErrs) != 0 {
		test.Fatalf("parse: %+v", parseErrs)
	}

	_, params, compileErr := filter.Compile(expr, filter.CompileOptions{})

	if compileErr != nil {
		test.Fatalf("compile: %v", compileErr)
	}

	if !reflect.DeepEqual(params, []any{"false"}) {
		test.Errorf("params = %v, want [\"false\"] (non-equality op skips bool coercion)", params)
	}
}

// TestCompile_HeadingLevelPredicate covers the kebab-case heading-level
// property used by section sub-units. SQLite's JSON1 accepts both
// `$.heading-level` and `$."heading-level"`; the compiler emits the
// unquoted form, which works in modern SQLite.
func TestCompile_HeadingLevelPredicate(test *testing.T) {
	expr, parseErrs := filter.NewParser("type=section AND heading-level<=2").Parse()

	if len(parseErrs) != 0 {
		test.Fatalf("parse: %v", parseErrs)
	}

	sqlText, _, compileErr := filter.Compile(expr, filter.CompileOptions{})

	if compileErr != nil {
		test.Fatalf("compile: %v", compileErr)
	}

	if !strings.Contains(sqlText, "type = ?") {
		test.Errorf("missing type predicate: %s", sqlText)
	}

	if !strings.Contains(sqlText, "heading-level") {
		test.Errorf("missing heading-level extract: %s", sqlText)
	}

	if !strings.Contains(sqlText, "<= ?") {
		test.Errorf("missing <= operator: %s", sqlText)
	}
}

func TestCompileNodeTypeRefScopes(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		input      string
		wantClause string
		wantParams []any
	}{
		{
			name:       "bare type",
			input:      "type=section",
			wantClause: "type = ?",
			wantParams: []any{"section"},
		},
		{
			name:       "user-namespace type",
			input:      "type=:section",
			wantClause: "source IS NULL AND type = ?",
			wantParams: []any{"section"},
		},
		{
			name:       "source-qualified type",
			input:      "type=markdown:section",
			wantClause: "source = ? AND type = ?",
			wantParams: []any{"markdown", "section"},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			expr, parseErrs := filter.NewParser(testCase.input).Parse()

			if len(parseErrs) > 0 {
				test.Fatalf("parse: %v", parseErrs[0])
			}

			sqlText, params, compileErr := filter.Compile(expr, filter.CompileOptions{})

			if compileErr != nil {
				test.Fatalf("compile: %v", compileErr)
			}

			if !strings.Contains(sqlText, testCase.wantClause) {
				test.Errorf("sql missing %q\ngot: %s", testCase.wantClause, sqlText)
			}

			if !reflect.DeepEqual(params, testCase.wantParams) {
				test.Errorf("params = %v, want %v", params, testCase.wantParams)
			}
		})
	}
}

// TestCompileNodeTypeRefScopesInsideEdgePredicate confirms scope-aware
// node-type compilation also fires for type predicates nested inside an
// edge predicate (e.g. `references->type=:section`). The edge type
// itself remains bare here because qualified-edge-type syntax in
// filter expressions requires parser/lexer extensions not in scope for
// this task — see Phase 5 follow-up.
func TestCompileNodeTypeRefScopesInsideEdgePredicate(test *testing.T) {
	test.Parallel()

	loaded := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"references": {Cardinality: manifest.CardinalityOneToMany},
		},
	}

	cases := []struct {
		name       string
		input      string
		wantClause string
	}{
		{
			name:       "bare inner type",
			input:      "references->type=section",
			wantClause: "n0.type = ?",
		},
		{
			name:       "user-namespace inner type",
			input:      "references->type=:section",
			wantClause: "n0.source IS NULL AND n0.type = ?",
		},
		{
			name:       "source-qualified inner type",
			input:      "references->type=markdown:section",
			wantClause: "n0.source = ? AND n0.type = ?",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			expr, parseErrs := filter.NewParser(testCase.input).Parse()

			if len(parseErrs) > 0 {
				test.Fatalf("parse: %v", parseErrs[0])
			}

			if validateErrs := filter.Validate(expr, loaded); len(validateErrs) > 0 {
				test.Fatalf("validate: %v", validateErrs[0])
			}

			sqlText, _, compileErr := filter.Compile(expr, filter.CompileOptions{})

			if compileErr != nil {
				test.Fatalf("compile: %v", compileErr)
			}

			if !strings.Contains(sqlText, testCase.wantClause) {
				test.Errorf("sql missing %q\ngot: %s", testCase.wantClause, sqlText)
			}
		})
	}
}
