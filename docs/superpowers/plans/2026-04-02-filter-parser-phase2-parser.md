# Filter Parser Phase 2: AST and Table-Driven Parser

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the AST types and table-driven parser that validates tokens from the lexer and produces a structured `FilterSet`.

**Architecture:** The parser takes the token list from Phase 1's `Lex()` function and produces an AST (`FilterSet` containing `FieldFilter`, `TagFilter`, and free text). A dispatch table maps field names to validator functions. The parser never short-circuits — it processes every token and collects all errors. No external dependencies.

**Tech Stack:** Go standard library only. Module: `github.com/germanamz/tusk`.

**Spec:** `docs/superpowers/specs/2026-04-02-filter-syntax-parser-design.md`

**Depends on:** Phase 1 (lexer, tokens, errors) must be complete. The `internal/filter` package must have `token.go` (with `Lex`, `Token`, `TokenType`) and `errors.go` (with `ParseError`).

---

### Task 1: AST Node Types

**Files:**
- Create: `internal/filter/ast.go`
- Test: `internal/filter/ast_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/filter/ast_test.go`:

```go
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

func TestFilterSet_IncludeTags(t *testing.T) {
	fs := &FilterSet{
		Tags: []TagFilter{
			{Name: "api", Exclude: false},
			{Name: "docs", Exclude: true},
			{Name: "frontend", Exclude: false},
		},
	}

	got := fs.IncludeTags()
	if len(got) != 2 || got[0] != "api" || got[1] != "frontend" {
		t.Fatalf("expected [api frontend], got %v", got)
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -run TestFilterSet -v`
Expected: FAIL — `FilterSet` type not defined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/filter/ast.go`:

```go
package filter

import "strings"

// FilterSet is the root AST node — a collection of filter terms, implicitly AND'd.
// Designed to later wrap a BoolExpr node for OR/NOT/grouping.
type FilterSet struct {
	Fields []FieldFilter
	Tags   []TagFilter
	Text   []string // free text tokens (joined as title when used in add)
}

// HasField returns true if the FilterSet contains a field with the given key.
func (fs *FilterSet) HasField(key string) bool {
	for _, f := range fs.Fields {
		if f.Key == key {
			return true
		}
	}
	return false
}

// GetField returns the first FieldFilter with the given key, or false if not found.
func (fs *FilterSet) GetField(key string) (FieldFilter, bool) {
	for _, f := range fs.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return FieldFilter{}, false
}

// IncludeTags returns the names of all non-excluded tags.
func (fs *FilterSet) IncludeTags() []string {
	var out []string
	for _, t := range fs.Tags {
		if !t.Exclude {
			out = append(out, t.Name)
		}
	}
	return out
}

// ExcludeTags returns the names of all excluded tags.
func (fs *FilterSet) ExcludeTags() []string {
	var out []string
	for _, t := range fs.Tags {
		if t.Exclude {
			out = append(out, t.Name)
		}
	}
	return out
}

// Title joins free text tokens into a single string.
func (fs *FilterSet) Title() string {
	return strings.Join(fs.Text, " ")
}

// FieldFilter represents a key:value or key:min..max term.
type FieldFilter struct {
	Key   string // "status", "project", "priority", "due", "parent", "tree", "waiting"
	Value string // raw value string, unparsed
	Pos   int    // byte offset in input
}

