package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/germanamz/tusk/domain"
)

func TestParseProjectCreate_Basic(test *testing.T) {
	out, err := parseProjectCreate([]string{"workflow=kanban"})

	if err != nil {
		test.Fatalf("parseProjectCreate: %v", err)
	}

	if out.Workflow != "kanban" {
		test.Fatalf("unexpected: %+v", out)
	}
}

func TestParseProjectCreate_AutoCompleteAndUrgency(test *testing.T) {
	out, err := parseProjectCreate([]string{
		"workflow=kanban",
		"auto-complete.trigger=completed",
		"auto-complete.target=completed",
		"urgency.blocking-weight=15",
	})

	if err != nil {
		test.Fatalf("parseProjectCreate: %v", err)
	}

	if out.Settings.AutoCompleteParent == nil ||
		out.Settings.AutoCompleteParent.TriggerStatus != "completed" {
		test.Fatalf("auto-complete: %+v", out.Settings.AutoCompleteParent)
	}
	if out.Settings.Urgency == nil ||
		out.Settings.Urgency.BlockingWeight == nil ||
		*out.Settings.Urgency.BlockingWeight != 15 {
		test.Fatalf("urgency: %+v", out.Settings.Urgency)
	}
}

func TestParseProjectCreate_Description(test *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "plain", args: []string{"workflow=kanban", `description="plain text"`}, want: "plain text"},
		{name: "empty", args: []string{"workflow=kanban", "description="}, want: ""},
		{name: "omitted", args: []string{"workflow=kanban"}, want: ""},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			out, err := parseProjectCreate(testCase.args)

			if err != nil {
				test.Fatalf("parseProjectCreate: %v", err)
			}

			if out.Description != testCase.want {
				test.Fatalf("description = %q, want %q", out.Description, testCase.want)
			}
		})
	}
}

func TestParseProjectCreate_DescriptionFromFile(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "vision.md")
	const body = "# vision\nbody text"

	if writeErr := os.WriteFile(path, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("writing fixture: %v", writeErr)
	}

	out, parseErr := parseProjectCreate([]string{"workflow=kanban", "description=@" + path})

	if parseErr != nil {
		test.Fatalf("parseProjectCreate: %v", parseErr)
	}

	if out.Description != "@"+path {
		test.Fatalf("parser captured Description = %q, want literal %q (expansion happens at RunE)", out.Description, "@"+path)
	}

	expanded, expandErr := expandRefs(out.Description, nil, 1<<20)

	if expandErr != nil {
		test.Fatalf("expandRefs: %v", expandErr)
	}

	if expanded != body {
		test.Fatalf("expanded = %q, want %q", expanded, body)
	}
}

func TestParseProjectCreate_RejectsModifier(test *testing.T) {
	_, err := parseProjectCreate([]string{"+workflow=kanban"})
	if err == nil {
		test.Fatal("expected modifier rejection")
	}
}

func TestParseProjectCreate_UnknownField(test *testing.T) {
	_, err := parseProjectCreate([]string{"ghost=value"})
	if err == nil {
		test.Fatal("expected unknown-field error")
	}
}

func TestParseProjectModify_BareSet(test *testing.T) {
	mut, err := parseProjectModify([]string{"workflow=sprint", "urgency.blocking-weight=10"})

	if err != nil {
		test.Fatalf("parseProjectModify: %v", err)
	}

	if mut.Workflow == nil || *mut.Workflow != "sprint" {
		test.Fatalf("workflow: %+v", mut.Workflow)
	}
	if mut.UrgencySet["blocking_weight"] != 10 {
		test.Fatalf("urgency set: %+v", mut.UrgencySet)
	}
}

func TestParseProjectModify_Delta(test *testing.T) {
	mut, err := parseProjectModify([]string{"+urgency.blocking-weight=2", "-urgency.due-weight=1"})

	if err != nil {
		test.Fatalf("parseProjectModify: %v", err)
	}

	if mut.UrgencyDelta["blocking_weight"] != 2 {
		test.Fatalf("add delta: %+v", mut.UrgencyDelta)
	}
	if mut.UrgencyDelta["due_weight"] != -1 {
		test.Fatalf("sub delta: %+v", mut.UrgencyDelta)
	}
}

