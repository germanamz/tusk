package filter

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/domain"
)

func TestResolveExpr_SingleTerm(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{}, []string{"pending", "active"})
	expr := TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	// Should be a TermFilter with Statuses set
	tf, ok := result.(*domain.TermFilter)
	if !ok {
		t.Fatalf("expected *domain.TermFilter, got %T", result)
	}
	if len(tf.Statuses) != 1 || tf.Statuses[0] != "active" {
		t.Fatalf("expected Statuses [active], got %v", tf.Statuses)
	}
}

func TestResolveExpr_AndExpr(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{}, []string{"pending", "active"})
	expr := AndExpr{Children: []Expr{
		TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		TermExpr{Tag: &TagFilter{Name: "api"}},
	}}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	af, ok := result.(*domain.AndFilter)
	if !ok {
		t.Fatalf("expected *domain.AndFilter, got %T", result)
	}
	if len(af.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(af.Children))
	}
}

func TestResolveExpr_OrExpr(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{}, []string{"pending", "active"})
	expr := OrExpr{Children: []Expr{
		TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		TermExpr{Field: &FieldFilter{Key: "status", Value: "pending"}},
	}}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	of, ok := result.(*domain.OrFilter)
	if !ok {
		t.Fatalf("expected *domain.OrFilter, got %T", result)
	}
	if len(of.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(of.Children))
	}
}

func TestResolveExpr_NotExpr(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{}, []string{"pending", "active"})
	expr := NotExpr{
		Child: TermExpr{Field: &FieldFilter{Key: "status", Value: "deleted"}},
	}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	nf, ok := result.(*domain.NotFilter)
	if !ok {
		t.Fatalf("expected *domain.NotFilter, got %T", result)
	}
	if nf.Child == nil {
		t.Fatal("expected non-nil child")
	}
}

func TestResolveExpr_DefaultStatuses(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{}, []string{"pending", "active"})
	// Expression with no status term — should get default status wrapping
	expr := TermExpr{Tag: &TagFilter{Name: "api"}}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	// Should be wrapped in AND(TermFilter{Statuses: [pending, active]}, original)
	af, ok := result.(*domain.AndFilter)
	if !ok {
		t.Fatalf("expected *domain.AndFilter for default status wrapping, got %T", result)
	}
	if len(af.Children) != 2 {
		t.Fatalf("expected 2 children (default status + original), got %d", len(af.Children))
	}
	defaultTerm, ok := af.Children[0].(*domain.TermFilter)
	if !ok {
		t.Fatalf("expected first child to be *domain.TermFilter, got %T", af.Children[0])
	}
	if len(defaultTerm.Statuses) != 2 || defaultTerm.Statuses[0] != "pending" || defaultTerm.Statuses[1] != "active" {
		t.Fatalf("expected default statuses [pending active], got %v", defaultTerm.Statuses)
	}
}

func TestResolveExpr_WithStatusSkipsDefault(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{}, []string{"pending", "active"})
	expr := TermExpr{Field: &FieldFilter{Key: "status", Value: "completed"}}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	// Should NOT be wrapped in AND — status is present
	tf, ok := result.(*domain.TermFilter)
	if !ok {
		t.Fatalf("expected *domain.TermFilter (no wrapping), got %T", result)
	}
	if len(tf.Statuses) != 1 || tf.Statuses[0] != "completed" {
		t.Fatalf("expected Statuses [completed], got %v", tf.Statuses)
	}
}

func TestResolveExpr_TagTerms(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{}, []string{"pending", "active"})
	expr := AndExpr{Children: []Expr{
		TermExpr{Tag: &TagFilter{Name: "api"}},
		TermExpr{Tag: &TagFilter{Name: "docs", Exclude: true}},
	}}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	// Should be AndFilter wrapping default statuses + AND(tag terms)
	af, ok := result.(*domain.AndFilter)
	if !ok {
		t.Fatalf("expected *domain.AndFilter, got %T", result)
	}
	// First child is default status TermFilter, second is the user's AndFilter
	if len(af.Children) < 2 {
		t.Fatalf("expected at least 2 children, got %d", len(af.Children))
	}
}

func TestResolveExpr_NilExpr(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{}, []string{"pending", "active"})
	result, errs := resolver.ResolveExpr(context.Background(), nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if result != nil {
		t.Fatalf("expected nil result for nil input, got %+v", result)
	}
}

func TestResolveExpr_FreeTextError(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{}, []string{"pending", "active"})
	expr := TermExpr{Text: "someword"}
	_, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for free text, got %d: %v", len(errs), errs)
	}
}