// TagFilter represents +tag or -tag.
type TagFilter struct {
	Name    string
	Exclude bool // true for -tag, false for +tag
	Pos     int
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -run TestFilterSet -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/germanamz/projects/tusk
git add internal/filter/ast.go internal/filter/ast_test.go
git commit -m "feat(filter): add AST node types (FilterSet, FieldFilter, TagFilter)"
```

---

### Task 2: Field Validators

**Files:**
- Create: `internal/filter/validators.go`
- Create: `internal/filter/validators_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/filter/validators_test.go`:

```go
package filter

import (
	"testing"
)

func TestValidateStatus(t *testing.T) {
	valid := []string{"active", "pending", "pending,active", "pending,active,completed"}
	for _, v := range valid {
		if err := validateStatus(v); err != nil {
			t.Errorf("validateStatus(%q) unexpected error: %v", v, err)
		}
	}

	invalid := []string{"", ",", ",active"}
	for _, v := range invalid {
		if err := validateStatus(v); err == nil {
			t.Errorf("validateStatus(%q) expected error", v)
		}
	}
}

func TestValidateProject(t *testing.T) {
	if err := validateProject("backend"); err != nil {
		t.Errorf("validateProject(\"backend\") unexpected error: %v", err)
	}
	if err := validateProject(""); err == nil {
		t.Error("validateProject(\"\") expected error")
	}
}

func TestValidatePriority(t *testing.T) {
	valid := []string{"0", "1", "2", "3", "4", "none", "low", "medium", "high", "urgent", "2..4", "low..high"}
	for _, v := range valid {
		if err := validatePriority(v); err != nil {
			t.Errorf("validatePriority(%q) unexpected error: %v", v, err)
		}
	}

	invalid := []string{"", "5", "-1", "critical", "abc", "5..6", "high..low"}
	for _, v := range invalid {
		if err := validatePriority(v); err == nil {
			t.Errorf("validatePriority(%q) expected error", v)
		}
	}
}

func TestValidateDue(t *testing.T) {
	valid := []string{
		"2026-04-10",
		"2026-04-10T15:30:00Z",
		"today",
		"tomorrow",
		"thisweek",
		"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday",
		"today..friday",
		"2026-04-01..2026-04-10",
	}
	for _, v := range valid {
		if err := validateDue(v); err != nil {
			t.Errorf("validateDue(%q) unexpected error: %v", v, err)
		}
	}

	invalid := []string{"", "notadate", "13-13-2026", "..friday", "today.."}
	for _, v := range invalid {
		if err := validateDue(v); err == nil {
			t.Errorf("validateDue(%q) expected error", v)
		}
	}
}

func TestValidateShortID(t *testing.T) {
	valid := []string{"a3f8b2c1", "DEADBEEF", "abcd1234", "abcdef012"}
	for _, v := range valid {
		if err := validateShortID(v); err != nil {
			t.Errorf("validateShortID(%q) unexpected error: %v", v, err)
		}
	}

	invalid := []string{"", "xyz!", "ab", "not-hex!!"}
	for _, v := range invalid {
		if err := validateShortID(v); err == nil {
			t.Errorf("validateShortID(%q) expected error", v)
		}
	}
}

func TestValidateBool(t *testing.T) {
	for _, v := range []string{"true", "false"} {
		if err := validateBool(v); err != nil {
			t.Errorf("validateBool(%q) unexpected error: %v", v, err)
		}
	}

	for _, v := range []string{"", "yes", "1", "TRUE"} {
		if err := validateBool(v); err == nil {
			t.Errorf("validateBool(%q) expected error", v)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -run TestValidate -v`
Expected: FAIL — validator functions not defined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/filter/validators.go`:

```go
package filter

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// fieldValidators maps field names to their validation functions.
// The parser uses this table to validate field values.
var fieldValidators = map[string]func(string) error{
	"status":   validateStatus,
	"project":  validateProject,
	"priority": validatePriority,
	"due":      validateDue,
	"parent":   validateShortID,
	"tree":     validateShortID,
	"waiting":  validateBool,
}

func validateStatus(v string) error {
	if v == "" {
		return fmt.Errorf("status value cannot be empty")
	}
	parts := strings.Split(v, ",")
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("status contains empty value in %q", v)
		}
	}
	return nil
}

func validateProject(v string) error {
	if v == "" {
		return fmt.Errorf("project name cannot be empty")
	}
	return nil
}

func validatePriority(v string) error {
	if v == "" {
		return fmt.Errorf("priority value cannot be empty")
	}

	if strings.Contains(v, "..") {
		parts := strings.SplitN(v, "..", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("invalid priority range %q: use min..max", v)
		}
		min, errMin := parsePriorityValue(parts[0])
		max, errMax := parsePriorityValue(parts[1])
		if errMin != nil {
			return fmt.Errorf("invalid priority range min %q: %w", parts[0], errMin)
		}
		if errMax != nil {
			return fmt.Errorf("invalid priority range max %q: %w", parts[1], errMax)
		}
		if min > max {
			return fmt.Errorf("invalid priority range: min (%d) must be <= max (%d)", min, max)
		}
		return nil
	}

	_, err := parsePriorityValue(v)
	return err
}

// parsePriorityValue converts a single priority string to an int.
// Accepts numeric (0-4) or named (none, low, medium, high, urgent).
func parsePriorityValue(s string) (int, error) {
	named := map[string]int{
		"none": 0, "low": 1, "medium": 2, "high": 3, "urgent": 4,
	}
	if v, ok := named[strings.ToLower(s)]; ok {
		return v, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 || v > 4 {
		return 0, fmt.Errorf("invalid priority %q: expected 0-4 or none/low/medium/high/urgent", s)
	}
	return v, nil
}

func validateDue(v string) error {
	if v == "" {
		return fmt.Errorf("due value cannot be empty")
	}

	// Range: "today..friday" or "2026-04-01..2026-04-10"
	if strings.Contains(v, "..") {
		parts := strings.SplitN(v, "..", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("invalid due range %q: use start..end", v)
		}
		if err := validateSingleDue(parts[0]); err != nil {
			return fmt.Errorf("invalid due range start: %w", err)
		}
		if err := validateSingleDue(parts[1]); err != nil {
			return fmt.Errorf("invalid due range end: %w", err)
		}
		return nil
	}

	return validateSingleDue(v)
}

// validateSingleDue checks that a single due value is a recognized format.
func validateSingleDue(v string) error {
	lower := strings.ToLower(v)

	// Relative keywords
	switch lower {
	case "today", "tomorrow", "thisweek":
		return nil
	}

	// Weekday names
	weekdays := []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
	for _, w := range weekdays {
		if lower == w {
			return nil
		}
	}

	// RFC 3339
	if _, err := time.Parse(time.RFC3339, v); err == nil {
		return nil
	}

	// Date-only
	if _, err := time.Parse("2006-01-02", v); err == nil {
		return nil
	}

	return fmt.Errorf("invalid due date %q: use YYYY-MM-DD, RFC3339, today, tomorrow, thisweek, or a weekday name", v)
}

func validateShortID(v string) error {
	if len(v) < 4 {
		return fmt.Errorf("short ID %q is too short: minimum 4 hex characters", v)
	}
	for _, c := range v {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return fmt.Errorf("short ID %q contains non-hex character %q", v, string(c))
		}
	}
	return nil
}

func validateBool(v string) error {
	if v != "true" && v != "false" {
		return fmt.Errorf("expected \"true\" or \"false\", got %q", v)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -run TestValidate -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/germanamz/projects/tusk
git add internal/filter/validators.go internal/filter/validators_test.go
git commit -m "feat(filter): add field validator functions for all supported fields"
```

---

### Task 3: Table-Driven Parser

**Files:**
- Create: `internal/filter/parser.go`
- Create: `internal/filter/parser_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/filter/parser_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -run TestParse -v`
Expected: FAIL — `Parse` function not defined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/filter/parser.go`:

```go
package filter

import "strings"

// Parse takes a raw filter string and returns the AST plus any parse errors.
// It always returns a FilterSet (possibly empty) even when errors are present,
// so callers can use partial results if desired.
func Parse(input string) (*FilterSet, []ParseError) {
	tokens, lexErrs := Lex(input)

	fs := &FilterSet{}
	var errs []ParseError
	errs = append(errs, lexErrs...)

	for _, tok := range tokens {
		switch tok.Type {
		case TokenTagInclude:
			fs.Tags = append(fs.Tags, TagFilter{
				Name:    tok.Value[1:], // strip leading '+'
				Exclude: false,
				Pos:     tok.Pos,
			})

		case TokenTagExclude:
			fs.Tags = append(fs.Tags, TagFilter{
				Name:    tok.Value[1:], // strip leading '-'
				Exclude: true,
				Pos:     tok.Pos,
			})

		case TokenText:
			fs.Text = append(fs.Text, tok.Value)

		case TokenField:
			key, value, _ := strings.Cut(tok.Value, ":")
			validator, known := fieldValidators[key]
			if !known {
				errs = append(errs, ParseError{
					Pos:     tok.Pos,
					Field:   key,
					Message: "unknown field",
				})
				continue
			}
			if err := validator(value); err != nil {
				errs = append(errs, ParseError{
					Pos:     tok.Pos,
					Field:   key,
					Message: err.Error(),
				})
				continue
			}
			fs.Fields = append(fs.Fields, FieldFilter{
				Key:   key,
				Value: value,
				Pos:   tok.Pos,
			})
		}
	}

	return fs, errs
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -run TestParse -v`
Expected: PASS

- [ ] **Step 5: Run all filter package tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
cd /Users/germanamz/projects/tusk
git add internal/filter/parser.go internal/filter/parser_test.go
git commit -m "feat(filter): add table-driven parser with multi-error collection"
```

---

### Task 4: Parser Edge Cases

**Files:**
- Modify: `internal/filter/parser_test.go` (add edge case tests)

- [ ] **Step 1: Write the tests**

Append to `internal/filter/parser_test.go`:

```go
func TestParse_LexErrorsPropagated(t *testing.T) {
	// Bare "+" should produce a lex error that propagates through Parse
	_, errs := Parse("+ status:active")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error from bare +, got %d: %v", len(errs), errs)
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
```

- [ ] **Step 2: Run tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -run TestParse -v`
Expected: All PASS. If any fail, fix the parser or validators and re-run.

- [ ] **Step 3: Run all filter package tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -v`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
cd /Users/germanamz/projects/tusk
git add internal/filter/parser_test.go
git commit -m "test(filter): add parser edge case tests"
```
