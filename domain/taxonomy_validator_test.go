package domain

import (
	"errors"
	"testing"
)

func strPtr(str string) *string { return &str }

func TestValidator_EmptyTaxonomyAccepts(test *testing.T) {
	err := TaxonomyValidator{}.Check(ValidationContext{Taxonomy: nil}, &Task{})

	if err != nil {
		test.Fatalf("expected nil on empty taxonomy, got %v", err)
	}
}

func TestValidator_MissingLevel(test *testing.T) {
	taxonomy := Taxonomy{{"milestone"}, {"story"}}
	task := &Task{}

	err := TaxonomyValidator{}.Check(ValidationContext{Taxonomy: taxonomy}, task)
	assertTaxonomyError(test, err, "missing")

	empty := ""
	task2 := &Task{Level: &empty}
	err = TaxonomyValidator{}.Check(ValidationContext{Taxonomy: taxonomy}, task2)
	assertTaxonomyError(test, err, "missing")
}

func TestValidator_UnknownLevel(test *testing.T) {
	taxonomy := Taxonomy{{"milestone"}, {"story"}}
	task := &Task{Level: strPtr("epic")}

	err := TaxonomyValidator{}.Check(ValidationContext{Taxonomy: taxonomy}, task)
	taxonomyErr := assertTaxonomyError(test, err, "unknown_level")
	if taxonomyErr.Level != "epic" {
		test.Fatalf("expected Level=epic, got %q", taxonomyErr.Level)
	}
}

func TestValidator_RootRequiresTopRank(test *testing.T) {
	taxonomy := Taxonomy{{"milestone"}, {"story"}}
	task := &Task{Level: strPtr("story")}

	err := TaxonomyValidator{}.Check(ValidationContext{Taxonomy: taxonomy, ParentLevel: nil}, task)
	assertTaxonomyError(test, err, "root_requires_top_rank")
}

func TestValidator_RootTopRankAccepted(test *testing.T) {
	taxonomy := Taxonomy{{"milestone"}, {"story"}}
	task := &Task{Level: strPtr("milestone")}

	err := TaxonomyValidator{}.Check(ValidationContext{Taxonomy: taxonomy, ParentLevel: nil}, task)

	if err != nil {
		test.Fatalf("expected nil, got %v", err)
	}
}

func TestValidator_ParentRankNotLower_SameRank(test *testing.T) {
	taxonomy := Taxonomy{{"milestone"}, {"story"}, {"task", "spike"}}
	task := &Task{Level: strPtr("task")}

	err := TaxonomyValidator{}.Check(ValidationContext{Taxonomy: taxonomy, ParentLevel: strPtr("spike")}, task)
	taxonomyErr := assertTaxonomyError(test, err, "parent_rank_not_lower")
	if taxonomyErr.ParentLevel != "spike" || taxonomyErr.Level != "task" {
		test.Fatalf("unexpected Level/ParentLevel in error: %+v", taxonomyErr)
	}
}

func TestValidator_ParentRankNotLower_Deeper(test *testing.T) {
	taxonomy := Taxonomy{{"milestone"}, {"story"}, {"task"}}
	task := &Task{Level: strPtr("story")}

	err := TaxonomyValidator{}.Check(ValidationContext{Taxonomy: taxonomy, ParentLevel: strPtr("task")}, task)
	assertTaxonomyError(test, err, "parent_rank_not_lower")
}

func TestValidator_ParentLevelless(test *testing.T) {
	taxonomy := Taxonomy{{"milestone"}, {"story"}}
	task := &Task{Level: strPtr("story")}
	parentLevel := ""

	err := TaxonomyValidator{}.Check(ValidationContext{Taxonomy: taxonomy, ParentLevel: &parentLevel}, task)
	assertTaxonomyError(test, err, "parent_rank_not_lower")
}

func TestValidator_ParentRankStrictLess(test *testing.T) {
	taxonomy := Taxonomy{{"milestone"}, {"story"}, {"task", "spike"}}
	task := &Task{Level: strPtr("task")}

	err := TaxonomyValidator{}.Check(ValidationContext{Taxonomy: taxonomy, ParentLevel: strPtr("story")}, task)

	if err != nil {
		test.Fatalf("expected nil for strictly lower parent rank, got %v", err)
	}
}

func assertTaxonomyError(test *testing.T, err error, reason string) *TaxonomyError {
	test.Helper()
	if err == nil {
		test.Fatalf("expected *TaxonomyError (reason=%s), got nil", reason)
	}
	if !errors.Is(err, ErrTaxonomyViolation) {
		test.Fatalf("expected errors.Is(err, ErrTaxonomyViolation), got err=%v", err)
	}
	var taxonomyErr *TaxonomyError
	if !errors.As(err, &taxonomyErr) {
		test.Fatalf("expected *TaxonomyError, got %T: %v", err, err)
	}
	if taxonomyErr.Reason != reason {
		test.Fatalf("expected reason %q, got %q (err=%v)", reason, taxonomyErr.Reason, err)
	}
	return taxonomyErr
}
