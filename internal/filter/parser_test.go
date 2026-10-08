package filter_test

import (
	"fmt"
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

// TestParser_MultiHopDepthCountsGroups pins that hops nested inside a group
// count toward the same limit as a bare chain, so parentheses can't be used to
// stack traversals past it. The over-limit case also guards against the parser
// looping forever when the rejected hop is left unconsumed inside a group.
func TestParser_MultiHopDepthCountsGroups(test *testing.T) {
	if _, errs := filter.NewParser("a->(a->(a->(a->(a->x=1))))").Parse(); len(errs) > 0 {
		test.Fatalf("five grouped hops: unexpected errors %v", errs)
	}

	_, errs := filter.NewParser("a->(a->(a->(a->(a->(a->x=1)))))").Parse()

	if len(errs) == 0 || errs[0].Message != "multi-hop chain exceeds max depth 5" {
		test.Fatalf("six grouped hops: errs = %v, want the max-depth error", errs)
	}
}

// TestParser_EdgeInnerTerm pins that the term after an arrow parses with the
// full predicate grammar: a group, a NOT, a recency check, or a hierarchy
// shortcut constrains the edge's target instead of failing to parse, binding to
// the outer node, or being read as a property literally named after the
// keyword (#761).
func TestParser_EdgeInnerTerm(test *testing.T) {
	cases := []struct {
		name  string
		input string
		check func(inner filter.Expr) bool
	}{
		{"group", "blocks->(status=open OR status=wip)", func(inner filter.Expr) bool {
			_, ok := inner.(*filter.OrExpr)

			return ok
		}},
		{"NOT", "blocks-> NOT status=done", func(inner filter.Expr) bool {
			notExpr, ok := inner.(*filter.NotExpr)

			if !ok {
				return false
			}

			pred, isProperty := notExpr.Inner.(*filter.PropertyPredicate)

			return isProperty && pred.Property == "status"
		}},
		{"modified-since", "blocks-> modified-since:7d", func(inner filter.Expr) bool {
			pred, ok := inner.(*filter.ModifiedSincePredicate)

			return ok && pred.Raw == "7d"
		}},
		{"tree shortcut", "blocks-> tree=epic", func(inner filter.Expr) bool {
			shortcut, ok := inner.(*filter.TraversalShortcut)

			return ok && shortcut.Kind == filter.ShortcutTree && shortcut.NodeID == "epic"
		}},
		{"qualified parent shortcut", "blocks-> parent:wbs=epic", func(inner filter.Expr) bool {
			shortcut, ok := inner.(*filter.TraversalShortcut)

			return ok && shortcut.Kind == filter.ShortcutParentOf && shortcut.Alias == "wbs" && shortcut.NodeID == "epic"
		}},
		{"root shortcut", "blocks-> root=epic", func(inner filter.Expr) bool {
			shortcut, ok := inner.(*filter.TraversalShortcut)

			return ok && shortcut.Kind == filter.ShortcutRoot
		}},
		{"user-namespace traversal", "blocks-> :tagged-> type=tag", func(inner filter.Expr) bool {
			pred, ok := inner.(*filter.EdgePredicate)

			return ok && pred.EdgeType == ":tagged"
		}},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			expr, errs := filter.NewParser(testCase.input).Parse()

			if len(errs) > 0 {
				test.Fatalf("input %q: errors %v", testCase.input, errs)
			}

			pred, ok := expr.(*filter.EdgePredicate)

			if !ok {
				test.Fatalf("input %q: got %T, want *EdgePredicate", testCase.input, expr)
			}

			if !testCase.check(pred.Inner) {
				test.Errorf("input %q: inner = %#v", testCase.input, pred.Inner)
			}
		})
	}
}

