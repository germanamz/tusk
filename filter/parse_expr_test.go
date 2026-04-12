package filter

import "testing"

// exprEqual is a test helper that compares two Expr trees structurally.
func exprEqual(a, b Expr) bool {
	switch a := a.(type) {
	case TermExpr:
		b, ok := b.(TermExpr)
		if !ok {
			return false
		}
		if a.Text != b.Text {
			return false
		}
		if (a.Field == nil) != (b.Field == nil) {
			return false
		}
		if a.Field != nil && (a.Field.Key != b.Field.Key || a.Field.Value != b.Field.Value) {
			return false
		}
		if (a.Tag == nil) != (b.Tag == nil) {
			return false
		}
		if a.Tag != nil && (a.Tag.Name != b.Tag.Name || a.Tag.Exclude != b.Tag.Exclude) {
			return false
		}
		return true
	case AndExpr:
		b, ok := b.(AndExpr)
		if !ok || len(a.Children) != len(b.Children) {
			return false
		}
		for i := range a.Children {
			if !exprEqual(a.Children[i], b.Children[i]) {
				return false
			}
		}
		return true
	case OrExpr:
		b, ok := b.(OrExpr)
		if !ok || len(a.Children) != len(b.Children) {
			return false
		}
		for i := range a.Children {
			if !exprEqual(a.Children[i], b.Children[i]) {
				return false
			}
		}
		return true
	case NotExpr:
		b, ok := b.(NotExpr)
		if !ok {
			return false
		}
		return exprEqual(a.Child, b.Child)
	default:
		return false
	}
}

func TestParseExpr_SingleTerm(t *testing.T) {
	expr, errs := ParseExpr("status=active")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}}
	if !exprEqual(expr, want) {
		t.Fatalf("expected %+v, got %+v", want, expr)
	}
}

func TestParseExpr_ImplicitAnd(t *testing.T) {
	expr, errs := ParseExpr("status=active +api")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := AndExpr{Children: []Expr{
		TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		TermExpr{Tag: &TagFilter{Name: "api"}},
	}}
	if !exprEqual(expr, want) {
		t.Fatalf("expected AndExpr with 2 children, got %+v", expr)
	}
}

func TestParseExpr_ExplicitAnd(t *testing.T) {
	expr, errs := ParseExpr("status=active AND +api")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := AndExpr{Children: []Expr{
		TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		TermExpr{Tag: &TagFilter{Name: "api"}},
	}}
	if !exprEqual(expr, want) {
		t.Fatalf("expected AndExpr with 2 children, got %+v", expr)
	}
}

func TestParseExpr_Or(t *testing.T) {
	expr, errs := ParseExpr("status=active OR status=pending")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := OrExpr{Children: []Expr{
		TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		TermExpr{Field: &FieldFilter{Key: "status", Value: "pending"}},
	}}
	if !exprEqual(expr, want) {
		t.Fatalf("expected OrExpr with 2 children, got %+v", expr)
	}
}

func TestParseExpr_Not(t *testing.T) {
	expr, errs := ParseExpr("NOT status=deleted")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := NotExpr{
		Child: TermExpr{Field: &FieldFilter{Key: "status", Value: "deleted"}},
	}
	if !exprEqual(expr, want) {
		t.Fatalf("expected NotExpr, got %+v", expr)
	}
}

func TestParseExpr_Precedence_AndBeforeOr(t *testing.T) {
	// "a OR b AND c" should parse as "a OR (b AND c)"
	// because AND binds tighter than OR
	expr, errs := ParseExpr("status=active OR +api priority=3")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := OrExpr{Children: []Expr{
		TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		AndExpr{Children: []Expr{
			TermExpr{Tag: &TagFilter{Name: "api"}},
			TermExpr{Field: &FieldFilter{Key: "priority", Value: "3"}},
		}},
	}}
	if !exprEqual(expr, want) {
		t.Fatalf("precedence wrong: expected OR(term, AND(term, term)), got %+v", expr)
	}
}

func TestParseExpr_Parentheses(t *testing.T) {
	// "(a OR b) AND c" — parens override default precedence
	expr, errs := ParseExpr("(status=active OR status=pending) +api")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := AndExpr{Children: []Expr{
		OrExpr{Children: []Expr{
			TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
			TermExpr{Field: &FieldFilter{Key: "status", Value: "pending"}},
		}},
		TermExpr{Tag: &TagFilter{Name: "api"}},
	}}
	if !exprEqual(expr, want) {
		t.Fatalf("expected AND(OR(...), term), got %+v", expr)
	}
}

