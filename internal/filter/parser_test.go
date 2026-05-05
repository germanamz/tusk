package filter_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/filter"
)

func TestParser_PropertyEquality(test *testing.T) {
	expr, errs := filter.NewParser("type=ticket").Parse()

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
