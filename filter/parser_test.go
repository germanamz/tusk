package filter

import (
	"testing"
)

func TestParse_EmptyInput(test *testing.T) {
	fs, errs := Parse("")
	if len(errs) != 0 {
		test.Fatalf("expected no errors, got %v", errs)
	}
	if len(fs.Fields) != 0 || len(fs.Tags) != 0 || len(fs.Text) != 0 {
		test.Fatalf("expected empty FilterSet, got %+v", fs)
	}
}

func TestParse_TextOnly(test *testing.T) {
	fs, errs := Parse("Implement auth middleware")
	if len(errs) != 0 {
		test.Fatalf("expected no errors, got %v", errs)
	}
	if fs.Title() != "Implement auth middleware" {
		test.Fatalf("expected title %q, got %q", "Implement auth middleware", fs.Title())
	}
	if len(fs.Fields) != 0 {
		test.Fatalf("expected no fields, got %+v", fs.Fields)
	}
}

func TestParse_FieldsOnly(test *testing.T) {
	fs, errs := Parse("status=active priority=3")
	if len(errs) != 0 {
		test.Fatalf("expected no errors, got %v", errs)
	}
	if len(fs.Fields) != 2 {
		test.Fatalf("expected 2 fields, got %d", len(fs.Fields))
	}

	field, ok := fs.GetField("status")
	if !ok || field.Value != "active" {
		test.Fatalf("expected status=active, got %+v", field)
	}

	field, ok = fs.GetField("priority")
	if !ok || field.Value != "3" {
		test.Fatalf("expected priority=3, got %+v", field)
	}
}

func TestParse_OrderSingle(test *testing.T) {
	fs, errs := Parse("order=2.5")
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	field, ok := fs.GetField("order")
	if !ok || field.Value != "2.5" {
		test.Fatalf("expected order=2.5, got %+v", field)
	}
}

func TestParse_OrderRange(test *testing.T) {
	fs, errs := Parse("order=1..5")
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	field, ok := fs.GetField("order")
	if !ok || field.Value != "1..5" {
		test.Fatalf("expected order=1..5, got %+v", field)
	}
}

func TestParse_OrderEmpty(test *testing.T) {
	fs, errs := Parse("order=")
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	field, ok := fs.GetField("order")
	if !ok || field.Value != "" {
		test.Fatalf("expected order= (empty), got %+v", field)
	}
}

func TestParse_OrderInvalidValue(test *testing.T) {
	_, errs := Parse("order=notanumber")
	if len(errs) == 0 {
		test.Fatal("expected parse error for non-numeric order value")
	}
}

func TestParse_OrderBadRange(test *testing.T) {
	_, errs := Parse("order=5..1")
	if len(errs) == 0 {
		test.Fatal("expected parse error when order range min > max")
	}
}

func TestParse_TagsOnly(test *testing.T) {
	fs, errs := Parse("+api +frontend -docs")
	if len(errs) != 0 {
		test.Fatalf("expected no errors, got %v", errs)
	}
	inc := fs.IncludeTags()
	exc := fs.ExcludeTags()
	if len(inc) != 2 || inc[0] != "api" || inc[1] != "frontend" {
		test.Fatalf("expected include tags [api frontend], got %v", inc)
	}
	if len(exc) != 1 || exc[0] != "docs" {
		test.Fatalf("expected exclude tags [docs], got %v", exc)
	}
}

func TestParse_MixedInput(test *testing.T) {
	fs, errs := Parse("My task project=backend +api -docs priority=3")
	if len(errs) != 0 {
		test.Fatalf("expected no errors, got %v", errs)
	}
	if fs.Title() != "My task" {
		test.Fatalf("expected title %q, got %q", "My task", fs.Title())
	}
	if len(fs.Fields) != 2 {
		test.Fatalf("expected 2 fields, got %d", len(fs.Fields))
	}
	if len(fs.IncludeTags()) != 1 || fs.IncludeTags()[0] != "api" {
		test.Fatalf("expected include tags [api], got %v", fs.IncludeTags())
	}
	if len(fs.ExcludeTags()) != 1 || fs.ExcludeTags()[0] != "docs" {
		test.Fatalf("expected exclude tags [docs], got %v", fs.ExcludeTags())
	}
}

