package filter

import "testing"

// exprEqual is a test helper that compares two Expr trees structurally.
func exprEqual(left, right Expr) bool {
	switch left := left.(type) {
	case TermExpr:
		right, ok := right.(TermExpr)
		if !ok {
			return false
		}
		if left.Text != right.Text {
			return false
		}
		if (left.Field == nil) != (right.Field == nil) {
			return false
		}
		if left.Field != nil && (left.Field.Key != right.Field.Key || left.Field.Value != right.Field.Value) {
			return false
		}
		if (left.Tag == nil) != (right.Tag == nil) {
			return false
		}
		if left.Tag != nil && (left.Tag.Name != right.Tag.Name || left.Tag.Exclude != right.Tag.Exclude) {
			return false
		}
		return true
	case AndExpr:
		right, ok := right.(AndExpr)
		if !ok || len(left.Children) != len(right.Children) {
			return false
		}
		for index := range left.Children {
			if !exprEqual(left.Children[index], right.Children[index]) {
				return false
			}
		}
		return true
	case OrExpr:
		right, ok := right.(OrExpr)
		if !ok || len(left.Children) != len(right.Children) {
			return false
		}
		for index := range left.Children {
			if !exprEqual(left.Children[index], right.Children[index]) {
				return false
			}
		}
		return true
	case NotExpr:
		right, ok := right.(NotExpr)
		if !ok {
			return false
		}
		return exprEqual(left.Child, right.Child)
	default:
		return false
	}
}

func TestParseExpr_SingleTerm(test *testing.T) {
	expr, errs := ParseExpr("status=active")
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	want := TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}}
	if !exprEqual(expr, want) {
		test.Fatalf("expected %+v, got %+v", want, expr)
	}
}

func TestParseExpr_ImplicitAnd(test *testing.T) {
	expr, errs := ParseExpr("status=active +api")
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	want := AndExpr{Children: []Expr{
		TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		TermExpr{Tag: &TagFilter{Name: "api"}},
	}}
	if !exprEqual(expr, want) {
		test.Fatalf("expected AndExpr with 2 children, got %+v", expr)
	}
}

func TestParseExpr_ExplicitAnd(test *testing.T) {
	expr, errs := ParseExpr("status=active AND +api")
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	want := AndExpr{Children: []Expr{
		TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		TermExpr{Tag: &TagFilter{Name: "api"}},
	}}
	if !exprEqual(expr, want) {
		test.Fatalf("expected AndExpr with 2 children, got %+v", expr)
	}
}

func TestParseExpr_Or(test *testing.T) {
	expr, errs := ParseExpr("status=active OR status=pending")
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	want := OrExpr{Children: []Expr{
		TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		TermExpr{Field: &FieldFilter{Key: "status", Value: "pending"}},
	}}
	if !exprEqual(expr, want) {
		test.Fatalf("expected OrExpr with 2 children, got %+v", expr)
	}
}

func TestParseExpr_Not(test *testing.T) {
	expr, errs := ParseExpr("NOT status=deleted")
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	want := NotExpr{
		Child: TermExpr{Field: &FieldFilter{Key: "status", Value: "deleted"}},
	}
	if !exprEqual(expr, want) {
		test.Fatalf("expected NotExpr, got %+v", expr)
	}
}

func TestParseExpr_Precedence_AndBeforeOr(test *testing.T) {
	// "a OR b AND c" should parse as "a OR (b AND c)"
	// because AND binds tighter than OR
	expr, errs := ParseExpr("status=active OR +api priority=3")
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	want := OrExpr{Children: []Expr{
		TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		AndExpr{Children: []Expr{
			TermExpr{Tag: &TagFilter{Name: "api"}},
			TermExpr{Field: &FieldFilter{Key: "priority", Value: "3"}},
		}},
	}}
	if !exprEqual(expr, want) {
		test.Fatalf("precedence wrong: expected OR(term, AND(term, term)), got %+v", expr)
	}
}

func TestParseExpr_Parentheses(test *testing.T) {
	// "(a OR b) AND c" — parens override default precedence
	expr, errs := ParseExpr("(status=active OR status=pending) +api")
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	want := AndExpr{Children: []Expr{
		OrExpr{Children: []Expr{
			TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
			TermExpr{Field: &FieldFilter{Key: "status", Value: "pending"}},
		}},
		TermExpr{Tag: &TagFilter{Name: "api"}},
	}}
	if !exprEqual(expr, want) {
		test.Fatalf("expected AND(OR(...), term), got %+v", expr)
	}
}

