package filter_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/filter"
)

func TestParser_PropertyEquality(test *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"equals", "type=ticket"},
		{"colon", "type:ticket"},
	}

	for _, tc := range cases {
		test.Run(tc.name, func(test *testing.T) {
			expr, errs := filter.NewParser(tc.input).Parse()

			if len(errs) > 0 {
				test.Fatalf("errors: %v", errs)
			}

			pred, ok := expr.(*filter.PropertyPredicate)

			if !ok {
				test.Fatalf("got %T, want *PropertyPredicate", expr)
			}

			if pred.Property != "type" || pred.Op != filter.OpEQ {
				test.Errorf("got property=%q op=%v, want type=", pred.Property, pred.Op)
			}

			if str, isString := pred.Value.(filter.StringValue); !isString || str.V != "ticket" {
				test.Errorf("got value=%v, want StringValue{ticket}", pred.Value)
			}
		})
	}
}

func TestParser_ColonValueWithEmbeddedColon(test *testing.T) {
	expr, errs := filter.NewParser("id:notes/foo:bar").Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	pred := expr.(*filter.PropertyPredicate)

	if str := pred.Value.(filter.StringValue).V; str != "notes/foo:bar" {
		test.Errorf("value=%q, want %q", str, "notes/foo:bar")
	}
}

func TestParser_ColonTraversalShortcut(test *testing.T) {
	expr, errs := filter.NewParser("tree:wbs/proj").Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	shortcut, ok := expr.(*filter.TraversalShortcut)

	if !ok {
		test.Fatalf("got %T, want *TraversalShortcut", expr)
	}

	if shortcut.NodeID != "wbs/proj" {
		test.Errorf("NodeID=%q, want %q", shortcut.NodeID, "wbs/proj")
	}
}

func TestParser_PropertyComparators(test *testing.T) {
	cases := []struct {
		input string
		op    filter.Op
		value string
	}{
		{"priority>=3", filter.OpGE, "3"},
		{"priority<3", filter.OpLT, "3"},
		{"priority<=3", filter.OpLE, "3"},
		{"priority>3", filter.OpGT, "3"},
		{"priority!=3", filter.OpNE, "3"},
	}

	for _, tc := range cases {
		expr, errs := filter.NewParser(tc.input).Parse()

		if len(errs) > 0 {
			test.Fatalf("input %q: errors: %v", tc.input, errs)
		}

		pred := expr.(*filter.PropertyPredicate)

		if pred.Op != tc.op {
			test.Errorf("input %q: op=%v, want %v", tc.input, pred.Op, tc.op)
		}

		if str := pred.Value.(filter.StringValue).V; str != tc.value {
			test.Errorf("input %q: value=%q, want %q", tc.input, str, tc.value)
		}
	}
}

func TestParser_PropertyRange(test *testing.T) {
	expr, errs := filter.NewParser("priority=2..4").Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	pred := expr.(*filter.PropertyPredicate)

	if pred.Op != filter.OpRange {
		test.Errorf("op = %v, want OpRange", pred.Op)
	}

	rangeValue, ok := pred.Value.(filter.RangeValue)

	if !ok {
		test.Fatalf("value type %T, want RangeValue", pred.Value)
	}

	if rangeValue.Min != "2" || rangeValue.Max != "4" {
		test.Errorf("got range %v..%v, want 2..4", rangeValue.Min, rangeValue.Max)
	}
}

func TestParser_QuotedStringValue(test *testing.T) {
	expr, _ := filter.NewParser(`title="Auth bug"`).Parse()
	pred := expr.(*filter.PropertyPredicate)

	if pred.Value.(filter.StringValue).V != "Auth bug" {
		test.Errorf("got %q, want \"Auth bug\"", pred.Value.(filter.StringValue).V)
	}
}

func TestParser_TraversalShortcut(test *testing.T) {
	cases := []struct {
		input string
		kind  filter.ShortcutKind
		id    string
	}{
		{"tree=tickets/foo", filter.ShortcutTree, "tickets/foo"},
		{"parent=tickets/foo", filter.ShortcutParentOf, "tickets/foo"},
		{"root=tickets/foo", filter.ShortcutRoot, "tickets/foo"},
	}

	for _, tc := range cases {
		expr, errs := filter.NewParser(tc.input).Parse()

		if len(errs) > 0 {
			test.Fatalf("input %q: errors %v", tc.input, errs)
		}

		shortcut, ok := expr.(*filter.TraversalShortcut)

		if !ok {
			test.Fatalf("input %q: type %T, want *TraversalShortcut", tc.input, expr)
		}

		if shortcut.Kind != tc.kind || shortcut.NodeID != tc.id {
			test.Errorf("input %q: got %+v, want kind=%v id=%q", tc.input, shortcut, tc.kind, tc.id)
		}
	}
}

