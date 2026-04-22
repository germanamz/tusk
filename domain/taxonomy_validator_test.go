package domain

import (
	"errors"
	"testing"
)

func strPtr(s string) *string { return &s }

func TestValidator_EmptyTaxonomyAccepts(t *testing.T) {
	err := TaxonomyValidator{}.Check(ValidationContext{Taxonomy: nil}, &Task{})
	if err != nil {
		t.Fatalf("expected nil on empty taxonomy, got %v", err)
	}
}

func TestValidator_MissingLevel(t *testing.T) {
	tax := Taxonomy{{"milestone"}, {"story"}}
	task := &Task{}
	err := TaxonomyValidator{}.Check(ValidationContext{Taxonomy: tax}, task)
	assertTaxonomyError(t, err, "missing")

	empty := ""
	task2 := &Task{Level: &empty}
	err = TaxonomyValidator{}.Check(ValidationContext{Taxonomy: tax}, task2)
	assertTaxonomyError(t, err, "missing")
}

func TestValidator_UnknownLevel(t *testing.T) {
	tax := Taxonomy{{"milestone"}, {"story"}}
	task := &Task{Level: strPtr("epic")}
	err := TaxonomyValidator{}.Check(ValidationContext{Taxonomy: tax}, task)
	te := assertTaxonomyError(t, err, "unknown_level")
	if te.Level != "epic" {
		t.Fatalf("expected Level=epic, got %q", te.Level)
	}
}

func TestValidator_RootRequiresTopRank(t *testing.T) {
	tax := Taxonomy{{"milestone"}, {"story"}}
	task := &Task{Level: strPtr("story")}
	err := TaxonomyValidator{}.Check(ValidationContext{Taxonomy: tax, ParentLevel: nil}, task)
	assertTaxonomyError(t, err, "root_requires_top_rank")
}

func TestValidator_RootTopRankAccepted(t *testing.T) {
	tax := Taxonomy{{"milestone"}, {"story"}}
	task := &Task{Level: strPtr("milestone")}
	err := TaxonomyValidator{}.Check(ValidationContext{Taxonomy: tax, ParentLevel: nil}, task)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidator_ParentRankNotLower_SameRank(t *testing.T) {
	tax := Taxonomy{{"milestone"}, {"story"}, {"task", "spike"}}
	task := &Task{Level: strPtr("task")}
	err := TaxonomyValidator{}.Check(ValidationContext{Taxonomy: tax, ParentLevel: strPtr("spike")}, task)
	te := assertTaxonomyError(t, err, "parent_rank_not_lower")
	if te.ParentLevel != "spike" || te.Level != "task" {
		t.Fatalf("unexpected Level/ParentLevel in error: %+v", te)
	}
}

func TestValidator_ParentRankNotLower_Deeper(t *testing.T) {
	tax := Taxonomy{{"milestone"}, {"story"}, {"task"}}
	task := &Task{Level: strPtr("story")}
	err := TaxonomyValidator{}.Check(ValidationContext{Taxonomy: tax, ParentLevel: strPtr("task")}, task)
	assertTaxonomyError(t, err, "parent_rank_not_lower")
}

func TestValidator_ParentLevelless(t *testing.T) {
	tax := Taxonomy{{"milestone"}, {"story"}}
	task := &Task{Level: strPtr("story")}
	parentLevel := ""
	err := TaxonomyValidator{}.Check(ValidationContext{Taxonomy: tax, ParentLevel: &parentLevel}, task)
	assertTaxonomyError(t, err, "parent_rank_not_lower")
}

func TestValidator_ParentRankStrictLess(t *testing.T) {
	tax := Taxonomy{{"milestone"}, {"story"}, {"task", "spike"}}
	task := &Task{Level: strPtr("task")}
	err := TaxonomyValidator{}.Check(ValidationContext{Taxonomy: tax, ParentLevel: strPtr("story")}, task)
	if err != nil {
		t.Fatalf("expected nil for strictly lower parent rank, got %v", err)
	}
}

func assertTaxonomyError(t *testing.T, err error, reason string) *TaxonomyError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected *TaxonomyError (reason=%s), got nil", reason)
	}
	if !errors.Is(err, ErrTaxonomyViolation) {
		t.Fatalf("expected errors.Is(err, ErrTaxonomyViolation), got err=%v", err)
	}
	var te *TaxonomyError
	if !errors.As(err, &te) {
		t.Fatalf("expected *TaxonomyError, got %T: %v", err, err)
	}
	if te.Reason != reason {
		t.Fatalf("expected reason %q, got %q (err=%v)", reason, te.Reason, err)
	}
	return te
}
