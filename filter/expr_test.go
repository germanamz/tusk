package filter

import "testing"

func TestExpr_Marker(t *testing.T) {
	// Verify all types satisfy the Expr interface at compile time
	var _ Expr = AndExpr{}
	var _ Expr = OrExpr{}
	var _ Expr = NotExpr{}
	var _ Expr = TermExpr{}
}

func TestAndExpr_Children(t *testing.T) {
	expr := AndExpr{
		Children: []Expr{
			TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
			TermExpr{Tag: &TagFilter{Name: "api"}},
		},
	}
	if len(expr.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(expr.Children))
	}
}

func TestOrExpr_Children(t *testing.T) {
	expr := OrExpr{
		Children: []Expr{
			TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
			TermExpr{Field: &FieldFilter{Key: "status", Value: "pending"}},
		},
	}
	if len(expr.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(expr.Children))
	}
}

func TestNotExpr_Child(t *testing.T) {
	expr := NotExpr{
		Child: TermExpr{Field: &FieldFilter{Key: "status", Value: "deleted"}},
	}
	term, ok := expr.Child.(TermExpr)
	if !ok {
		t.Fatal("expected TermExpr child")
	}
	if term.Field.Value != "deleted" {
		t.Fatalf("expected value deleted, got %q", term.Field.Value)
	}
}

func TestTermExpr_Variants(t *testing.T) {
	// Field term
	ft := TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}}
	if ft.Field == nil {
		t.Fatal("expected non-nil field")
	}

	// Tag term
	tt := TermExpr{Tag: &TagFilter{Name: "api"}}
	if tt.Tag == nil {
		t.Fatal("expected non-nil tag")
	}

	// Text term
	txt := TermExpr{Text: "hello"}
	if txt.Text != "hello" {
		t.Fatalf("expected text hello, got %q", txt.Text)
	}
}
