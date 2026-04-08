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

func TestParse_LexErrorsPropagated(t *testing.T) {
	// Bare "+" should produce a lex error that propagates through Parse
	_, errs := Parse("+ status:active")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error from bare +, got %d: %v", len(errs), errs)
	}
	if errs[0].Pos != 0 {
		t.Fatalf("expected error at pos 0 (bare +), got pos %d", errs[0].Pos)
	}
}

func TestParse_FieldWithEmptyValue(t *testing.T) {
	// "status:" has an empty value — validateStatus should reject it
	_, errs := Parse("status:")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for empty status value, got %d: %v", len(errs), errs)
	}
}

func TestParse_DuplicateFields(t *testing.T) {
	// Two status fields: both should be accepted (resolver decides how to handle)
	fs, errs := Parse("status:active status:pending")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	count := 0
	for _, f := range fs.Fields {
		if f.Key == "status" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 status fields, got %d", count)
	}
}

func TestParse_AllFieldTypes(t *testing.T) {
	input := "status:active project:backend priority:3 due:today parent:a3f8b2c1 tree:deadbeef waiting:true"
	fs, errs := Parse(input)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(fs.Fields) != 7 {
		t.Fatalf("expected 7 fields, got %d: %+v", len(fs.Fields), fs.Fields)
	}
	// Spot-check a few field values
	if f, ok := fs.GetField("waiting"); !ok || f.Value != "true" {
		t.Fatalf("expected waiting=true, got %+v", f)
	}
	if f, ok := fs.GetField("project"); !ok || f.Value != "backend" {
		t.Fatalf("expected project=backend, got %+v", f)
	}
}

func TestParse_MixedErrorsAndValid(t *testing.T) {
	// "foo:bar" is unknown, "priority:3" is valid, "waiting:yes" is invalid
	fs, errs := Parse("foo:bar priority:3 waiting:yes")
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}
	if len(fs.Fields) != 1 {
		t.Fatalf("expected 1 valid field, got %d", len(fs.Fields))
	}
	if fs.Fields[0].Key != "priority" {
		t.Fatalf("expected valid field to be priority, got %q", fs.Fields[0].Key)
	}
}

func TestParse_UDAField(t *testing.T) {
	fs, errs := Parse("uda.env:prod status:active")
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(fs.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fs.Fields))
	}
	f, ok := fs.GetField("uda.env")
	if !ok || f.Value != "prod" {
		t.Fatalf("expected uda.env=prod, got %+v", f)
	}
}

func TestParse_UDAFieldEmptyValue(t *testing.T) {
	fs, errs := Parse("uda.env:")
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	f, ok := fs.GetField("uda.env")
	if !ok || f.Value != "" {
		t.Fatalf("expected uda.env with empty value, got %+v ok=%v", f, ok)
	}
}

func TestParse_UDAFieldMultiple(t *testing.T) {
	fs, errs := Parse("uda.env:prod uda.team:backend")
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	count := 0
	for _, f := range fs.Fields {
		if f.Key == "uda.env" || f.Key == "uda.team" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 uda fields, got %d", count)
	}
}

func TestParse_UDAFieldInvalidKey(t *testing.T) {
	_, errs := Parse("uda.:value")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for empty UDA key name, got %d: %v", len(errs), errs)
	}
}

func TestParse_UDAFieldBadKeyChars(t *testing.T) {
	_, errs := Parse("uda.bad$key:value")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for invalid UDA key chars, got %d: %v", len(errs), errs)
	}
}

func TestParse_UDAMixedWithOtherFields(t *testing.T) {
	fs, errs := Parse("My task uda.env:prod priority:3 +api")
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if fs.Title() != "My task" {
		t.Fatalf("expected title 'My task', got %q", fs.Title())
	}
	if len(fs.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fs.Fields))
	}
	if len(fs.IncludeTags()) != 1 {
		t.Fatalf("expected 1 include tag, got %d", len(fs.IncludeTags()))
	}
}

func TestParse_QuotedTextTitle(t *testing.T) {
	fs, errs := Parse(`"My complex task"`)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if fs.Title() != "My complex task" {
		t.Fatalf("expected title %q, got %q", "My complex task", fs.Title())
	}
}

func TestParse_QuotedTextWithFields(t *testing.T) {
	fs, errs := Parse(`"My task" project:backend +api`)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if fs.Title() != "My task" {
		t.Fatalf("expected title %q, got %q", "My task", fs.Title())
	}
	if len(fs.Fields) != 1 || fs.Fields[0].Key != "project" {
		t.Fatalf("expected 1 field (project), got %+v", fs.Fields)
	}
	if len(fs.IncludeTags()) != 1 || fs.IncludeTags()[0] != "api" {
		t.Fatalf("expected include tags [api], got %v", fs.IncludeTags())
	}
}

func TestParse_TitleField(t *testing.T) {
	fs, errs := Parse(`title:"auth middleware"`)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	f, ok := fs.GetField("title")
	if !ok || f.Value != "auth middleware" {
		t.Fatalf("expected title=auth middleware, got %+v ok=%v", f, ok)
	}
}

func TestParse_DescriptionField(t *testing.T) {
	fs, errs := Parse(`description:"implement the feature"`)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	f, ok := fs.GetField("description")
	if !ok || f.Value != "implement the feature" {
		t.Fatalf("expected description=implement the feature, got %+v ok=%v", f, ok)
	}
}

func TestParse_TitleFieldEmpty(t *testing.T) {
	_, errs := Parse("title:")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for empty title, got %d: %v", len(errs), errs)
	}
}

func TestParse_DescriptionFieldEmpty(t *testing.T) {
	_, errs := Parse("description:")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for empty description, got %d: %v", len(errs), errs)
	}
}

func TestParse_KeywordsPreservedAsText(t *testing.T) {
	// Parse is for input building (tusk add/modify). Boolean keywords
	// must be preserved as title text, not silently dropped.
	tests := []struct {
		name  string
		input string
		title string
	}{
		{"AND in title", "Fix AND cleanup", "Fix AND cleanup"},
		{"OR in title", "Read OR write", "Read OR write"},
		{"NOT in title", "NOT a bug", "NOT a bug"},
		{"parens in title", "(draft) My task", "( draft ) My task"},
		{"mixed keywords", "Do AND OR NOT things", "Do AND OR NOT things"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs, errs := Parse(tt.input)
			if len(errs) != 0 {
				t.Fatalf("expected no errors, got %v", errs)
			}
			if fs.Title() != tt.title {
				t.Fatalf("expected title %q, got %q", tt.title, fs.Title())
			}
		})
	}
}
