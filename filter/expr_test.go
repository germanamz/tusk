package filter

import "testing"

func TestExpr_Marker(test *testing.T) {
	// Verify all types satisfy the Expr interface at compile time
	var _ Expr = AndExpr{}
	var _ Expr = OrExpr{}
	var _ Expr = NotExpr{}
	var _ Expr = TermExpr{}
}

func TestAndExpr_Children(test *testing.T) {
	expr := AndExpr{
		Children: []Expr{
			TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
			TermExpr{Tag: &TagFilter{Name: "api"}},
		},
	}
	if len(expr.Children) != 2 {
		test.Fatalf("expected 2 children, got %d", len(expr.Children))
	}
}

func TestOrExpr_Children(test *testing.T) {
	expr := OrExpr{
		Children: []Expr{
			TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
			TermExpr{Field: &FieldFilter{Key: "status", Value: "pending"}},
		},
	}
	if len(expr.Children) != 2 {
		test.Fatalf("expected 2 children, got %d", len(expr.Children))
	}
}

func TestNotExpr_Child(test *testing.T) {
	expr := NotExpr{
		Child: TermExpr{Field: &FieldFilter{Key: "status", Value: "deleted"}},
	}
	term, ok := expr.Child.(TermExpr)
	if !ok {
		test.Fatal("expected TermExpr child")
	}
	if term.Field.Value != "deleted" {
		test.Fatalf("expected value deleted, got %q", term.Field.Value)
	}
}

func TestTermExpr_Variants(test *testing.T) {
	// Field term
	fieldTerm := TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}}
	if fieldTerm.Field == nil {
		test.Fatal("expected non-nil field")
	}

	// Tag term
	tagTerm := TermExpr{Tag: &TagFilter{Name: "api"}}
	if tagTerm.Tag == nil {
		test.Fatal("expected non-nil tag")
	}

	// Text term
	textTerm := TermExpr{Text: "hello"}
	if textTerm.Text != "hello" {
		test.Fatalf("expected text hello, got %q", textTerm.Text)
	}
}
