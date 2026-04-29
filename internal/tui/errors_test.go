package tui

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/domain"
)

func TestFormatTaxonomyError_Missing(test *testing.T) {
	taxonomyErr := &domain.TaxonomyError{
		Reason:   "missing",
		Taxonomy: domain.Taxonomy{{"milestone", "epic"}, {"story"}},
	}
	msg, ok := formatTaxonomyError(taxonomyErr, "backend")
	if !ok {
		test.Fatalf("expected ok=true")
	}
	for _, sub := range []string{"project backend", "requires a level", "milestone,epic"} {
		if !strings.Contains(msg, sub) {
			test.Errorf("expected %q in message, got: %q", sub, msg)
		}
	}
}

func TestFormatTaxonomyError_UnknownLevel(test *testing.T) {
	taxonomyErr := &domain.TaxonomyError{
		Reason:   "unknown_level",
		Level:    "bogus",
		Taxonomy: domain.Taxonomy{{"milestone"}, {"story"}},
	}
	msg, _ := formatTaxonomyError(taxonomyErr, "ops")
	for _, sub := range []string{"level bogus", "ops", "milestone:story"} {
		if !strings.Contains(msg, sub) {
			test.Errorf("expected %q in message, got: %q", sub, msg)
		}
	}
}

func TestFormatTaxonomyError_RootRequiresTopRank(test *testing.T) {
	taxonomyErr := &domain.TaxonomyError{
		Reason:   "root_requires_top_rank",
		Level:    "story",
		Taxonomy: domain.Taxonomy{{"milestone"}, {"story"}},
	}
	msg, _ := formatTaxonomyError(taxonomyErr, "")
	for _, sub := range []string{"top-rank level", "milestone", "got story"} {
		if !strings.Contains(msg, sub) {
			test.Errorf("expected %q in message, got: %q", sub, msg)
		}
	}
}

func TestFormatTaxonomyError_ParentRankNotLower(test *testing.T) {
	taxonomyErr := &domain.TaxonomyError{
		Reason:      "parent_rank_not_lower",
		Level:       "milestone",
		ParentLevel: "milestone",
		Taxonomy:    domain.Taxonomy{{"milestone"}, {"story"}},
	}
	msg, _ := formatTaxonomyError(taxonomyErr, "")
	for _, sub := range []string{"milestone cannot sit under milestone", "strictly lower"} {
		if !strings.Contains(msg, sub) {
			test.Errorf("expected %q in message, got: %q", sub, msg)
		}
	}
}

func TestFormatTaxonomyError_NonTaxonomyErrorReturnsFalse(test *testing.T) {
	_, ok := formatTaxonomyError(domain.ErrNotFound, "x")
	if ok {
		test.Fatal("expected ok=false for non-taxonomy error")
	}
}

func TestRunCreate_TaxonomyErrorFriendlyMessage(test *testing.T) {
	levels := [][]string{{"milestone"}, {"story"}}
	app, _, _ := testAppWithTaxonomy(test, levels)

	app.root.SetArgs([]string{"task", "create", "needs level"})
	err := app.root.Execute()
	if err == nil {
		test.Fatal("expected error for missing level, got nil")
	}
	if !strings.Contains(err.Error(), "requires a level") {
		test.Fatalf("expected friendly message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "milestone") {
		test.Fatalf("expected top-rank peer in message, got: %v", err)
	}
}
