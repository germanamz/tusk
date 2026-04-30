package domain

import (
	"strings"
	"testing"
)

func TestTaxonomy_Validate_Accepts(test *testing.T) {
	taxonomy := Taxonomy{{"milestone"}, {"initiative"}, {"story"}, {"task", "spike"}}
	if err := taxonomy.Validate(); err != nil {
		test.Fatalf("expected valid taxonomy, got %v", err)
	}
}

func TestTaxonomy_Validate_RejectsEmpty(test *testing.T) {
	var taxonomy Taxonomy
	err := taxonomy.Validate()
	if err == nil {
		test.Fatal("expected error for empty taxonomy")
	}
}

func TestTaxonomy_Validate_RejectsEmptyPeerGroup(test *testing.T) {
	taxonomy := Taxonomy{{"milestone"}, {}}
	err := taxonomy.Validate()
	if err == nil {
		test.Fatal("expected error for empty peer group")
	}
	if !strings.Contains(err.Error(), "rank 1") {
		test.Fatalf("expected error to reference the offending rank index, got %v", err)
	}
}

func TestTaxonomy_Validate_RejectsBadName(test *testing.T) {
	taxonomy := Taxonomy{{"milestone"}, {"1bad"}}
	err := taxonomy.Validate()
	if err == nil {
		test.Fatal("expected error for invalid name")
	}
	if !strings.Contains(err.Error(), "1bad") {
		test.Fatalf("expected error to reference %q, got %v", "1bad", err)
	}
}

func TestTaxonomy_Validate_RejectsDuplicate(test *testing.T) {
	taxonomy := Taxonomy{{"milestone"}, {"story"}, {"story"}}
	err := taxonomy.Validate()
	if err == nil {
		test.Fatal("expected error for duplicate level")
	}
	if !strings.Contains(err.Error(), "story") {
		test.Fatalf("expected error to mention the duplicate, got %v", err)
	}
}

func TestTaxonomy_RankOf(test *testing.T) {
	taxonomy := Taxonomy{{"milestone"}, {"story"}, {"task", "spike"}}
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
	for _, testCase := range cases {
		got, ok := taxonomy.RankOf(testCase.level)
		if ok != testCase.wantOK {
			test.Errorf("%s: RankOf(%q) ok = %v, want %v", testCase.comment, testCase.level, ok, testCase.wantOK)
		}
		if got != testCase.want {
			test.Errorf("%s: RankOf(%q) = %d, want %d", testCase.comment, testCase.level, got, testCase.want)
		}
	}
}

func TestTaxonomy_Contains(test *testing.T) {
	taxonomy := Taxonomy{{"milestone"}, {"story"}}
	if !taxonomy.Contains("milestone") {
		test.Error("expected Contains(milestone) = true")
	}
	if taxonomy.Contains("task") {
		test.Error("expected Contains(task) = false")
	}
}

func TestTaxonomy_IsTopRank(test *testing.T) {
	taxonomy := Taxonomy{{"milestone"}, {"story"}}
	if !taxonomy.IsTopRank("milestone") {
		test.Error("expected milestone to be top rank")
	}
	if taxonomy.IsTopRank("story") {
		test.Error("expected story NOT to be top rank")
	}
	if taxonomy.IsTopRank("unknown") {
		test.Error("expected unknown NOT to be top rank")
	}
}

func TestTaxonomy_IsEmpty(test *testing.T) {
	var empty Taxonomy
	if !empty.IsEmpty() {
		test.Error("expected nil taxonomy to be empty")
	}
	populated := Taxonomy{{"a"}}
	if populated.IsEmpty() {
		test.Error("expected populated taxonomy not to be empty")
	}
}

func TestTaxonomy_Clone(test *testing.T) {
	original := Taxonomy{{"milestone"}, {"task", "spike"}}
	clone := original.Clone()
	clone[1][0] = "mutated"
	if original[1][0] != "task" {
		test.Fatalf("Clone did not produce a deep copy: original was mutated to %q", original[1][0])
	}

	var nilTax Taxonomy
	if got := nilTax.Clone(); got != nil {
		test.Errorf("expected nil clone for nil taxonomy, got %v", got)
	}
}
