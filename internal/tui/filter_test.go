package tui

import (
	"testing"
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
