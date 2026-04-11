package syntax

import "testing"

func TestFilterSet_Title(t *testing.T) {
	fs := &FilterSet{Text: []string{"My", "cool", "task"}}
	got := fs.Title()
	if got != "My cool task" {
		t.Errorf("Title() = %q, want %q", got, "My cool task")
	}
}

func TestFilterSet_HasField(t *testing.T) {
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "status", Value: "active"}},
	}
	if !fs.HasField("status") {
		t.Error("HasField(\"status\") = false, want true")
	}
	if fs.HasField("project") {
		t.Error("HasField(\"project\") = true, want false")
	}
}

func TestFilterSet_GetField(t *testing.T) {
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "priority", Value: "3"}},
	}
	f, ok := fs.GetField("priority")
	if !ok {
		t.Fatal("GetField(\"priority\") returned false")
	}
	if f.Value != "3" {
		t.Errorf("GetField(\"priority\").Value = %q, want %q", f.Value, "3")
	}
	_, ok = fs.GetField("due")
	if ok {
		t.Error("GetField(\"due\") returned true, want false")
	}
}

func TestFilterSet_Tags(t *testing.T) {
	fs := &FilterSet{
		Tags: []TagFilter{
			{Name: "api", Exclude: false},
			{Name: "docs", Exclude: true},
			{Name: "backend", Exclude: false},
		},
	}
	inc := fs.IncludeTags()
	if len(inc) != 2 || inc[0] != "api" || inc[1] != "backend" {
		t.Errorf("IncludeTags() = %v, want [api backend]", inc)
	}
	exc := fs.ExcludeTags()
	if len(exc) != 1 || exc[0] != "docs" {
		t.Errorf("ExcludeTags() = %v, want [docs]", exc)
	}
}