func TestParse_UnknownField(test *testing.T) {
	fs, errs := Parse("foo=bar status=active")
	if len(errs) != 1 {
		test.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "foo" {
		test.Fatalf("expected error for field \"foo\", got %q", errs[0].Field)
	}
	// Valid field should still be parsed
	if len(fs.Fields) != 1 {
		test.Fatalf("expected 1 valid field, got %d", len(fs.Fields))
	}
}

func TestParse_InvalidFieldValue(test *testing.T) {
	fs, errs := Parse("priority=xyz status=active")
	if len(errs) != 1 {
		test.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "priority" {
		test.Fatalf("expected error for field \"priority\", got %q", errs[0].Field)
	}
	// Valid field should still be parsed
	if len(fs.Fields) != 1 {
		test.Fatalf("expected 1 valid field, got %d", len(fs.Fields))
	}
}

func TestParse_MultipleErrors(test *testing.T) {
	_, errs := Parse("foo=bar priority=xyz baz=qux")
	if len(errs) != 3 {
		test.Fatalf("expected 3 errors, got %d: %v", len(errs), errs)
	}
}

func TestParse_FieldPosition(test *testing.T) {
	fs, errs := Parse("status=active priority=3")
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	// "status=active" starts at 0, "priority=3" starts at 14
	if fs.Fields[0].Pos != 0 {
		test.Fatalf("expected Fields[0].Pos=0, got %d", fs.Fields[0].Pos)
	}
	if fs.Fields[1].Pos != 14 {
		test.Fatalf("expected Fields[1].Pos=14, got %d", fs.Fields[1].Pos)
	}
}

func TestParse_PriorityRange(test *testing.T) {
	fs, errs := Parse("priority=2..4")
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	field, ok := fs.GetField("priority")
	if !ok || field.Value != "2..4" {
		test.Fatalf("expected priority=2..4, got %+v", field)
	}
}

func TestParse_DueRange(test *testing.T) {
	fs, errs := Parse("due=today..friday")
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	field, ok := fs.GetField("due")
	if !ok || field.Value != "today..friday" {
		test.Fatalf("expected due=today..friday, got %+v", field)
	}
}

func TestParse_CommaStatuses(test *testing.T) {
	fs, errs := Parse("status=pending,active,completed")
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	field, ok := fs.GetField("status")
	if !ok || field.Value != "pending,active,completed" {
		test.Fatalf("expected status=pending,active,completed, got %+v", field)
	}
}

func TestParse_LexErrorsPropagated(test *testing.T) {
	// Bare "+" should produce a lex error that propagates through Parse
	_, errs := Parse("+ status=active")
	if len(errs) != 1 {
		test.Fatalf("expected 1 error from bare +, got %d: %v", len(errs), errs)
	}
	if errs[0].Pos != 0 {
		test.Fatalf("expected error at pos 0 (bare +), got pos %d", errs[0].Pos)
	}
}

func TestParse_FieldWithEmptyValue(test *testing.T) {
	// "status=" has an empty value — validateStatus should reject it
	_, errs := Parse("status=")
	if len(errs) != 1 {
		test.Fatalf("expected 1 error for empty status value, got %d: %v", len(errs), errs)
	}
}

func TestParse_DuplicateFields(test *testing.T) {
	// Two status fields: both should be accepted (resolver decides how to handle)
	fs, errs := Parse("status=active status=pending")
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	count := 0
	for _, field := range fs.Fields {
		if field.Key == "status" {
			count++
		}
	}
	if count != 2 {
		test.Fatalf("expected 2 status fields, got %d", count)
	}
}

func TestParse_AllFieldTypes(test *testing.T) {
	input := "status=active project=backend priority=3 due=today parent=a3f8b2c1 tree=deadbeef waiting=true"
	fs, errs := Parse(input)
	if len(errs) != 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if len(fs.Fields) != 7 {
		test.Fatalf("expected 7 fields, got %d: %+v", len(fs.Fields), fs.Fields)
	}
	// Spot-check a few field values
	if field, ok := fs.GetField("waiting"); !ok || field.Value != "true" {
		test.Fatalf("expected waiting=true, got %+v", field)
	}
	if field, ok := fs.GetField("project"); !ok || field.Value != "backend" {
		test.Fatalf("expected project=backend, got %+v", field)
	}
}

func TestParse_MixedErrorsAndValid(test *testing.T) {
	// "foo=bar" is unknown, "priority=3" is valid, "waiting=yes" is invalid
	fs, errs := Parse("foo=bar priority=3 waiting=yes")
	if len(errs) != 2 {
		test.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}
	if len(fs.Fields) != 1 {
		test.Fatalf("expected 1 valid field, got %d", len(fs.Fields))
	}
	if fs.Fields[0].Key != "priority" {
		test.Fatalf("expected valid field to be priority, got %q", fs.Fields[0].Key)
	}
}

func TestParse_UDAField(test *testing.T) {
	fs, errs := Parse("uda.env=prod status=active")
	if len(errs) != 0 {
		test.Fatalf("expected no errors, got %v", errs)
	}
	if len(fs.Fields) != 2 {
		test.Fatalf("expected 2 fields, got %d", len(fs.Fields))
	}
	field, ok := fs.GetField("uda.env")
	if !ok || field.Value != "prod" {
		test.Fatalf("expected uda.env=prod, got %+v", field)
	}
}

func TestParse_UDAFieldEmptyValue(test *testing.T) {
	fs, errs := Parse("uda.env=")
	if len(errs) != 0 {
		test.Fatalf("expected no errors, got %v", errs)
	}
	field, ok := fs.GetField("uda.env")
	if !ok || field.Value != "" {
		test.Fatalf("expected uda.env with empty value, got %+v ok=%v", field, ok)
	}
}

func TestParse_UDAFieldMultiple(test *testing.T) {
	fs, errs := Parse("uda.env=prod uda.team=backend")
	if len(errs) != 0 {
		test.Fatalf("expected no errors, got %v", errs)
	}
	count := 0
	for _, field := range fs.Fields {
		if field.Key == "uda.env" || field.Key == "uda.team" {
			count++
		}
	}
	if count != 2 {
		test.Fatalf("expected 2 uda fields, got %d", count)
	}
}

func TestParse_UDAFieldInvalidKey(test *testing.T) {
	_, errs := Parse("uda.=value")
	if len(errs) != 1 {
		test.Fatalf("expected 1 error for empty UDA key name, got %d: %v", len(errs), errs)
	}
}

func TestParse_UDAFieldBadKeyChars(test *testing.T) {
	_, errs := Parse("uda.bad$key=value")
	if len(errs) != 1 {
		test.Fatalf("expected 1 error for invalid UDA key chars, got %d: %v", len(errs), errs)
	}
}

func TestParse_UDAMixedWithOtherFields(test *testing.T) {
	fs, errs := Parse("My task uda.env=prod priority=3 +api")
	if len(errs) != 0 {
		test.Fatalf("expected no errors, got %v", errs)
	}
	if fs.Title() != "My task" {
		test.Fatalf("expected title 'My task', got %q", fs.Title())
	}
	if len(fs.Fields) != 2 {
		test.Fatalf("expected 2 fields, got %d", len(fs.Fields))
	}
	if len(fs.IncludeTags()) != 1 {
		test.Fatalf("expected 1 include tag, got %d", len(fs.IncludeTags()))
	}
}

func TestParse_QuotedTextTitle(test *testing.T) {
	fs, errs := Parse(`"My complex task"`)
	if len(errs) != 0 {
		test.Fatalf("expected no errors, got %v", errs)
	}
	if fs.Title() != "My complex task" {
		test.Fatalf("expected title %q, got %q", "My complex task", fs.Title())
	}
}

func TestParse_QuotedTextWithFields(test *testing.T) {
	fs, errs := Parse(`"My task" project=backend +api`)
	if len(errs) != 0 {
		test.Fatalf("expected no errors, got %v", errs)
	}
	if fs.Title() != "My task" {
		test.Fatalf("expected title %q, got %q", "My task", fs.Title())
	}
	if len(fs.Fields) != 1 || fs.Fields[0].Key != "project" {
		test.Fatalf("expected 1 field (project), got %+v", fs.Fields)
	}
	if len(fs.IncludeTags()) != 1 || fs.IncludeTags()[0] != "api" {
		test.Fatalf("expected include tags [api], got %v", fs.IncludeTags())
	}
}

func TestParse_TitleField(test *testing.T) {
	fs, errs := Parse(`title="auth middleware"`)
	if len(errs) != 0 {
		test.Fatalf("expected no errors, got %v", errs)
	}
	field, ok := fs.GetField("title")
	if !ok || field.Value != "auth middleware" {
		test.Fatalf("expected title=auth middleware, got %+v ok=%v", field, ok)
	}
}

func TestParse_DescriptionField(test *testing.T) {
	fs, errs := Parse(`description="implement the feature"`)
	if len(errs) != 0 {
		test.Fatalf("expected no errors, got %v", errs)
	}
	field, ok := fs.GetField("description")
	if !ok || field.Value != "implement the feature" {
		test.Fatalf("expected description=implement the feature, got %+v ok=%v", field, ok)
	}
}

func TestParse_TitleFieldEmpty(test *testing.T) {
	_, errs := Parse("title=")
	if len(errs) != 1 {
		test.Fatalf("expected 1 error for empty title, got %d: %v", len(errs), errs)
	}
}

func TestParse_DescriptionFieldEmpty(test *testing.T) {
	// `description=` is accepted by the parser — an empty value is the
	// documented clear signal on `tusk task modify`. The runModify command
	// interprets it as a double-pointer clear; runCreate treats it as a
	// no-op (creating a task with an empty description).
	fs, errs := Parse("description=")
	if len(errs) != 0 {
		test.Fatalf("expected no errors for empty description, got: %v", errs)
	}
	field, ok := fs.GetField("description")
	if !ok {
		test.Fatal("expected description field in result")
	}
	if field.Value != "" {
		test.Fatalf("expected empty value, got %q", field.Value)
	}
}

func TestParse_KeywordsPreservedAsText(test *testing.T) {
	// Parse is for input building (tusk add/modify). Boolean keywords
	// must be preserved as title text, not silently dropped.
	cases := []struct {
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
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			fs, errs := Parse(testCase.input)
			if len(errs) != 0 {
				test.Fatalf("expected no errors, got %v", errs)
			}
			if fs.Title() != testCase.title {
				test.Fatalf("expected title %q, got %q", testCase.title, fs.Title())
			}
		})
	}
}