func TestParser_EmptyInputAcceptedAsTrue(test *testing.T) {
	expr, errs := filter.NewParser("").Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	if expr != nil {
		test.Errorf("expected nil expression for empty input, got %T", expr)
	}
}

func TestParser_EdgeProbe(test *testing.T) {
	expr, errs := filter.NewParser("blocks->").Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	pred, ok := expr.(*filter.EdgePredicate)

	if !ok {
		test.Fatalf("got %T, want *EdgePredicate", expr)
	}

	if pred.EdgeType != "blocks" || pred.Direction != filter.DirectionOutgoing || pred.Inner != nil {
		test.Errorf("got %+v", pred)
	}
}

func TestParser_EdgeIncomingProbe(test *testing.T) {
	expr, _ := filter.NewParser("blocks<-").Parse()
	pred := expr.(*filter.EdgePredicate)

	if pred.Direction != filter.DirectionIncoming {
		test.Errorf("expected incoming")
	}
}

func TestParser_EdgePredicate(test *testing.T) {
	expr, errs := filter.NewParser("blocks->status=active").Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	outer := expr.(*filter.EdgePredicate)

	if outer.EdgeType != "blocks" || outer.Direction != filter.DirectionOutgoing {
		test.Errorf("outer = %+v", outer)
	}

	inner := outer.Inner.(*filter.PropertyPredicate)

	if inner.Property != "status" || inner.Value.(filter.StringValue).V != "active" {
		test.Errorf("inner = %+v", inner)
	}
}

func TestParser_MultiHopChain(test *testing.T) {
	expr, errs := filter.NewParser(`parent->parent->name="auth"`).Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	hop1 := expr.(*filter.EdgePredicate)
	hop2 := hop1.Inner.(*filter.EdgePredicate)
	leaf := hop2.Inner.(*filter.PropertyPredicate)

	if hop1.EdgeType != "parent" || hop2.EdgeType != "parent" || leaf.Property != "name" {
		test.Errorf("got hop1=%+v hop2=%+v leaf=%+v", hop1, hop2, leaf)
	}

	if leaf.Value.(filter.StringValue).V != "auth" {
		test.Errorf("leaf value = %q", leaf.Value.(filter.StringValue).V)
	}
}

func TestParser_MultiHopExceedsMaxDepth(test *testing.T) {
	input := "parent->parent->parent->parent->parent->parent->name=x"

	_, errs := filter.NewParser(input).Parse()

	if len(errs) == 0 {
		test.Fatalf("expected error for depth > 5")
	}
}

func TestParser_ExplicitAnd(test *testing.T) {
	expr, errs := filter.NewParser("type=ticket AND status=active").Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	andExpr, ok := expr.(*filter.AndExpr)

	if !ok {
		test.Fatalf("got %T, want *AndExpr", expr)
	}

	left := andExpr.Left.(*filter.PropertyPredicate)
	right := andExpr.Right.(*filter.PropertyPredicate)

	if left.Property != "type" || right.Property != "status" {
		test.Errorf("got left=%+v right=%+v", left, right)
	}
}

func TestParser_ImplicitAnd(test *testing.T) {
	expr, errs := filter.NewParser("type=ticket status=active priority>=3").Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	outer := expr.(*filter.AndExpr)
	inner := outer.Left.(*filter.AndExpr)

	if inner.Left.(*filter.PropertyPredicate).Property != "type" {
		test.Errorf("inner.left = %+v", inner.Left)
	}

	if inner.Right.(*filter.PropertyPredicate).Property != "status" {
		test.Errorf("inner.right = %+v", inner.Right)
	}

	if outer.Right.(*filter.PropertyPredicate).Property != "priority" {
		test.Errorf("outer.right = %+v", outer.Right)
	}
}

func TestParser_Or(test *testing.T) {
	expr, _ := filter.NewParser("type=ticket OR type=note").Parse()
	orExpr := expr.(*filter.OrExpr)

	if orExpr.Left.(*filter.PropertyPredicate).Property != "type" {
		test.Errorf("left = %+v", orExpr.Left)
	}
}

func TestParser_Not(test *testing.T) {
	expr, _ := filter.NewParser("NOT status=completed").Parse()
	notExpr := expr.(*filter.NotExpr)

	if notExpr.Inner.(*filter.PropertyPredicate).Property != "status" {
		test.Errorf("inner = %+v", notExpr.Inner)
	}
}

