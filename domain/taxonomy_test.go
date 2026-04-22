package domain

import (
	"strings"
	"testing"
)

func TestTaxonomy_Validate_Accepts(t *testing.T) {
	tax := Taxonomy{{"milestone"}, {"initiative"}, {"story"}, {"task", "spike"}}
	if err := tax.Validate(); err != nil {
		t.Fatalf("expected valid taxonomy, got %v", err)
	}
}

func TestTaxonomy_Validate_RejectsEmpty(t *testing.T) {
	var tax Taxonomy
	err := tax.Validate()
	if err == nil {
		t.Fatal("expected error for empty taxonomy")
	}
}

func TestTaxonomy_Validate_RejectsEmptyPeerGroup(t *testing.T) {
	tax := Taxonomy{{"milestone"}, {}}
	err := tax.Validate()
	if err == nil {
		t.Fatal("expected error for empty peer group")
	}
	if !strings.Contains(err.Error(), "rank 1") {
		t.Fatalf("expected error to reference the offending rank index, got %v", err)
	}
}

func TestTaxonomy_Validate_RejectsBadName(t *testing.T) {
	tax := Taxonomy{{"milestone"}, {"1bad"}}
	err := tax.Validate()
	if err == nil {
		t.Fatal("expected error for invalid name")
	}
	if !strings.Contains(err.Error(), "1bad") {
		t.Fatalf("expected error to reference %q, got %v", "1bad", err)
	}
}

func TestTaxonomy_Validate_RejectsDuplicate(t *testing.T) {
	tax := Taxonomy{{"milestone"}, {"story"}, {"story"}}
	err := tax.Validate()
	if err == nil {
		t.Fatal("expected error for duplicate level")
	}
	if !strings.Contains(err.Error(), "story") {
		t.Fatalf("expected error to mention the duplicate, got %v", err)
	}
}

func TestTaxonomy_RankOf(t *testing.T) {
	tax := Taxonomy{{"milestone"}, {"story"}, {"task", "spike"}}
	cases := []struct {
		level   string
		want    int
		wantOK  bool
		comment string
	}{
		{"milestone", 0, true, "top rank"},
		{"story", 1, true, "middle"},
		{"task", 2, true, "peer member"},
		{"spike", 2, true, "peer member"},
		{"unknown", 0, false, "not declared"},
	}
	for _, c := range cases {
		got, ok := tax.RankOf(c.level)
		if ok != c.wantOK {
			t.Errorf("%s: RankOf(%q) ok = %v, want %v", c.comment, c.level, ok, c.wantOK)
		}
		if got != c.want {
			t.Errorf("%s: RankOf(%q) = %d, want %d", c.comment, c.level, got, c.want)
		}
	}
}

func TestTaxonomy_Contains(t *testing.T) {
	tax := Taxonomy{{"milestone"}, {"story"}}
	if !tax.Contains("milestone") {
		t.Error("expected Contains(milestone) = true")
	}
	if tax.Contains("task") {
		t.Error("expected Contains(task) = false")
	}
}

func TestTaxonomy_IsTopRank(t *testing.T) {
	tax := Taxonomy{{"milestone"}, {"story"}}
	if !tax.IsTopRank("milestone") {
		t.Error("expected milestone to be top rank")
	}
	if tax.IsTopRank("story") {
		t.Error("expected story NOT to be top rank")
	}
	if tax.IsTopRank("unknown") {
		t.Error("expected unknown NOT to be top rank")
	}
}

func TestTaxonomy_IsEmpty(t *testing.T) {
	var empty Taxonomy
	if !empty.IsEmpty() {
		t.Error("expected nil taxonomy to be empty")
	}
	populated := Taxonomy{{"a"}}
	if populated.IsEmpty() {
		t.Error("expected populated taxonomy not to be empty")
	}
}

func TestTaxonomy_Clone(t *testing.T) {
	original := Taxonomy{{"milestone"}, {"task", "spike"}}
	clone := original.Clone()
	clone[1][0] = "mutated"
	if original[1][0] != "task" {
		t.Fatalf("Clone did not produce a deep copy: original was mutated to %q", original[1][0])
	}

	var nilTax Taxonomy
	if got := nilTax.Clone(); got != nil {
		t.Errorf("expected nil clone for nil taxonomy, got %v", got)
	}
}
