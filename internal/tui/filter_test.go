package tui

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
)

func TestParseArgs_TitleOnly(t *testing.T) {
	got := parseArgs([]string{"Implement", "auth", "middleware"})
	if got.Title != "Implement auth middleware" {
		t.Fatalf("expected title 'Implement auth middleware', got %q", got.Title)
	}
	if len(got.Fields) != 0 {
		t.Fatalf("expected no fields, got %d", len(got.Fields))
	}
	if len(got.Tags) != 0 {
		t.Fatalf("expected no tags, got %d", len(got.Tags))
	}
}

func TestParseArgs_KeyValuePairs(t *testing.T) {
	got := parseArgs([]string{"My", "task", "project:backend", "priority:3"})
	if got.Title != "My task" {
		t.Fatalf("expected title 'My task', got %q", got.Title)
	}
	if got.Fields["project"] != "backend" {
		t.Fatalf("expected project=backend, got %q", got.Fields["project"])
	}
	if got.Fields["priority"] != "3" {
		t.Fatalf("expected priority=3, got %q", got.Fields["priority"])
	}
}

func TestParseArgs_Tags(t *testing.T) {
	got := parseArgs([]string{"My", "task", "+api", "+frontend", "-docs"})
	if got.Title != "My task" {
		t.Fatalf("expected title 'My task', got %q", got.Title)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "api" || got.Tags[1] != "frontend" {
		t.Fatalf("expected tags [api frontend], got %v", got.Tags)
	}
	if len(got.ExclTags) != 1 || got.ExclTags[0] != "docs" {
		t.Fatalf("expected excl tags [docs], got %v", got.ExclTags)
	}
}

func TestParseArgs_AllMixed(t *testing.T) {
	got := parseArgs([]string{"Build", "the", "feature", "project:backend", "+api", "-docs", "priority:3"})
	if got.Title != "Build the feature" {
		t.Fatalf("expected title 'Build the feature', got %q", got.Title)
	}
	if got.Fields["project"] != "backend" {
		t.Fatalf("expected project=backend, got %q", got.Fields["project"])
	}
	if got.Fields["priority"] != "3" {
		t.Fatalf("expected priority=3, got %q", got.Fields["priority"])
	}
	if len(got.Tags) != 1 || got.Tags[0] != "api" {
		t.Fatalf("expected tags [api], got %v", got.Tags)
	}
	if len(got.ExclTags) != 1 || got.ExclTags[0] != "docs" {
		t.Fatalf("expected excl tags [docs], got %v", got.ExclTags)
	}
}

func TestParseArgs_Empty(t *testing.T) {
	got := parseArgs([]string{})
	if got.Title != "" {
		t.Fatalf("expected empty title, got %q", got.Title)
	}
	if len(got.Fields) != 0 {
		t.Fatalf("expected no fields, got %d", len(got.Fields))
	}
}

func TestParseArgs_ColonInValue(t *testing.T) {
	got := parseArgs([]string{"title:has:colons"})
	if got.Fields["title"] != "has:colons" {
		t.Fatalf("expected 'has:colons', got %q", got.Fields["title"])
	}
}

func TestParsePriority_Numeric(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"0", 0},
		{"1", 1},
		{"2", 2},
		{"3", 3},
		{"4", 4},
	}
	for _, tt := range tests {
		got, err := parsePriority(tt.input)
		if err != nil {
			t.Fatalf("parsePriority(%q): %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("parsePriority(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParsePriority_Named(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"none", 0},
		{"low", 1},
		{"medium", 2},
		{"high", 3},
		{"urgent", 4},
		{"None", 0},
		{"HIGH", 3},
	}
	for _, tt := range tests {
		got, err := parsePriority(tt.input)
		if err != nil {
			t.Fatalf("parsePriority(%q): %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("parsePriority(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParsePriority_Invalid(t *testing.T) {
	for _, input := range []string{"5", "-1", "critical", "abc"} {
		_, err := parsePriority(input)
		if err == nil {
			t.Fatalf("parsePriority(%q): expected error", input)
		}
	}
}

func TestParseDate_RFC3339(t *testing.T) {
	got, err := parseDate("2026-04-10T15:30:00Z")
	if err != nil {
		t.Fatalf("parseDate: %v", err)
	}
	want := time.Date(2026, 4, 10, 15, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseDate_DateOnly(t *testing.T) {
	got, err := parseDate("2026-04-10")
	if err != nil {
		t.Fatalf("parseDate: %v", err)
	}
	want := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseDate_Today(t *testing.T) {
	got, err := parseDate("today")
	if err != nil {
		t.Fatalf("parseDate: %v", err)
	}
	now := time.Now().UTC()
	want := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseDate_Tomorrow(t *testing.T) {
	got, err := parseDate("tomorrow")
	if err != nil {
		t.Fatalf("parseDate: %v", err)
	}
	now := time.Now().UTC().AddDate(0, 0, 1)
	want := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseDate_Weekday(t *testing.T) {
	// "monday" should return the next Monday from today
	got, err := parseDate("monday")
	if err != nil {
		t.Fatalf("parseDate: %v", err)
	}
	if got.Weekday() != time.Monday {
		t.Fatalf("expected Monday, got %s", got.Weekday())
	}
	if got.Before(time.Now().UTC().Truncate(24 * time.Hour)) {
		t.Fatal("expected date in the future")
	}
}

func TestParseDate_Invalid(t *testing.T) {
	_, err := parseDate("notadate")
	if err == nil {
		t.Fatal("expected error for invalid date")
	}
}

// testProjectRepo creates an in-memory SQLite store and returns its ProjectRepo.
func testProjectRepo(t *testing.T) *sqlite.ProjectRepo {
	t.Helper()
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return sqlite.NewProjectRepo(store.DB())
}

func TestBuildTaskFilter_DefaultStatuses(t *testing.T) {
	repo := testProjectRepo(t)
	p := ParsedArgs{Fields: map[string]string{}}

	filter, err := buildTaskFilter(context.Background(), p, repo)
	if err != nil {
		t.Fatalf("buildTaskFilter: %v", err)
	}
	if len(filter.Statuses) != 2 {
		t.Fatalf("expected 2 default statuses, got %d", len(filter.Statuses))
	}
	if filter.Statuses[0] != "pending" || filter.Statuses[1] != "active" {
		t.Fatalf("expected [pending active], got %v", filter.Statuses)
	}
}

func TestBuildTaskFilter_ExplicitStatus(t *testing.T) {
	repo := testProjectRepo(t)
	p := ParsedArgs{
		Fields: map[string]string{"status": "completed"},
	}

	filter, err := buildTaskFilter(context.Background(), p, repo)
	if err != nil {
		t.Fatalf("buildTaskFilter: %v", err)
	}
	if len(filter.Statuses) != 1 || filter.Statuses[0] != "completed" {
		t.Fatalf("expected [completed], got %v", filter.Statuses)
	}
}

func TestBuildTaskFilter_MultipleStatuses(t *testing.T) {
	repo := testProjectRepo(t)
	p := ParsedArgs{
		Fields: map[string]string{"status": "pending,active,completed"},
	}

	filter, err := buildTaskFilter(context.Background(), p, repo)
	if err != nil {
		t.Fatalf("buildTaskFilter: %v", err)
	}
	if len(filter.Statuses) != 3 {
		t.Fatalf("expected 3 statuses, got %d", len(filter.Statuses))
	}
}

func TestBuildTaskFilter_ProjectByName(t *testing.T) {
	repo := testProjectRepo(t)
	p := ParsedArgs{
		Fields: map[string]string{"project": "_default"},
	}

	filter, err := buildTaskFilter(context.Background(), p, repo)
	if err != nil {
		t.Fatalf("buildTaskFilter: %v", err)
	}
	if filter.ProjectID == nil {
		t.Fatal("expected ProjectID to be set")
	}
}

func TestBuildTaskFilter_ProjectNotFound(t *testing.T) {
	repo := testProjectRepo(t)
	p := ParsedArgs{
		Fields: map[string]string{"project": "nonexistent"},
	}

	_, err := buildTaskFilter(context.Background(), p, repo)
	if err == nil {
		t.Fatal("expected error for nonexistent project")
	}
}

func TestBuildTaskFilter_PriorityRange(t *testing.T) {
	repo := testProjectRepo(t)
	p := ParsedArgs{
		Fields: map[string]string{"priority": "2..4"},
	}

	filter, err := buildTaskFilter(context.Background(), p, repo)
	if err != nil {
		t.Fatalf("buildTaskFilter: %v", err)
	}
	if filter.PriorityMin == nil || *filter.PriorityMin != 2 {
		t.Fatalf("expected PriorityMin=2, got %v", filter.PriorityMin)
	}
	if filter.PriorityMax == nil || *filter.PriorityMax != 4 {
		t.Fatalf("expected PriorityMax=4, got %v", filter.PriorityMax)
	}
}

func TestBuildTaskFilter_PrioritySingle(t *testing.T) {
	repo := testProjectRepo(t)
	p := ParsedArgs{
		Fields: map[string]string{"priority": "3"},
	}

	filter, err := buildTaskFilter(context.Background(), p, repo)
	if err != nil {
		t.Fatalf("buildTaskFilter: %v", err)
	}
	if filter.PriorityMin == nil || *filter.PriorityMin != 3 {
		t.Fatalf("expected PriorityMin=3, got %v", filter.PriorityMin)
	}
	if filter.PriorityMax == nil || *filter.PriorityMax != 3 {
		t.Fatalf("expected PriorityMax=3, got %v", filter.PriorityMax)
	}
}

func TestBuildTaskFilter_Tags(t *testing.T) {
	repo := testProjectRepo(t)
	p := ParsedArgs{
		Fields: map[string]string{},
		Tags:   []string{"api"},
	}

	_, err := buildTaskFilter(context.Background(), p, repo)
	if err == nil || err.Error() != "tag filtering not yet supported" {
		t.Fatalf("expected 'tag filtering not yet supported' error, got %v", err)
	}
}

func TestBuildTaskFilter_ExclTags(t *testing.T) {
	repo := testProjectRepo(t)
	p := ParsedArgs{
		Fields:   map[string]string{},
		ExclTags: []string{"docs"},
	}

	_, err := buildTaskFilter(context.Background(), p, repo)
	if err == nil || err.Error() != "tag filtering not yet supported" {
		t.Fatalf("expected 'tag filtering not yet supported' error, got %v", err)
	}
}