func TestParseExpr_NestedNot(test *testing.T) {
	expr, errs := ParseExpr("NOT NOT status=active")
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	want := NotExpr{
		Child: NotExpr{
			Child: TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		},
	}
	if !exprEqual(expr, want) {
		test.Fatalf("expected NOT(NOT(term)), got %+v", expr)
	}
}

func TestParseExpr_EmptyInput(test *testing.T) {
	expr, errs := ParseExpr("")
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if expr != nil {
		test.Fatalf("expected nil expr for empty input, got %+v", expr)
	}
}

func TestParseExpr_MismatchedParen(test *testing.T) {
	_, errs := ParseExpr("(status=active")
	if len(errs) == 0 {
		test.Fatal("expected error for unclosed paren")
	}
}

func TestParseExpr_UnexpectedRParen(test *testing.T) {
	_, errs := ParseExpr("status=active)")
	if len(errs) == 0 {
		test.Fatal("expected error for unexpected )")
	}
}

func TestParseExpr_TagExclude(test *testing.T) {
	expr, errs := ParseExpr("-docs")
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	want := TermExpr{Tag: &TagFilter{Name: "docs", Exclude: true}}
	if !exprEqual(expr, want) {
		test.Fatalf("expected exclude tag term, got %+v", expr)
	}
}

func TestParseExpr_TextTerm(test *testing.T) {
	expr, errs := ParseExpr("sometext")
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	want := TermExpr{Text: "sometext"}
	if !exprEqual(expr, want) {
		test.Fatalf("expected text term, got %+v", expr)
	}
}

func TestParseExpr_ComplexExpression(test *testing.T) {
	// (project=backend OR project=frontend) AND +api AND NOT status=deleted
	expr, errs := ParseExpr(`(project=backend OR project=frontend) AND +api AND NOT status=deleted`)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
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
		test.Fatalf("complex expression mismatch, got %+v", expr)
	}
}

func TestParseExpr_FieldValidation(test *testing.T) {
	// Unknown field should produce an error but continue
	expr, errs := ParseExpr("foo=bar OR status=active")
	if len(errs) != 1 {
		test.Fatalf("expected 1 error for unknown field, got %d: %v", len(errs), errs)
	}
	// The valid status=active side of OR should still be preserved
	if expr == nil {
		test.Fatal("expected non-nil expr — valid terms should survive validation errors")
	}
}

func TestParseExpr_FieldValidation_ImplicitAnd(test *testing.T) {
	// "foo=bar status=active" — bad field should not truncate the AND chain
	expr, errs := ParseExpr("foo=bar status=active")
	if len(errs) != 1 {
		test.Fatalf("expected 1 error for unknown field, got %d: %v", len(errs), errs)
	}
	if expr == nil {
		test.Fatal("expected non-nil expr — status=active should survive")
	}
	// The surviving expression should be the status=active term
	want := TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}}
	if !exprEqual(expr, want) {
		test.Fatalf("expected status=active term to survive, got %+v", expr)
	}
}

func TestParseExpr_FieldValidation_MiddleBadTerm(test *testing.T) {
	// "+api foo=bar status=active" — bad field in the middle should not affect surrounding terms
	expr, errs := ParseExpr("+api foo=bar status=active")
	if len(errs) != 1 {
		test.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	want := AndExpr{Children: []Expr{
		TermExpr{Tag: &TagFilter{Name: "api"}},
		TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
	}}
	if !exprEqual(expr, want) {
		test.Fatalf("expected AND(+api, status=active), got %+v", expr)
	}
}

func TestParseExprFieldCarriesModifier(test *testing.T) {
	expr, errs := ParseExpr("+priority=4")
	if len(errs) > 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	term, ok := expr.(TermExpr)
	if !ok || term.Field == nil {
		test.Fatalf("expected TermExpr with Field, got %T", expr)
	}
	if term.Field.Modifier != '+' || term.Field.Key != "priority" || term.Field.Value != "4" {
		test.Errorf("unexpected field: %+v", term.Field)
	}
}

func TestParseExprTagRoundTrip(test *testing.T) {
	expr, _ := ParseExpr("+urgent")
	term, ok := expr.(TermExpr)
	if !ok || term.Tag == nil || term.Tag.Name != "urgent" || term.Tag.Exclude {
		test.Fatalf("unexpected: %+v", expr)
	}
}