func TestParseProjectModify_DeltaOnNonUrgencyRejected(test *testing.T) {
	_, err := parseProjectModify([]string{"+workflow=sprint"})
	if err == nil {
		test.Fatal("expected rejection of modifier on workflow")
	}
}

func TestParseProjectModify_DescriptionSet(test *testing.T) {
	mut, err := parseProjectModify([]string{`description="new text"`})

	if err != nil {
		test.Fatalf("parseProjectModify: %v", err)
	}

	if mut.Description == nil {
		test.Fatal("Description outer pointer is nil")
	}
	if *mut.Description == nil {
		test.Fatal("Description inner pointer is nil; expected non-nil for set")
	}
	if **mut.Description != "new text" {
		test.Fatalf("Description = %q, want %q", **mut.Description, "new text")
	}
}

func TestParseProjectModify_DescriptionClear(test *testing.T) {
	mut, err := parseProjectModify([]string{"description="})

	if err != nil {
		test.Fatalf("parseProjectModify: %v", err)
	}

	if mut.Description == nil {
		test.Fatal("Description outer pointer is nil; expected non-nil for clear")
	}
	if *mut.Description != nil {
		test.Fatalf("Description inner = %v, want nil for clear", *mut.Description)
	}
}

func TestParseProjectModify_DescriptionFromFile(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "vision.md")
	const body = "updated body"

	if writeErr := os.WriteFile(path, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("writing fixture: %v", writeErr)
	}

	mut, parseErr := parseProjectModify([]string{"description=@" + path})

	if parseErr != nil {
		test.Fatalf("parseProjectModify: %v", parseErr)
	}

	if mut.Description == nil || *mut.Description == nil {
		test.Fatalf("Description not set: %+v", mut.Description)
	}
	if **mut.Description != "@"+path {
		test.Fatalf("parser captured Description = %q, want literal %q (expansion happens at RunE)", **mut.Description, "@"+path)
	}

	expanded, expandErr := expandRefs(**mut.Description, nil, 1<<20)

	if expandErr != nil {
		test.Fatalf("expandRefs: %v", expandErr)
	}

	if expanded != body {
		test.Fatalf("expanded = %q, want %q", expanded, body)
	}
}

func TestParseProjectModify_TaxonomyLevelsSet(test *testing.T) {
	mut, err := parseProjectModify([]string{"taxonomy.levels=milestone:story:(task,spike)"})

	if err != nil {
		test.Fatalf("parseProjectModify: %v", err)
	}

	if mut.TaxonomyAction != taxonomyActionSet {
		test.Fatalf("action: got %v, want set", mut.TaxonomyAction)
	}
	want := domain.Taxonomy{{"milestone"}, {"story"}, {"task", "spike"}}
	if !reflect.DeepEqual(mut.TaxonomyValue, want) {
		test.Fatalf("value: got %+v, want %+v", mut.TaxonomyValue, want)
	}
}

func TestParseProjectModify_TaxonomyLevelsClear(test *testing.T) {
	mut, err := parseProjectModify([]string{"taxonomy.levels="})

	if err != nil {
		test.Fatalf("parseProjectModify: %v", err)
	}

	if mut.TaxonomyAction != taxonomyActionClear {
		test.Fatalf("action: got %v, want clear", mut.TaxonomyAction)
	}
	if mut.TaxonomyValue != nil {
		test.Fatalf("value: got %+v, want nil", mut.TaxonomyValue)
	}
}

func TestParseProjectModify_TaxonomyDisableTrue(test *testing.T) {
	mut, err := parseProjectModify([]string{"taxonomy.disable=true"})

	if err != nil {
		test.Fatalf("parseProjectModify: %v", err)
	}

	if mut.TaxonomyAction != taxonomyActionEmpty {
		test.Fatalf("action: got %v, want empty", mut.TaxonomyAction)
	}
	if len(mut.TaxonomyValue) != 0 {
		test.Fatalf("expected empty value, got %+v", mut.TaxonomyValue)
	}
}