func TestParseExpr_NestedNot(t *testing.T) {
	expr, errs := ParseExpr("NOT NOT status=active")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := NotExpr{
		Child: NotExpr{
			Child: TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		},
	}
	if !exprEqual(expr, want) {
		t.Fatalf("expected NOT(NOT(term)), got %+v", expr)
	}
}

func TestParseExpr_EmptyInput(t *testing.T) {
	expr, errs := ParseExpr("")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if expr != nil {
		t.Fatalf("expected nil expr for empty input, got %+v", expr)
	}
}

func TestParseExpr_MismatchedParen(t *testing.T) {
	_, errs := ParseExpr("(status=active")
	if len(errs) == 0 {
		t.Fatal("expected error for unclosed paren")
	}
}

func TestParseExpr_UnexpectedRParen(t *testing.T) {
	_, errs := ParseExpr("status=active)")
	if len(errs) == 0 {
		t.Fatal("expected error for unexpected )")
	}
}

func TestParseExpr_TagExclude(t *testing.T) {
	expr, errs := ParseExpr("-docs")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := TermExpr{Tag: &TagFilter{Name: "docs", Exclude: true}}
	if !exprEqual(expr, want) {
		t.Fatalf("expected exclude tag term, got %+v", expr)
	}
}

func TestParseExpr_TextTerm(t *testing.T) {
	expr, errs := ParseExpr("sometext")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := TermExpr{Text: "sometext"}
	if !exprEqual(expr, want) {
		t.Fatalf("expected text term, got %+v", expr)
	}
}

func TestParseExpr_ComplexExpression(t *testing.T) {
	// (project=backend OR project=frontend) AND +api AND NOT status=deleted
	expr, errs := ParseExpr(`(project=backend OR project=frontend) AND +api AND NOT status=deleted`)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := AndExpr{Children: []Expr{
		OrExpr{Children: []Expr{
			TermExpr{Field: &FieldFilter{Key: "project", Value: "backend"}},
			TermExpr{Field: &FieldFilter{Key: "project", Value: "frontend"}},
		}},
		TermExpr{Tag: &TagFilter{Name: "api"}},
		NotExpr{Child: TermExpr{Field: &FieldFilter{Key: "status", Value: "deleted"}}},
	}}
	if !exprEqual(expr, want) {
		t.Fatalf("complex expression mismatch, got %+v", expr)
	}
}

func TestParseExpr_FieldValidation(t *testing.T) {
	// Unknown field should produce an error but continue
	expr, errs := ParseExpr("foo=bar OR status=active")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for unknown field, got %d: %v", len(errs), errs)
	}
	// The valid status=active side of OR should still be preserved
	if expr == nil {
		t.Fatal("expected non-nil expr — valid terms should survive validation errors")
	}
}

func TestParseExpr_FieldValidation_ImplicitAnd(t *testing.T) {
	// "foo=bar status=active" — bad field should not truncate the AND chain
	expr, errs := ParseExpr("foo=bar status=active")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for unknown field, got %d: %v", len(errs), errs)
	}
	if expr == nil {
		t.Fatal("expected non-nil expr — status=active should survive")
	}
	// The surviving expression should be the status=active term
	want := TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}}
	if !exprEqual(expr, want) {
		t.Fatalf("expected status=active term to survive, got %+v", expr)
	}
}

func TestParseExpr_FieldValidation_MiddleBadTerm(t *testing.T) {
	// "+api foo=bar status=active" — bad field in the middle should not affect surrounding terms
	expr, errs := ParseExpr("+api foo=bar status=active")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	want := AndExpr{Children: []Expr{
		TermExpr{Tag: &TagFilter{Name: "api"}},
		TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
	}}
	if !exprEqual(expr, want) {
		t.Fatalf("expected AND(+api, status=active), got %+v", expr)
	}
}

func TestParseExprFieldCarriesModifier(t *testing.T) {
	expr, errs := ParseExpr("+priority=4")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	term, ok := expr.(TermExpr)
	if !ok || term.Field == nil {
		t.Fatalf("expected TermExpr with Field, got %T", expr)
	}
	if term.Field.Modifier != '+' || term.Field.Key != "priority" || term.Field.Value != "4" {
		t.Errorf("unexpected field: %+v", term.Field)
	}
}

func TestParseExprTagRoundTrip(t *testing.T) {
	expr, _ := ParseExpr("+urgent")
	term, ok := expr.(TermExpr)
	if !ok || term.Tag == nil || term.Tag.Name != "urgent" || term.Tag.Exclude {
		t.Fatalf("unexpected: %+v", expr)
	}
}
