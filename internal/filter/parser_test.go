package filter

import (
	"testing"
)

func TestParse_EmptyInput(t *testing.T) {
	fs, errs := Parse("")
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(fs.Fields) != 0 || len(fs.Tags) != 0 || len(fs.Text) != 0 {
		t.Fatalf("expected empty FilterSet, got %+v", fs)
	}
}

func TestParse_TextOnly(t *testing.T) {
	fs, errs := Parse("Implement auth middleware")
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if fs.Title() != "Implement auth middleware" {
		t.Fatalf("expected title %q, got %q", "Implement auth middleware", fs.Title())
	}
	if len(fs.Fields) != 0 {
		t.Fatalf("expected no fields, got %+v", fs.Fields)
	}
}

func TestParse_FieldsOnly(t *testing.T) {
	fs, errs := Parse("status:active priority:3")
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(fs.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fs.Fields))
	}

	f, ok := fs.GetField("status")
	if !ok || f.Value != "active" {
		t.Fatalf("expected status=active, got %+v", f)
	}

	f, ok = fs.GetField("priority")
	if !ok || f.Value != "3" {
		t.Fatalf("expected priority=3, got %+v", f)
	}
}

func TestParse_TagsOnly(t *testing.T) {
	fs, errs := Parse("+api +frontend -docs")
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	inc := fs.IncludeTags()
	exc := fs.ExcludeTags()
	if len(inc) != 2 || inc[0] != "api" || inc[1] != "frontend" {
		t.Fatalf("expected include tags [api frontend], got %v", inc)
	}
	if len(exc) != 1 || exc[0] != "docs" {
		t.Fatalf("expected exclude tags [docs], got %v", exc)
	}
}

func TestParse_MixedInput(t *testing.T) {
	fs, errs := Parse("My task project:backend +api -docs priority:3")
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if fs.Title() != "My task" {
		t.Fatalf("expected title %q, got %q", "My task", fs.Title())
	}
	if len(fs.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fs.Fields))
	}
	if len(fs.IncludeTags()) != 1 || fs.IncludeTags()[0] != "api" {
		t.Fatalf("expected include tags [api], got %v", fs.IncludeTags())
	}
	if len(fs.ExcludeTags()) != 1 || fs.ExcludeTags()[0] != "docs" {
		t.Fatalf("expected exclude tags [docs], got %v", fs.ExcludeTags())
	}
}

func TestParse_UnknownField(t *testing.T) {
	fs, errs := Parse("foo:bar status:active")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "foo" {
		t.Fatalf("expected error for field \"foo\", got %q", errs[0].Field)
	}
	// Valid field should still be parsed
	if len(fs.Fields) != 1 {
		t.Fatalf("expected 1 valid field, got %d", len(fs.Fields))
	}
}

func TestParse_InvalidFieldValue(t *testing.T) {
	fs, errs := Parse("priority:xyz status:active")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "priority" {
		t.Fatalf("expected error for field \"priority\", got %q", errs[0].Field)
	}
	// Valid field should still be parsed
	if len(fs.Fields) != 1 {
		t.Fatalf("expected 1 valid field, got %d", len(fs.Fields))
	}
}

func TestParse_MultipleErrors(t *testing.T) {
	_, errs := Parse("foo:bar priority:xyz baz:qux")
	if len(errs) != 3 {
		t.Fatalf("expected 3 errors, got %d: %v", len(errs), errs)
	}
}

func TestParse_FieldPosition(t *testing.T) {
	fs, errs := Parse("status:active priority:3")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	// "status:active" starts at 0, "priority:3" starts at 14
	if fs.Fields[0].Pos != 0 {
		t.Fatalf("expected Fields[0].Pos=0, got %d", fs.Fields[0].Pos)
	}
	if fs.Fields[1].Pos != 14 {
		t.Fatalf("expected Fields[1].Pos=14, got %d", fs.Fields[1].Pos)
	}
}

func TestParse_PriorityRange(t *testing.T) {
	fs, errs := Parse("priority:2..4")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	f, ok := fs.GetField("priority")
	if !ok || f.Value != "2..4" {
		t.Fatalf("expected priority=2..4, got %+v", f)
	}
}

func TestParse_DueRange(t *testing.T) {
	fs, errs := Parse("due:today..friday")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	f, ok := fs.GetField("due")
	if !ok || f.Value != "today..friday" {
		t.Fatalf("expected due=today..friday, got %+v", f)
	}
}

func TestParse_CommaStatuses(t *testing.T) {
	fs, errs := Parse("status:pending,active,completed")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	f, ok := fs.GetField("status")
	if !ok || f.Value != "pending,active,completed" {
		t.Fatalf("expected status=pending,active,completed, got %+v", f)
	}
}
