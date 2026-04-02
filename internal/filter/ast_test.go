package filter

import (
	"testing"
)

func TestFilterSet_HasField(t *testing.T) {
	fs := &FilterSet{
		Fields: []FieldFilter{
			{Key: "status", Value: "active", Pos: 0},
			{Key: "priority", Value: "3", Pos: 14},
		},
	}

	if !fs.HasField("status") {
		t.Fatal("expected HasField(\"status\") to be true")
	}
	if !fs.HasField("priority") {
		t.Fatal("expected HasField(\"priority\") to be true")
	}
	if fs.HasField("project") {
		t.Fatal("expected HasField(\"project\") to be false")
	}
}

func TestFilterSet_GetField(t *testing.T) {
	fs := &FilterSet{
		Fields: []FieldFilter{
			{Key: "status", Value: "active", Pos: 0},
		},
	}

	f, ok := fs.GetField("status")
	if !ok {
		t.Fatal("expected GetField(\"status\") to return true")
	}
	if f.Value != "active" {
		t.Fatalf("expected Value=\"active\", got %q", f.Value)
	}

	_, ok = fs.GetField("project")
	if ok {
		t.Fatal("expected GetField(\"project\") to return false")
	}
}

func TestFilterSet_GetFieldDuplicateKeys(t *testing.T) {
	fs := &FilterSet{
		Fields: []FieldFilter{
			{Key: "status", Value: "active", Pos: 0},
			{Key: "status", Value: "pending", Pos: 14},
		},
	}

	// GetField returns the first match
	f, ok := fs.GetField("status")
	if !ok {
		t.Fatal("expected GetField(\"status\") to return true")
	}
	if f.Value != "active" {
		t.Fatalf("expected first match Value=\"active\", got %q", f.Value)
	}
}

func TestFilterSet_IncludeTags(t *testing.T) {
	fs := &FilterSet{
		Tags: []TagFilter{
			{Name: "api", Exclude: false, Pos: 0},
			{Name: "docs", Exclude: true, Pos: 5},
			{Name: "frontend", Exclude: false, Pos: 11},
		},
	}

	got := fs.IncludeTags()
	if len(got) != 2 || got[0] != "api" || got[1] != "frontend" {
		t.Fatalf("expected [api frontend], got %v", got)
	}

	// Verify Pos is preserved on tags
	if fs.Tags[0].Pos != 0 {
		t.Fatalf("expected Tags[0].Pos=0, got %d", fs.Tags[0].Pos)
	}
	if fs.Tags[2].Pos != 11 {
		t.Fatalf("expected Tags[2].Pos=11, got %d", fs.Tags[2].Pos)
	}
}

func TestFilterSet_ExcludeTags(t *testing.T) {
	fs := &FilterSet{
		Tags: []TagFilter{
			{Name: "api", Exclude: false},
			{Name: "docs", Exclude: true},
			{Name: "wip", Exclude: true},
		},
	}

	got := fs.ExcludeTags()
	if len(got) != 2 || got[0] != "docs" || got[1] != "wip" {
		t.Fatalf("expected [docs wip], got %v", got)
	}
}

func TestFilterSet_Title(t *testing.T) {
	fs := &FilterSet{
		Text: []string{"Implement", "auth", "middleware"},
	}

	got := fs.Title()
	if got != "Implement auth middleware" {
		t.Fatalf("expected %q, got %q", "Implement auth middleware", got)
	}
}

func TestFilterSet_TitleEmpty(t *testing.T) {
	fs := &FilterSet{}
	if fs.Title() != "" {
		t.Fatalf("expected empty string, got %q", fs.Title())
	}
}