func TestParseFieldCarriesPlusModifier(test *testing.T) {
	fs, errs := Parse("+priority=3")
	if len(errs) > 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if len(fs.Fields) != 1 {
		test.Fatalf("expected 1 field, got %d", len(fs.Fields))
	}
	field := fs.Fields[0]
	if field.Key != "priority" || field.Value != "3" || field.Modifier != '+' {
		test.Errorf("field = %+v", field)
	}
}

func TestParseFieldCarriesMinusModifier(test *testing.T) {
	fs, errs := Parse("-priority=2")
	if len(errs) > 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if len(fs.Fields) != 1 || fs.Fields[0].Modifier != '-' {
		test.Fatalf("expected Modifier='-', got %+v", fs.Fields)
	}
}

func TestParseBareFieldHasZeroModifier(test *testing.T) {
	fs, _ := Parse("priority=3")
	if len(fs.Fields) != 1 || fs.Fields[0].Modifier != 0 {
		test.Fatalf("expected zero modifier, got %+v", fs.Fields)
	}
}

func TestParseTagRoundTrip(test *testing.T) {
	fs, errs := Parse("+urgent -blocked")
	if len(errs) > 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if len(fs.Tags) != 2 {
		test.Fatalf("expected 2 tags, got %d", len(fs.Tags))
	}
	if fs.Tags[0].Name != "urgent" || fs.Tags[0].Exclude {
		test.Errorf("tag[0] = %+v", fs.Tags[0])
	}
	if fs.Tags[1].Name != "blocked" || !fs.Tags[1].Exclude {
		test.Errorf("tag[1] = %+v", fs.Tags[1])
	}
}