func TestParser_Parens(test *testing.T) {
	expr, _ := filter.NewParser("(type=ticket OR type=note) AND status=active").Parse()

	andExpr := expr.(*filter.AndExpr)
	_ = andExpr.Left.(*filter.OrExpr)
}

func TestParser_Precedence(test *testing.T) {
	expr, _ := filter.NewParser("type=ticket AND status=active OR type=note").Parse()
	orExpr := expr.(*filter.OrExpr)
	_ = orExpr.Left.(*filter.AndExpr)
	_ = orExpr.Right.(*filter.PropertyPredicate)
}

func TestParser_QualifiedTreeShortcut(test *testing.T) {
	parser := filter.NewParser("tree:wbs=wbs/root")

	expr, errs := parser.Parse()

	if len(errs) != 0 {
		test.Fatalf("unexpected parse errors: %+v", errs)
	}

	shortcut, ok := expr.(*filter.TraversalShortcut)

	if !ok {
		test.Fatalf("expr = %T, want *TraversalShortcut", expr)
	}

	if shortcut.Kind != filter.ShortcutTree {
		test.Errorf("Kind = %v, want ShortcutTree", shortcut.Kind)
	}

	if shortcut.Alias != "wbs" {
		test.Errorf("Alias = %q, want %q", shortcut.Alias, "wbs")
	}

	if shortcut.NodeID != "wbs/root" {
		test.Errorf("NodeID = %q, want %q", shortcut.NodeID, "wbs/root")
	}
}

func TestParser_QualifiedParentAndRootShortcuts(test *testing.T) {
	cases := []struct {
		input string
		kind  filter.ShortcutKind
		alias string
		id    string
	}{
		{"parent:kanban=task/123", filter.ShortcutParentOf, "kanban", "task/123"},
		{"root:wbs=wbs/leaf", filter.ShortcutRoot, "wbs", "wbs/leaf"},
	}

	for _, testCase := range cases {
		parser := filter.NewParser(testCase.input)
		expr, errs := parser.Parse()

		if len(errs) != 0 {
			test.Fatalf("input %q: unexpected errors %+v", testCase.input, errs)
		}

		shortcut, ok := expr.(*filter.TraversalShortcut)

		if !ok {
			test.Fatalf("input %q: expr = %T, want *TraversalShortcut", testCase.input, expr)
		}

		if shortcut.Kind != testCase.kind {
			test.Errorf("input %q: Kind = %v, want %v", testCase.input, shortcut.Kind, testCase.kind)
		}

		if shortcut.Alias != testCase.alias {
			test.Errorf("input %q: Alias = %q, want %q", testCase.input, shortcut.Alias, testCase.alias)
		}

		if shortcut.NodeID != testCase.id {
			test.Errorf("input %q: NodeID = %q, want %q", testCase.input, shortcut.NodeID, testCase.id)
		}
	}
}

func TestParser_UnqualifiedShortcutStillParses(test *testing.T) {
	parser := filter.NewParser("tree=root/node")
	expr, errs := parser.Parse()

	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %+v", errs)
	}

	shortcut := expr.(*filter.TraversalShortcut)

	if shortcut.Alias != "" {
		test.Errorf("Alias = %q, want empty", shortcut.Alias)
	}

	if shortcut.NodeID != "root/node" {
		test.Errorf("NodeID = %q, want %q", shortcut.NodeID, "root/node")
	}
}

func TestParser_MalformedQualifiedShortcut(test *testing.T) {
	cases := []string{
		"tree:=foo",      // empty alias / missing value
		"tree:wbs:x=foo", // colon-after-alias not allowed
	}

	for _, input := range cases {
		parser := filter.NewParser(input)
		_, errs := parser.Parse()

		if len(errs) == 0 {
			test.Errorf("input %q: expected parse errors, got none", input)
		}
	}
}

func TestParser_ColonShorthandStillWorks(test *testing.T) {
	// Pre-existing behavior: `tree:foo` is the colon-as-equals shorthand,
	// meaning `tree=foo`. Should parse with Alias = "" and NodeID = "foo".
	parser := filter.NewParser("tree:foo/bar")

	expr, errs := parser.Parse()

	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %+v", errs)
	}

	shortcut := expr.(*filter.TraversalShortcut)

	if shortcut.Alias != "" {
		test.Errorf("Alias = %q, want empty (colon-shorthand)", shortcut.Alias)
	}

	if shortcut.NodeID != "foo/bar" {
		test.Errorf("NodeID = %q, want %q", shortcut.NodeID, "foo/bar")
	}
}
