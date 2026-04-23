package tui

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/domain"
)

func TestFormatTaxonomyError_Missing(t *testing.T) {
	te := &domain.TaxonomyError{
		Reason:   "missing",
		Taxonomy: domain.Taxonomy{{"milestone", "epic"}, {"story"}},
	}
	msg, ok := formatTaxonomyError(te, "backend")
	if !ok {
		t.Fatalf("expected ok=true")
	}
	for _, sub := range []string{"project backend", "requires a level", "milestone,epic"} {
		if !strings.Contains(msg, sub) {
			t.Errorf("expected %q in message, got: %q", sub, msg)
		}
	}
}

func TestFormatTaxonomyError_UnknownLevel(t *testing.T) {
	te := &domain.TaxonomyError{
		Reason:   "unknown_level",
		Level:    "bogus",
		Taxonomy: domain.Taxonomy{{"milestone"}, {"story"}},
	}
	msg, _ := formatTaxonomyError(te, "ops")
	for _, sub := range []string{"level bogus", "ops", "milestone:story"} {
		if !strings.Contains(msg, sub) {
			t.Errorf("expected %q in message, got: %q", sub, msg)
		}
	}
}

func TestFormatTaxonomyError_RootRequiresTopRank(t *testing.T) {
	te := &domain.TaxonomyError{
		Reason:   "root_requires_top_rank",
		Level:    "story",
		Taxonomy: domain.Taxonomy{{"milestone"}, {"story"}},
	}
	msg, _ := formatTaxonomyError(te, "")
	for _, sub := range []string{"top-rank level", "milestone", "got story"} {
		if !strings.Contains(msg, sub) {
			t.Errorf("expected %q in message, got: %q", sub, msg)
		}
	}
}

func TestFormatTaxonomyError_ParentRankNotLower(t *testing.T) {
	te := &domain.TaxonomyError{
		Reason:      "parent_rank_not_lower",
		Level:       "milestone",
		ParentLevel: "milestone",
		Taxonomy:    domain.Taxonomy{{"milestone"}, {"story"}},
	}
	msg, _ := formatTaxonomyError(te, "")
	for _, sub := range []string{"milestone cannot sit under milestone", "strictly lower"} {
		if !strings.Contains(msg, sub) {
			t.Errorf("expected %q in message, got: %q", sub, msg)
		}
	}
}

func TestFormatTaxonomyError_NonTaxonomyErrorReturnsFalse(t *testing.T) {
	_, ok := formatTaxonomyError(domain.ErrNotFound, "x")
	if ok {
		t.Fatal("expected ok=false for non-taxonomy error")
	}
}

func TestRunCreate_TaxonomyErrorFriendlyMessage(t *testing.T) {
	levels := [][]string{{"milestone"}, {"story"}}
	app, _, _ := testAppWithTaxonomy(t, levels)

	app.root.SetArgs([]string{"task", "create", "needs level"})
	err := app.root.Execute()
	if err == nil {
		t.Fatal("expected error for missing level, got nil")
	}
	if !strings.Contains(err.Error(), "requires a level") {
		t.Fatalf("expected friendly message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "milestone") {
		t.Fatalf("expected top-rank peer in message, got: %v", err)
	}
}
