package syntax

import "testing"

func TestFilterSet_Title(test *testing.T) {
	fs := &FilterSet{Text: []string{"My", "cool", "task"}}
	got := fs.Title()
	if got != "My cool task" {
		test.Errorf("Title() = %q, want %q", got, "My cool task")
	}
}

func TestFilterSet_HasField(test *testing.T) {
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "status", Value: "active"}},
	}
	if !fs.HasField("status") {
		test.Error("HasField(\"status\") = false, want true")
	}
	if fs.HasField("project") {
		test.Error("HasField(\"project\") = true, want false")
	}
}

func TestFilterSet_GetField(test *testing.T) {
	fs := &FilterSet{
		Fields: []FieldFilter{{Key: "priority", Value: "3"}},
	}
	found, ok := fs.GetField("priority")
	if !ok {
		test.Fatal("GetField(\"priority\") returned false")
	}
	if found.Value != "3" {
		test.Errorf("GetField(\"priority\").Value = %q, want %q", found.Value, "3")
	}
	_, ok = fs.GetField("due")
	if ok {
		test.Error("GetField(\"due\") returned true, want false")
	}
}

func TestFilterSet_Tags(test *testing.T) {
	fs := &FilterSet{
		Tags: []TagFilter{
			{Name: "api", Exclude: false},
			{Name: "docs", Exclude: true},
			{Name: "backend", Exclude: false},
		},
	}
	inc := fs.IncludeTags()
	if len(inc) != 2 || inc[0] != "api" || inc[1] != "backend" {
		test.Errorf("IncludeTags() = %v, want [api backend]", inc)
	}
	exc := fs.ExcludeTags()
	if len(exc) != 1 || exc[0] != "docs" {
		test.Errorf("ExcludeTags() = %v, want [docs]", exc)
	}
}

func TestFieldFilterModifierFieldExistsAndDefaultsToZero(test *testing.T) {
	field := FieldFilter{Key: "priority", Value: "3"}
	if field.Modifier != 0 {
		test.Errorf("zero-value FieldFilter.Modifier = %q, want 0", field.Modifier)
	}
}