// TestParser_EdgeInnerBindsOneTerm pins that without parentheses the arrow
// takes only the next term, the same way AND binds, so filters written before
// #761 keep their meaning: what follows that term applies to the outer node.
func TestParser_EdgeInnerBindsOneTerm(test *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantInner string // %T of the edge's Inner, or "<nil>" for a bare traversal
	}{
		{"property then property", "blocks-> status=open priority=high", "*filter.PropertyPredicate"},
		{"NOT term then property", "blocks-> NOT status=done priority=high", "*filter.NotExpr"},
		{"bare traversal then AND NOT", "blocks-> AND NOT status=done", "<nil>"},
		{"parenthesized bare traversal then NOT", "(blocks->) NOT status=done", "<nil>"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			expr, errs := filter.NewParser(testCase.input).Parse()

			if len(errs) > 0 {
				test.Fatalf("input %q: errors %v", testCase.input, errs)
			}

			andExpr, ok := expr.(*filter.AndExpr)

			if !ok {
				test.Fatalf("input %q: got %T, want *AndExpr", testCase.input, expr)
			}

			edge, ok := andExpr.Left.(*filter.EdgePredicate)

			if !ok {
				test.Fatalf("input %q: left = %T, want *EdgePredicate", testCase.input, andExpr.Left)
			}

			if got := fmt.Sprintf("%T", edge.Inner); got != testCase.wantInner {
				test.Errorf("input %q: inner = %s, want %s", testCase.input, got, testCase.wantInner)
			}

			if andExpr.Right == nil {
				test.Errorf("input %q: right side of AND is nil", testCase.input)
			}
		})
	}
}

// TestParser_EdgeInnerRejectsNonTerm pins that a token that can't start a term
// after the arrow is a parse error, not the end of the traversal.
func TestParser_EdgeInnerRejectsNonTerm(test *testing.T) {
	_, errs := filter.NewParser("blocks-> =x").Parse()

	if len(errs) == 0 || errs[0].Pos != 9 || errs[0].Message != "expected inner predicate or end of edge predicate" {
		test.Fatalf("errs = %v, want inner-predicate error at 9", errs)
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

func TestParser_ModifiedSincePredicate(test *testing.T) {
	cases := []struct {
		name  string
		input string
		raw   string
	}{
		{"duration_colon", "modified-since:7d", "7d"},
		{"duration_equals", "modified-since=48h", "48h"},
		{"date_colon", "modified-since:2026-05-23", "2026-05-23"},
		{"date_equals", "modified-since=2026-05-23T12:00:00Z", "2026-05-23T12:00:00Z"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			expr, errs := filter.NewParser(testCase.input).Parse()

			if len(errs) != 0 {
				test.Fatalf("unexpected parse errors: %+v", errs)
			}

			pred, ok := expr.(*filter.ModifiedSincePredicate)

			if !ok {
				test.Fatalf("expr = %T, want *ModifiedSincePredicate", expr)
			}

			if pred.Raw != testCase.raw {
				test.Errorf("Raw = %q, want %q", pred.Raw, testCase.raw)
			}

			// The parser must not interpret the value — that's the
			// validator's job.
			if pred.Duration != 0 {
				test.Errorf("Duration = %v, want 0 (validator hasn't run)", pred.Duration)
			}

			if !pred.Since.IsZero() {
				test.Errorf("Since = %v, want zero (validator hasn't run)", pred.Since)
			}
		})
	}
}

func TestParser_ModifiedSinceMissingValue(test *testing.T) {
	_, errs := filter.NewParser("modified-since:").Parse()

	if len(errs) == 0 {
		test.Fatalf("expected parse error for missing value")
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

// TestParser_RejectsUnrecognizedInput pins that no input is dropped silently: a
// character the grammar doesn't know, or a string that never closes, is a
// parse error at its own position instead of an early end of input (#760).
func TestParser_RejectsUnrecognizedInput(test *testing.T) {
	cases := []struct {
		name    string
		input   string
		pos     int
		message string
	}{
		{"ampersand between predicates", "type=note & domain=technical", 10, "unexpected character '&'"},
		{"glob in a bare value", "path=docs/technical/*", 20, "unexpected character '*'"},
		{"leading character", "&", 0, "unexpected character '&'"},
		{"non-ASCII in a bare value", "title=héllo", 7, "unexpected character 'é'"},
		{"non-breaking space", "type=note\u00a0status=open", 9, `unexpected character '\u00a0'`},
		{"inside parens", "(type=note & status=open)", 11, "unexpected character '&'"},
		{"in operator position", "status&open", 6, "unexpected character '&'"},
		{"after an edge arrow", "blocks->&", 8, "unexpected character '&'"},
		{"unterminated string predicate", `type=note "open`, 10, "unterminated string"},
		{"unterminated string value", `title="open`, 6, "unterminated string"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			_, errs := filter.NewParser(testCase.input).Parse()

			if len(errs) == 0 {
				test.Fatalf("input %q: no parse error", testCase.input)
			}

			if errs[0].Pos != testCase.pos || errs[0].Message != testCase.message {
				test.Errorf("input %q: first error = %+v, want {Pos:%d Message:%q}", testCase.input, errs[0], testCase.pos, testCase.message)
			}
		})
	}
}
