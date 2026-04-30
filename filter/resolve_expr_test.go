package filter

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/domain"
)

func TestResolveExpr_SingleTerm(test *testing.T) {
	resolver := NewResolver(mockTaskLookup{}, emptyProjects(), []string{"pending", "active"})
	expr := TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}

	// Should be a TermFilter with Statuses set
	termFilter, ok := result.(*domain.TermFilter)
	if !ok {
		test.Fatalf("expected *domain.TermFilter, got %T", result)
	}
	if len(termFilter.Statuses) != 1 || termFilter.Statuses[0] != "active" {
		test.Fatalf("expected Statuses [active], got %v", termFilter.Statuses)
	}
}

func TestResolveExpr_AndExpr(test *testing.T) {
	resolver := NewResolver(mockTaskLookup{}, emptyProjects(), []string{"pending", "active"})
	expr := AndExpr{Children: []Expr{
		TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		TermExpr{Tag: &TagFilter{Name: "api"}},
	}}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}

	andFilter, ok := result.(*domain.AndFilter)
	if !ok {
		test.Fatalf("expected *domain.AndFilter, got %T", result)
	}
	if len(andFilter.Children) != 2 {
		test.Fatalf("expected 2 children, got %d", len(andFilter.Children))
	}
}

func TestResolveExpr_OrExpr(test *testing.T) {
	resolver := NewResolver(mockTaskLookup{}, emptyProjects(), []string{"pending", "active"})
	expr := OrExpr{Children: []Expr{
		TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		TermExpr{Field: &FieldFilter{Key: "status", Value: "pending"}},
	}}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}

	orFilter, ok := result.(*domain.OrFilter)
	if !ok {
		test.Fatalf("expected *domain.OrFilter, got %T", result)
	}
	if len(orFilter.Children) != 2 {
		test.Fatalf("expected 2 children, got %d", len(orFilter.Children))
	}
}

func TestResolveExpr_NotExpr(test *testing.T) {
	resolver := NewResolver(mockTaskLookup{}, emptyProjects(), []string{"pending", "active"})
	expr := NotExpr{
		Child: TermExpr{Field: &FieldFilter{Key: "status", Value: "deleted"}},
	}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}

	notFilter, ok := result.(*domain.NotFilter)
	if !ok {
		test.Fatalf("expected *domain.NotFilter, got %T", result)
	}
	if notFilter.Child == nil {
		test.Fatal("expected non-nil child")
	}
}

func TestResolveExpr_DefaultStatuses(test *testing.T) {
	resolver := NewResolver(mockTaskLookup{}, emptyProjects(), []string{"pending", "active"})
	// Expression with no status term — should get default status wrapping
	expr := TermExpr{Tag: &TagFilter{Name: "api"}}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}

	// Should be wrapped in AND(TermFilter{Statuses: [pending, active]}, original)
	andFilter, ok := result.(*domain.AndFilter)
	if !ok {
		test.Fatalf("expected *domain.AndFilter for default status wrapping, got %T", result)
	}
	if len(andFilter.Children) != 2 {
		test.Fatalf("expected 2 children (default status + original), got %d", len(andFilter.Children))
	}
	defaultTerm, ok := andFilter.Children[0].(*domain.TermFilter)
	if !ok {
		test.Fatalf("expected first child to be *domain.TermFilter, got %T", andFilter.Children[0])
	}
	if len(defaultTerm.Statuses) != 2 || defaultTerm.Statuses[0] != "pending" || defaultTerm.Statuses[1] != "active" {
		test.Fatalf("expected default statuses [pending active], got %v", defaultTerm.Statuses)
	}
}

func TestResolveExpr_WithStatusSkipsDefault(test *testing.T) {
	resolver := NewResolver(mockTaskLookup{}, emptyProjects(), []string{"pending", "active"})
	expr := TermExpr{Field: &FieldFilter{Key: "status", Value: "completed"}}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}

	// Should NOT be wrapped in AND — status is present
	termFilter, ok := result.(*domain.TermFilter)
	if !ok {
		test.Fatalf("expected *domain.TermFilter (no wrapping), got %T", result)
	}
	if len(termFilter.Statuses) != 1 || termFilter.Statuses[0] != "completed" {
		test.Fatalf("expected Statuses [completed], got %v", termFilter.Statuses)
	}
}

func TestResolveExpr_TagTerms(test *testing.T) {
	resolver := NewResolver(mockTaskLookup{}, emptyProjects(), []string{"pending", "active"})
	expr := AndExpr{Children: []Expr{
		TermExpr{Tag: &TagFilter{Name: "api"}},
		TermExpr{Tag: &TagFilter{Name: "docs", Exclude: true}},
	}}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}

	// Should be AndFilter wrapping default statuses + AND(tag terms)
	andFilter, ok := result.(*domain.AndFilter)
	if !ok {
		test.Fatalf("expected *domain.AndFilter, got %T", result)
	}
	// First child is default status TermFilter, second is the user's AndFilter
	if len(andFilter.Children) < 2 {
		test.Fatalf("expected at least 2 children, got %d", len(andFilter.Children))
	}
}

func TestResolveExpr_NilExpr(test *testing.T) {
	resolver := NewResolver(mockTaskLookup{}, emptyProjects(), []string{"pending", "active"})
	result, errs := resolver.ResolveExpr(context.Background(), nil)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if result != nil {
		test.Fatalf("expected nil result for nil input, got %+v", result)
	}
}

func TestResolveExpr_FreeTextError(test *testing.T) {
	resolver := NewResolver(mockTaskLookup{}, emptyProjects(), []string{"pending", "active"})
	expr := TermExpr{Text: "someword"}
	_, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 1 {
		test.Fatalf("expected 1 error for free text, got %d: %v", len(errs), errs)
	}
}