func TestParseProjectModify_TaxonomyDisableFalse(test *testing.T) {
	mut, err := parseProjectModify([]string{"taxonomy.disable=false"})

	if err != nil {
		test.Fatalf("parseProjectModify: %v", err)
	}

	if mut.TaxonomyAction != taxonomyActionClear {
		test.Fatalf("action: got %v, want clear", mut.TaxonomyAction)
	}
}

func TestParseProjectModify_TaxonomyDisableInvalid(test *testing.T) {
	_, err := parseProjectModify([]string{"taxonomy.disable=maybe"})
	if err == nil {
		test.Fatal("expected error for taxonomy.disable=maybe")
	}
}

func TestParseProjectModify_TaxonomyJSONSet(test *testing.T) {
	mut, err := parseProjectModify([]string{`taxonomy={"ranks":[["milestone"],["story"],["task","spike"]]}`})

	if err != nil {
		test.Fatalf("parseProjectModify: %v", err)
	}

	if mut.TaxonomyAction != taxonomyActionSet {
		test.Fatalf("action: got %v, want set", mut.TaxonomyAction)
	}
	want := domain.Taxonomy{{"milestone"}, {"story"}, {"task", "spike"}}
	if !reflect.DeepEqual(mut.TaxonomyValue, want) {
		test.Fatalf("value: got %+v, want %+v", mut.TaxonomyValue, want)
	}
}

func TestParseProjectModify_TaxonomyJSONEmpty(test *testing.T) {
	mut, err := parseProjectModify([]string{`taxonomy={"ranks":[]}`})

	if err != nil {
		test.Fatalf("parseProjectModify: %v", err)
	}

	if mut.TaxonomyAction != taxonomyActionEmpty {
		test.Fatalf("action: got %v, want empty", mut.TaxonomyAction)
	}
}

func TestParseProjectModify_TaxonomyJSONMalformed(test *testing.T) {
	_, err := parseProjectModify([]string{`taxonomy=not-json`})
	if err == nil {
		test.Fatal("expected error for non-JSON taxonomy value")
	}
}

func TestParseProjectModify_TaxonomyMutualExclusion(test *testing.T) {
	_, err := parseProjectModify([]string{"taxonomy.levels=milestone", "taxonomy.disable=true"})
	if err == nil {
		test.Fatal("expected mutual-exclusion error")
	}
}

func TestParseProjectModify_TaxonomyModifierRejected(test *testing.T) {
	cases := [][]string{
		{"+taxonomy.levels=milestone"},
		{"-taxonomy.disable=true"},
	}
	for _, args := range cases {
		if _, err := parseProjectModify(args); err == nil {
			test.Fatalf("expected modifier rejection for %v", args)
		}
	}
}

func TestParseProjectModify_TaxonomyInvalidInline(test *testing.T) {
	_, err := parseProjectModify([]string{"taxonomy.levels=a::b"})
	if err == nil {
		test.Fatal("expected error for malformed inline taxonomy")
	}
}

func TestExpandProjectFieldRefs_TaxonomyFileExpansion(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "taxonomy.json")

	if writeErr := os.WriteFile(path, []byte(`{"ranks":[["milestone"],["story"]]}`), 0o644); writeErr != nil {
		test.Fatalf("writing file: %v", writeErr)
	}

	expanded, expandErr := expandProjectFieldRefs([]string{"taxonomy=@" + path}, nil, 1<<20)

	if expandErr != nil {
		test.Fatalf("expandProjectFieldRefs: %v", expandErr)
	}

	if len(expanded) != 1 {
		test.Fatalf("expected 1 arg, got %d", len(expanded))
	}

	mut, parseErr := parseProjectModify(expanded)

	if parseErr != nil {
		test.Fatalf("parseProjectModify: %v", parseErr)
	}

	if mut.TaxonomyAction != taxonomyActionSet {
		test.Fatalf("action: got %v, want set", mut.TaxonomyAction)
	}
	want := domain.Taxonomy{{"milestone"}, {"story"}}
	if !reflect.DeepEqual(mut.TaxonomyValue, want) {
		test.Fatalf("value: got %+v, want %+v", mut.TaxonomyValue, want)
	}
}
