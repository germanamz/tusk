package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/germanamz/tusk/domain"
)

func TestParseProjectCreate_Basic(t *testing.T) {
	out, err := parseProjectCreate([]string{"workflow=kanban"})
	if err != nil {
		t.Fatalf("parseProjectCreate: %v", err)
	}
	if out.Workflow != "kanban" {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestParseProjectCreate_AutoCompleteAndUrgency(t *testing.T) {
	out, err := parseProjectCreate([]string{
		"workflow=kanban",
		"auto-complete.trigger=completed",
		"auto-complete.target=completed",
		"urgency.blocking-weight=15",
	})
	if err != nil {
		t.Fatalf("parseProjectCreate: %v", err)
	}
	if out.Settings.AutoCompleteParent == nil ||
		out.Settings.AutoCompleteParent.TriggerStatus != "completed" {
		t.Fatalf("auto-complete: %+v", out.Settings.AutoCompleteParent)
	}
	if out.Settings.Urgency == nil ||
		out.Settings.Urgency.BlockingWeight == nil ||
		*out.Settings.Urgency.BlockingWeight != 15 {
		t.Fatalf("urgency: %+v", out.Settings.Urgency)
	}
}

func TestParseProjectCreate_RejectsModifier(t *testing.T) {
	_, err := parseProjectCreate([]string{"+workflow=kanban"})
	if err == nil {
		t.Fatal("expected modifier rejection")
	}
}

func TestParseProjectCreate_UnknownField(t *testing.T) {
	_, err := parseProjectCreate([]string{"ghost=value"})
	if err == nil {
		t.Fatal("expected unknown-field error")
	}
}

func TestParseProjectModify_BareSet(t *testing.T) {
	mut, err := parseProjectModify([]string{"workflow=sprint", "urgency.blocking-weight=10"})
	if err != nil {
		t.Fatalf("parseProjectModify: %v", err)
	}
	if mut.Workflow == nil || *mut.Workflow != "sprint" {
		t.Fatalf("workflow: %+v", mut.Workflow)
	}
	if mut.UrgencySet["blocking_weight"] != 10 {
		t.Fatalf("urgency set: %+v", mut.UrgencySet)
	}
}

func TestParseProjectModify_Delta(t *testing.T) {
	mut, err := parseProjectModify([]string{"+urgency.blocking-weight=2", "-urgency.due-weight=1"})
	if err != nil {
		t.Fatalf("parseProjectModify: %v", err)
	}
	if mut.UrgencyDelta["blocking_weight"] != 2 {
		t.Fatalf("add delta: %+v", mut.UrgencyDelta)
	}
	if mut.UrgencyDelta["due_weight"] != -1 {
		t.Fatalf("sub delta: %+v", mut.UrgencyDelta)
	}
}

func TestParseProjectModify_DeltaOnNonUrgencyRejected(t *testing.T) {
	_, err := parseProjectModify([]string{"+workflow=sprint"})
	if err == nil {
		t.Fatal("expected rejection of modifier on workflow")
	}
}

func TestParseProjectModify_TaxonomyLevelsSet(t *testing.T) {
	mut, err := parseProjectModify([]string{"taxonomy.levels=milestone:story:(task,spike)"})
	if err != nil {
		t.Fatalf("parseProjectModify: %v", err)
	}
	if mut.TaxonomyAction != taxonomyActionSet {
		t.Fatalf("action: got %v, want set", mut.TaxonomyAction)
	}
	want := domain.Taxonomy{{"milestone"}, {"story"}, {"task", "spike"}}
	if !reflect.DeepEqual(mut.TaxonomyValue, want) {
		t.Fatalf("value: got %+v, want %+v", mut.TaxonomyValue, want)
	}
}

func TestParseProjectModify_TaxonomyLevelsClear(t *testing.T) {
	mut, err := parseProjectModify([]string{"taxonomy.levels="})
	if err != nil {
		t.Fatalf("parseProjectModify: %v", err)
	}
	if mut.TaxonomyAction != taxonomyActionClear {
		t.Fatalf("action: got %v, want clear", mut.TaxonomyAction)
	}
	if mut.TaxonomyValue != nil {
		t.Fatalf("value: got %+v, want nil", mut.TaxonomyValue)
	}
}

func TestParseProjectModify_TaxonomyDisableTrue(t *testing.T) {
	mut, err := parseProjectModify([]string{"taxonomy.disable=true"})
	if err != nil {
		t.Fatalf("parseProjectModify: %v", err)
	}
	if mut.TaxonomyAction != taxonomyActionEmpty {
		t.Fatalf("action: got %v, want empty", mut.TaxonomyAction)
	}
	if len(mut.TaxonomyValue) != 0 {
		t.Fatalf("expected empty value, got %+v", mut.TaxonomyValue)
	}
}

func TestParseProjectModify_TaxonomyDisableFalse(t *testing.T) {
	mut, err := parseProjectModify([]string{"taxonomy.disable=false"})
	if err != nil {
		t.Fatalf("parseProjectModify: %v", err)
	}
	if mut.TaxonomyAction != taxonomyActionClear {
		t.Fatalf("action: got %v, want clear", mut.TaxonomyAction)
	}
}

func TestParseProjectModify_TaxonomyDisableInvalid(t *testing.T) {
	_, err := parseProjectModify([]string{"taxonomy.disable=maybe"})
	if err == nil {
		t.Fatal("expected error for taxonomy.disable=maybe")
	}
}

func TestParseProjectModify_TaxonomyJSONSet(t *testing.T) {
	mut, err := parseProjectModify([]string{`taxonomy={"ranks":[["milestone"],["story"],["task","spike"]]}`})
	if err != nil {
		t.Fatalf("parseProjectModify: %v", err)
	}
	if mut.TaxonomyAction != taxonomyActionSet {
		t.Fatalf("action: got %v, want set", mut.TaxonomyAction)
	}
	want := domain.Taxonomy{{"milestone"}, {"story"}, {"task", "spike"}}
	if !reflect.DeepEqual(mut.TaxonomyValue, want) {
		t.Fatalf("value: got %+v, want %+v", mut.TaxonomyValue, want)
	}
}

func TestParseProjectModify_TaxonomyJSONEmpty(t *testing.T) {
	mut, err := parseProjectModify([]string{`taxonomy={"ranks":[]}`})
	if err != nil {
		t.Fatalf("parseProjectModify: %v", err)
	}
	if mut.TaxonomyAction != taxonomyActionEmpty {
		t.Fatalf("action: got %v, want empty", mut.TaxonomyAction)
	}
}

func TestParseProjectModify_TaxonomyJSONMalformed(t *testing.T) {
	_, err := parseProjectModify([]string{`taxonomy=not-json`})
	if err == nil {
		t.Fatal("expected error for non-JSON taxonomy value")
	}
}

func TestParseProjectModify_TaxonomyMutualExclusion(t *testing.T) {
	_, err := parseProjectModify([]string{"taxonomy.levels=milestone", "taxonomy.disable=true"})
	if err == nil {
		t.Fatal("expected mutual-exclusion error")
	}
}

func TestParseProjectModify_TaxonomyModifierRejected(t *testing.T) {
	cases := [][]string{
		{"+taxonomy.levels=milestone"},
		{"-taxonomy.disable=true"},
	}
	for _, args := range cases {
		if _, err := parseProjectModify(args); err == nil {
			t.Fatalf("expected modifier rejection for %v", args)
		}
	}
}

func TestParseProjectModify_TaxonomyInvalidInline(t *testing.T) {
	_, err := parseProjectModify([]string{"taxonomy.levels=a::b"})
	if err == nil {
		t.Fatal("expected error for malformed inline taxonomy")
	}
}

func TestExpandTaxonomyRefs_FileExpansion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "taxonomy.json")
	if err := os.WriteFile(path, []byte(`{"ranks":[["milestone"],["story"]]}`), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	expanded, err := expandTaxonomyRefs([]string{"taxonomy=@" + path}, nil, 1<<20)
	if err != nil {
		t.Fatalf("expandTaxonomyRefs: %v", err)
	}
	if len(expanded) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(expanded))
	}

	mut, err := parseProjectModify(expanded)
	if err != nil {
		t.Fatalf("parseProjectModify: %v", err)
	}
	if mut.TaxonomyAction != taxonomyActionSet {
		t.Fatalf("action: got %v, want set", mut.TaxonomyAction)
	}
	want := domain.Taxonomy{{"milestone"}, {"story"}}
	if !reflect.DeepEqual(mut.TaxonomyValue, want) {
		t.Fatalf("value: got %+v, want %+v", mut.TaxonomyValue, want)
	}
}
