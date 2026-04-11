# Inline Syntax Migration — Overview

> **This is a reference document.** The authoritative execution plans are the per-phase docs below. This file provides context and the file/task inventory.

**Goal:** Extract shared parsing infrastructure from the filter package into a reusable `syntax` package, migrate all CLI inline syntax from `key:value` to `key=value`, and add first-class modifier support (`,`, `:`, `..`, `()`, `+`, `-`).

**Architecture:** The filter package currently owns all tokenization, parsing, and resolution. This plan extracts a new `syntax/` package containing a generic lexer with modifier-aware tokenization. The `filter/` package becomes a thin consumer that defines domain-specific field validators on top of shared primitives. The separator change (`:`→`=`) happens in the shared lexer. A dual-separator bridge ensures backward compatibility during migration.

**Tech Stack:** Go standard library only. No new dependencies.

## Phase Documents (execute in order)

| Phase | Doc | Tasks | Summary |
|-------|-----|-------|---------|
| 1 | [Phase 1](2026-04-11-inline-syntax-migration-phase-1.md) | 3 | Create `syntax/` package (errors, AST, lexer with `=` separator) |
| 2 | [Phase 2](2026-04-11-inline-syntax-migration-phase-2.md) | 4 | Rewire `filter/` to use `syntax/` types + dual-separator bridge |
| 3 | [Phase 3](2026-04-11-inline-syntax-migration-phase-3.md) | 5 | Migrate all consumers from `:` to `=`, remove bridge |

Prerequisites: Phase 1 → Phase 2 → Phase 3 (linear, no parallelism).

---

## File Structure

### New files to create

| File | Responsibility |
|------|----------------|
| `syntax/token.go` | Token types, Token struct, Modifier enum, `Lex()` function with `=` separator and modifier-aware tokenization |
| `syntax/token_test.go` | Lexer unit tests — field detection, modifiers, quoted strings, groups, edge cases |
| `syntax/ast.go` | Shared AST types: `FieldFilter`, `TagFilter`, `FilterSet` (moved from `filter/ast.go`) |
| `syntax/errors.go` | `ParseError` type and `FormatErrors()` (moved from `filter/errors.go`) |

### Existing files to modify

| File | Change |
|------|--------|
| `filter/token.go` | Replace with thin import of `syntax.Lex()` or delete entirely (re-export from syntax) |
| `filter/ast.go` | Replace types with aliases or re-exports from `syntax/` |
| `filter/errors.go` | Replace types with aliases or re-exports from `syntax/` |
| `filter/parser.go` | Use `syntax.Token`, `syntax.FieldFilter`, split on `=` instead of `:` |
| `filter/parse_expr.go` | Use `syntax.Token`, `syntax.FieldFilter`, split on `=` instead of `:` |
| `filter/resolve.go` | Use `syntax.FieldFilter`, `syntax.FilterSet` |
| `filter/validators.go` | No structural change — just receives values without separator |
| `filter/token_test.go` | Update all test inputs from `key:value` to `key=value`, update expected token values |
| `filter/parser_test.go` | Update test inputs from `key:value` to `key=value` |
| `filter/parse_expr_test.go` | Update test inputs from `key:value` to `key=value` |
| `filter/resolve_test.go` | Update test inputs from `key:value` to `key=value` |
| `filter/resolve_expr_test.go` | Update test inputs from `key:value` to `key=value` |
| `filter/resolve_uda_test.go` | Update test inputs from `key:value` to `key=value` |
| `filter/integration_test.go` | Update test inputs from `key:value` to `key=value` |
| `internal/tui/commands.go` | Update `Use:` help strings from `key:value` to `key=value` |
| `internal/tui/commands_test.go` | Update test inputs |
| `internal/mcp/server.go` | Update example strings in tool descriptions (lines 230, 521, 535) |
| `tests/e2e/filtering_test.go` | Update 21 `key:value` occurrences to `key=value` |
| `tests/e2e/hierarchy_test.go` | Update 15 occurrences |
| `tests/e2e/propagation_test.go` | Update 13 occurrences |
| `tests/e2e/task_lifecycle_test.go` | Update 7 occurrences |
| `tests/e2e/urgency_test.go` | Update 7 occurrences |
| `tests/e2e/output_format_test.go` | Update 7 occurrences |
| `tests/e2e/task_queue_test.go` | Update 5 occurrences |
| `tests/e2e/player_test.go` | Update 2 occurrences |
| `tests/e2e/error_handling_test.go` | Update 2 occurrences |

---

## Phase 1: Extract Shared Lexer and AST

### Task 1: Create `syntax/errors.go` — shared error types

**Files:**
- Create: `syntax/errors.go`
- Create: `syntax/errors_test.go`
- Reference: `filter/errors.go` (source of types to extract)

- [ ] **Step 1: Write the failing test**

Create `syntax/errors_test.go`:

```go
package syntax

import "testing"

func TestParseError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  ParseError
		want string
	}{
		{
			name: "with position and field",
			err:  ParseError{Pos: 5, Field: "status", Message: "unknown field"},
			want: `filter error at position 5: field "status": unknown field`,
		},
		{
			name: "with position no field",
			err:  ParseError{Pos: 0, Message: "bare \"+\" is not a valid token"},
			want: `filter error at position 0: bare "+" is not a valid token`,
		},
		{
			name: "negative position",
			err:  ParseError{Pos: -1, Message: "something went wrong"},
			want: "filter error: something went wrong",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.err.Error()
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatErrors(t *testing.T) {
	errs := []ParseError{
		{Pos: 0, Message: "first error"},
		{Pos: 10, Field: "due", Message: "invalid date"},
	}
	got := FormatErrors(errs)
	want := "filter error at position 0: first error\nfilter error at position 10: field \"due\": invalid date"
	if got != want {
		t.Errorf("FormatErrors:\ngot:  %q\nwant: %q", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./syntax -run TestParseError`
Expected: FAIL — package `syntax` does not exist yet

- [ ] **Step 3: Write the implementation**

Create `syntax/errors.go` — this is a direct copy from `filter/errors.go` with package name changed:

```go
package syntax

import (
	"fmt"
	"strings"
)

// ParseError represents a single issue found during parsing or validation.
type ParseError struct {
	Pos     int    // byte offset in input (-1 if not applicable)
	Field   string // field name, if relevant
	Message string // human-readable description
}

func (e ParseError) Error() string {
	var b strings.Builder
	if e.Pos >= 0 {
		fmt.Fprintf(&b, "filter error at position %d: ", e.Pos)
	} else {
		b.WriteString("filter error: ")
	}
	if e.Field != "" {
		fmt.Fprintf(&b, "field %q: ", e.Field)
	}
	b.WriteString(e.Message)
	return b.String()
}

// FormatErrors joins multiple ParseErrors into a newline-separated string.
func FormatErrors(errs []ParseError) string {
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "\n")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./syntax -run TestParseError`
Expected: PASS

Run: `go test -v ./syntax -run TestFormatErrors`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add syntax/errors.go syntax/errors_test.go
git commit -m "$(cat <<'EOF'
refactor(syntax): extract ParseError and FormatErrors from filter package

Foundation for the shared syntax package. These types will be used by
both the filter package and future inline syntax consumers.
EOF
)"
```

---

### Task 2: Create `syntax/ast.go` — shared AST types

**Files:**
- Create: `syntax/ast.go`
- Reference: `filter/ast.go` (source of types to extract)

- [ ] **Step 1: Write the failing test**

No separate test file needed — these are data types that will be tested through the lexer and parser. But we verify the package compiles and the `Title()` helper works.

Create `syntax/ast_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./syntax -run TestFilterSet`
Expected: FAIL — `FilterSet` not defined yet

- [ ] **Step 3: Write the implementation**

Create `syntax/ast.go`:

```go
package syntax

import "strings"

// FilterSet is a collection of parsed inline syntax terms, implicitly AND'd.
// Used by both filter expressions and task creation/modification commands.
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

// GetField returns the first FieldFilter with the given key.
// The bool is false if no field with that key exists.
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

// FieldFilter represents a key=value term.
type FieldFilter struct {
	Key   string // field name (e.g. "status", "project", "uda.env")
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

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -v ./syntax -run TestFilterSet`
Expected: PASS (all four subtests)

- [ ] **Step 5: Commit**

```bash
git add syntax/ast.go syntax/ast_test.go
git commit -m "$(cat <<'EOF'
refactor(syntax): extract shared AST types from filter package

FilterSet, FieldFilter, and TagFilter are the building blocks for both
filter expressions and inline task creation/modification syntax.
EOF
)"
```

---

### Task 3: Create `syntax/token.go` — shared lexer with `=` separator and modifier support

This is the core of the shared lexer. It tokenizes input using `=` as the field separator and understands modifiers as first-class primitives.

**Files:**
- Create: `syntax/token.go`
- Create: `syntax/token_test.go`
- Reference: `filter/token.go` (current lexer using `:` separator)

- [ ] **Step 1: Write the failing test for basic tokenization**

Create `syntax/token_test.go`:

```go
package syntax

import "testing"

func TestTokenType_String(t *testing.T) {
	tests := []struct {
		tt   TokenType
		want string
	}{
		{TokenField, "Field"},
		{TokenTagInclude, "TagInclude"},
		{TokenTagExclude, "TagExclude"},
		{TokenText, "Text"},
		{TokenAnd, "And"},
		{TokenOr, "Or"},
		{TokenNot, "Not"},
		{TokenLParen, "LParen"},
		{TokenRParen, "RParen"},
	}
	for _, tc := range tests {
		got := tc.tt.String()
		if got != tc.want {
			t.Fatalf("TokenType(%d).String() = %q, want %q", tc.tt, got, tc.want)
		}
	}
}

func TestLex(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   []Token
		errors int
	}{
		{
			name:  "empty input",
			input: "",
			want:  nil,
		},
		{
			name:  "text only",
			input: "Implement auth middleware",
			want: []Token{
				{Type: TokenText, Value: "Implement", Pos: 0},
				{Type: TokenText, Value: "auth", Pos: 10},
				{Type: TokenText, Value: "middleware", Pos: 15},
			},
		},
		{
			name:  "field key=value",
			input: "status=active",
			want: []Token{
				{Type: TokenField, Value: "status=active", Pos: 0},
			},
		},
		{
			name:  "tag include",
			input: "+api +frontend",
			want: []Token{
				{Type: TokenTagInclude, Value: "+api", Pos: 0},
				{Type: TokenTagInclude, Value: "+frontend", Pos: 5},
			},
		},
		{
			name:  "tag exclude",
			input: "-docs -wip",
			want: []Token{
				{Type: TokenTagExclude, Value: "-docs", Pos: 0},
				{Type: TokenTagExclude, Value: "-wip", Pos: 6},
			},
		},
		{
			name:  "mixed input",
			input: "My task project=backend +api -docs priority=3",
			want: []Token{
				{Type: TokenText, Value: "My", Pos: 0},
				{Type: TokenText, Value: "task", Pos: 3},
				{Type: TokenField, Value: "project=backend", Pos: 8},
				{Type: TokenTagInclude, Value: "+api", Pos: 24},
				{Type: TokenTagExclude, Value: "-docs", Pos: 29},
				{Type: TokenField, Value: "priority=3", Pos: 35},
			},
		},
		{
			name:  "bare plus sign",
			input: "title + more",
			want: []Token{
				{Type: TokenText, Value: "title", Pos: 0},
				{Type: TokenText, Value: "more", Pos: 8},
			},
			errors: 1,
		},
		{
			name:  "bare minus sign",
			input: "title - more",
			want: []Token{
				{Type: TokenText, Value: "title", Pos: 0},
				{Type: TokenText, Value: "more", Pos: 8},
			},
			errors: 1,
		},
		{
			name:  "multiple spaces between tokens",
			input: "status=active   +api",
			want: []Token{
				{Type: TokenField, Value: "status=active", Pos: 0},
				{Type: TokenTagInclude, Value: "+api", Pos: 16},
			},
		},
		{
			name:  "quoted text standalone",
			input: `"fix the bug"`,
			want: []Token{
				{Type: TokenText, Value: "fix the bug", Pos: 0},
			},
		},
		{
			name:  "quoted field value",
			input: `title="fix the bug"`,
			want: []Token{
				{Type: TokenField, Value: `title=fix the bug`, Pos: 0},
			},
		},
		{
			name:  "mixed quoted and unquoted",
			input: `status=active title="fix the bug" +api`,
			want: []Token{
				{Type: TokenField, Value: "status=active", Pos: 0},
				{Type: TokenField, Value: `title=fix the bug`, Pos: 14},
				{Type: TokenTagInclude, Value: "+api", Pos: 34},
			},
		},
		{
			name:  "escaped quote inside quoted string",
			input: `title="say \"hello\""`,
			want: []Token{
				{Type: TokenField, Value: `title=say "hello"`, Pos: 0},
			},
		},
		{
			name:  "AND keyword",
			input: "status=active AND +api",
			want: []Token{
				{Type: TokenField, Value: "status=active", Pos: 0},
				{Type: TokenAnd, Value: "AND", Pos: 14},
				{Type: TokenTagInclude, Value: "+api", Pos: 18},
			},
		},
		{
			name:  "OR keyword",
			input: "status=active OR status=pending",
			want: []Token{
				{Type: TokenField, Value: "status=active", Pos: 0},
				{Type: TokenOr, Value: "OR", Pos: 14},
				{Type: TokenField, Value: "status=pending", Pos: 17},
			},
		},
		{
			name:  "NOT keyword",
			input: "NOT status=deleted",
			want: []Token{
				{Type: TokenNot, Value: "NOT", Pos: 0},
				{Type: TokenField, Value: "status=deleted", Pos: 4},
			},
		},
		{
			name:  "boolean grouping parentheses with space",
			input: "(status=active OR +urgent)",
			want: []Token{
				{Type: TokenLParen, Value: "(", Pos: 0},
				{Type: TokenField, Value: "status=active", Pos: 1},
				{Type: TokenOr, Value: "OR", Pos: 15},
				{Type: TokenTagInclude, Value: "+urgent", Pos: 18},
				{Type: TokenRParen, Value: ")", Pos: 25},
			},
		},
		{
			name:  "lowercase and/or/not is text not keyword",
			input: "and or not",
			want: []Token{
				{Type: TokenText, Value: "and", Pos: 0},
				{Type: TokenText, Value: "or", Pos: 4},
				{Type: TokenText, Value: "not", Pos: 7},
			},
		},
		{
			name:  "comma modifier in value — set",
			input: "status=pending,active",
			want: []Token{
				{Type: TokenField, Value: "status=pending,active", Pos: 0},
			},
		},
		{
			name:  "range modifier in value",
			input: "priority=2..4",
			want: []Token{
				{Type: TokenField, Value: "priority=2..4", Pos: 0},
			},
		},
		{
			name:  "colon modifier in value — sequence",
			input: "transition=pending:active",
			want: []Token{
				{Type: TokenField, Value: "transition=pending:active", Pos: 0},
			},
		},
		{
			name:  "group modifier — parens immediately after value",
			input: "status=pending(initial)",
			want: []Token{
				{Type: TokenField, Value: "status=pending(initial)", Pos: 0},
			},
		},
		{
			name:  "group with set inside",
			input: "status=active(start,highlight)",
			want: []Token{
				{Type: TokenField, Value: "status=active(start,highlight)", Pos: 0},
			},
		},
		{
			name:  "quoted value is opaque — no modifier tokenization",
			input: `title="pending(initial)"`,
			want: []Token{
				{Type: TokenField, Value: `title=pending(initial)`, Pos: 0},
			},
		},
		{
			name:  "due date with colon in value",
			input: "due=2026-04-10T15:30:00Z",
			want: []Token{
				{Type: TokenField, Value: "due=2026-04-10T15:30:00Z", Pos: 0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, errs := Lex(tt.input)
			if len(errs) != tt.errors {
				t.Fatalf("Lex(%q) returned %d errors, want %d: %v", tt.input, len(errs), tt.errors, errs)
			}
			if len(tokens) != len(tt.want) {
				t.Fatalf("Lex(%q) returned %d tokens, want %d:\ngot:  %+v\nwant: %+v",
					tt.input, len(tokens), len(tt.want), tokens, tt.want)
			}
			for i, tok := range tokens {
				if tok.Type != tt.want[i].Type {
					t.Errorf("token[%d].Type = %v, want %v", i, tok.Type, tt.want[i].Type)
				}
				if tok.Value != tt.want[i].Value {
					t.Errorf("token[%d].Value = %q, want %q", i, tok.Value, tt.want[i].Value)
				}
				if tok.Pos != tt.want[i].Pos {
					t.Errorf("token[%d].Pos = %d, want %d", i, tok.Pos, tt.want[i].Pos)
				}
			}
		})
	}
}

func TestLex_EdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   []Token
		errors int
	}{
		{
			name:  "field with empty value",
			input: "status=",
			want: []Token{
				{Type: TokenField, Value: "status=", Pos: 0},
			},
		},
		{
			name:  "equals at start is text not field",
			input: "=value",
			want: []Token{
				{Type: TokenText, Value: "=value", Pos: 0},
			},
		},
		{
			name:  "tag with numbers",
			input: "+v2 -v1",
			want: []Token{
				{Type: TokenTagInclude, Value: "+v2", Pos: 0},
				{Type: TokenTagExclude, Value: "-v1", Pos: 4},
			},
		},
		{
			name:  "multiple errors collected",
			input: "+ text -",
			want: []Token{
				{Type: TokenText, Value: "text", Pos: 2},
			},
			errors: 2,
		},
		{
			name:  "only whitespace",
			input: "   \t  ",
			want:  nil,
		},
		{
			name:   "unclosed quote",
			input:  `title="fix the bug`,
			want:   nil,
			errors: 1,
		},
		{
			name:  "empty quoted string is text",
			input: `""`,
			want: []Token{
				{Type: TokenText, Value: "", Pos: 0},
			},
		},
		{
			name:  "quoted string with only spaces",
			input: `"  "`,
			want: []Token{
				{Type: TokenText, Value: "  ", Pos: 0},
			},
		},
		{
			name:  "field with quoted value containing equals",
			input: `title="step 1 = do things"`,
			want: []Token{
				{Type: TokenField, Value: `title=step 1 = do things`, Pos: 0},
			},
		},
		{
			name:  "additive modifier with field",
			input: "+status=review",
			want: []Token{
				{Type: TokenField, Value: "+status=review", Pos: 0},
			},
		},
		{
			name:  "subtractive modifier with field",
			input: "-status=review",
			want: []Token{
				{Type: TokenField, Value: "-status=review", Pos: 0},
			},
		},
		{
			name:  "group followed by space is boolean grouping",
			input: "(status=active)",
			want: []Token{
				{Type: TokenLParen, Value: "(", Pos: 0},
				{Type: TokenField, Value: "status=active", Pos: 1},
				{Type: TokenRParen, Value: ")", Pos: 14},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, errs := Lex(tt.input)
			if len(errs) != tt.errors {
				t.Fatalf("Lex(%q) returned %d errors, want %d: %v", tt.input, len(errs), tt.errors, errs)
			}
			if len(tokens) != len(tt.want) {
				t.Fatalf("Lex(%q) returned %d tokens, want %d:\ngot:  %+v\nwant: %+v",
					tt.input, len(tokens), len(tt.want), tokens, tt.want)
			}
			for i, tok := range tokens {
				if tok.Type != tt.want[i].Type {
					t.Errorf("token[%d].Type = %v, want %v", i, tok.Type, tt.want[i].Type)
				}
				if tok.Value != tt.want[i].Value {
					t.Errorf("token[%d].Value = %q, want %q", i, tok.Value, tt.want[i].Value)
				}
				if tok.Pos != tt.want[i].Pos {
					t.Errorf("token[%d].Pos = %d, want %d", i, tok.Pos, tt.want[i].Pos)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./syntax -run TestLex`
Expected: FAIL — `Lex` not defined

- [ ] **Step 3: Write the lexer implementation**

Create `syntax/token.go`:

```go
package syntax

import "fmt"

// TokenType classifies a lexed token.
type TokenType int

const (
	TokenField      TokenType = iota // key=value
	TokenTagInclude                  // +word
	TokenTagExclude                  // -word
	TokenText                        // anything else
	TokenAnd                         // AND
	TokenOr                          // OR
	TokenNot                         // NOT
	TokenLParen                      // (
	TokenRParen                      // )
)

func (t TokenType) String() string {
	switch t {
	case TokenField:
		return "Field"
	case TokenTagInclude:
		return "TagInclude"
	case TokenTagExclude:
		return "TagExclude"
	case TokenText:
		return "Text"
	case TokenAnd:
		return "And"
	case TokenOr:
		return "Or"
	case TokenNot:
		return "Not"
	case TokenLParen:
		return "LParen"
	case TokenRParen:
		return "RParen"
	default:
		return "Unknown"
	}
}

// Token is a single lexed element from an input string.
type Token struct {
	Type  TokenType
	Value string // raw text of the token
	Pos   int    // byte offset in the original input
}

// Lex splits the input string into tokens using = as the field separator.
// Modifiers (,  :  ..  ()) inside field values are preserved as part of the
// raw value — the lexer does not decompose them into sub-tokens.
// Quoted strings are opaque: no modifier tokenization inside quotes.
// Parentheses are position-disambiguated: ( immediately after a value (no
// whitespace) is part of the value (group modifier); ( preceded by whitespace
// is a boolean grouping operator.
//
// Returns all tokens produced plus any errors encountered. Processing
// continues past errors so all issues are reported in one pass.
func Lex(input string) ([]Token, []ParseError) {
	var tokens []Token
	var errs []ParseError

	i := 0
	for i < len(input) {
		// Skip whitespace
		if input[i] == ' ' || input[i] == '\t' {
			i++
			continue
		}

		start := i
		var raw string

		if input[i] == '"' {
			// Standalone quoted string: "some text"
			content, end, err := scanQuoted(input, i)
			if err != nil {
				errs = append(errs, ParseError{Pos: start, Message: err.Error()})
				break // unclosed quote — can't continue scanning
			}
			i = end
			raw = content
			tokens = append(tokens, Token{Type: TokenText, Value: raw, Pos: start})
			continue
		}

		// Parentheses preceded by whitespace (or at start) are boolean grouping
		if input[i] == '(' {
			tokens = append(tokens, Token{Type: TokenLParen, Value: "(", Pos: i})
			i++
			continue
		}
		if input[i] == ')' {
			tokens = append(tokens, Token{Type: TokenRParen, Value: ")", Pos: i})
			i++
			continue
		}

		// Scan a token: unquoted characters until whitespace
		// Quotes mid-token (key="value") are inlined.
		// Parentheses immediately after non-whitespace are part of the token
		// (group modifier), but ) always closes.
		var buf []byte
		unclosedQuote := false
		parenDepth := 0
		for i < len(input) && input[i] != ' ' && input[i] != '\t' {
			// ) at paren depth 0 is a boolean close — stop
			if input[i] == ')' && parenDepth == 0 {
				break
			}
			// ( immediately after prior characters (no whitespace) is a group modifier
			if input[i] == '(' {
				parenDepth++
				buf = append(buf, input[i])
				i++
				continue
			}
			if input[i] == ')' && parenDepth > 0 {
				parenDepth--
				buf = append(buf, input[i])
				i++
				continue
			}
			if input[i] == '"' {
				// Quote inside a token: key="value with spaces"
				content, end, err := scanQuoted(input, i)
				if err != nil {
					errs = append(errs, ParseError{Pos: i, Message: err.Error()})
					raw = string(buf)
					unclosedQuote = true
					break
				}
				buf = append(buf, content...)
				i = end
				continue
			}
			buf = append(buf, input[i])
			i++
		}
		if unclosedQuote {
			break
		}

		if raw == "" {
			raw = string(buf)
		}

		if raw == "" {
			continue
		}

		// Classify the token
		switch {
		case len(raw) == 1 && (raw[0] == '+' || raw[0] == '-'):
			errs = append(errs, ParseError{
				Pos:     start,
				Message: fmt.Sprintf("bare %q is not a valid token; use %s<name> for tags", raw, raw),
			})

		case isFieldToken(raw):
			tokens = append(tokens, Token{Type: TokenField, Value: raw, Pos: start})

		case raw[0] == '+':
			// +key=value is a field with additive modifier
			if hasEquals(raw[1:]) {
				tokens = append(tokens, Token{Type: TokenField, Value: raw, Pos: start})
			} else {
				tokens = append(tokens, Token{Type: TokenTagInclude, Value: raw, Pos: start})
			}

		case raw[0] == '-':
			// -key=value is a field with subtractive modifier
			if hasEquals(raw[1:]) {
				tokens = append(tokens, Token{Type: TokenField, Value: raw, Pos: start})
			} else {
				tokens = append(tokens, Token{Type: TokenTagExclude, Value: raw, Pos: start})
			}

		case raw == "AND":
			tokens = append(tokens, Token{Type: TokenAnd, Value: raw, Pos: start})

		case raw == "OR":
			tokens = append(tokens, Token{Type: TokenOr, Value: raw, Pos: start})

		case raw == "NOT":
			tokens = append(tokens, Token{Type: TokenNot, Value: raw, Pos: start})

		default:
			tokens = append(tokens, Token{Type: TokenText, Value: raw, Pos: start})
		}
	}

	return tokens, errs
}

// scanQuoted reads a quoted string starting at input[pos] (which must be '"').
// Returns the unescaped content (without surrounding quotes), the byte index
// immediately after the closing quote, and any error (unclosed quote).
// Supports \" as an escaped literal quote inside the string.
func scanQuoted(input string, pos int) (string, int, error) {
	i := pos + 1
	var buf []byte
	for i < len(input) {
		if input[i] == '\\' && i+1 < len(input) && input[i+1] == '"' {
			buf = append(buf, '"')
			i += 2
			continue
		}
		if input[i] == '"' {
			return string(buf), i + 1, nil
		}
		buf = append(buf, input[i])
		i++
	}
	return "", pos, fmt.Errorf("unclosed quoted string")
}

// isFieldToken returns true if the raw token contains = with a non-empty key.
// Tokens starting with + or - are handled separately by the caller.
func isFieldToken(raw string) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] == '=' {
			return i > 0
		}
	}
	return false
}

// hasEquals returns true if s contains at least one '=' character.
func hasEquals(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -v ./syntax -run TestLex`
Expected: PASS (all subtests in both TestLex and TestLex_EdgeCases)

Run: `go test -v ./syntax -run TestTokenType`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add syntax/token.go syntax/token_test.go
git commit -m "$(cat <<'EOF'
feat(syntax): shared lexer with = separator and modifier support

The new lexer uses = as the field separator (freeing : for use in
values like workflow transitions). Modifiers (,  :  ..  ()) are
preserved in the raw token value. Quoted strings are opaque — no
modifier tokenization inside quotes. Group parens are
position-disambiguated from boolean grouping parens.
EOF
)"
```

---

### Task 4: Rewire `filter/` to delegate to `syntax/` package

Replace filter's own token types, AST types, and error types with re-exports from `syntax/`. Update `Lex` calls and field splitting from `:` to `=`.

**Files:**
- Modify: `filter/token.go`
- Modify: `filter/ast.go`
- Modify: `filter/errors.go`
- Modify: `filter/parser.go`
- Modify: `filter/parse_expr.go`
- Modify: `filter/resolve.go`
- Modify: `filter/validators.go` (no change needed — just receives values)

- [ ] **Step 1: Replace `filter/errors.go` with re-exports**

Replace the contents of `filter/errors.go` with:

```go
package filter

import "github.com/germanamz/tusk/syntax"

// ParseError is a re-export of the shared syntax.ParseError type.
type ParseError = syntax.ParseError

// FormatErrors is a re-export of the shared syntax.FormatErrors function.
var FormatErrors = syntax.FormatErrors
```

- [ ] **Step 2: Replace `filter/ast.go` with re-exports**

Replace the contents of `filter/ast.go` with:

```go
package filter

import "github.com/germanamz/tusk/syntax"

// Re-export shared AST types so existing consumers compile unchanged.
type FilterSet = syntax.FilterSet
type FieldFilter = syntax.FieldFilter
type TagFilter = syntax.TagFilter
```

- [ ] **Step 3: Replace `filter/token.go` with re-exports and delegating Lex**

Replace the contents of `filter/token.go` with:

```go
package filter

import "github.com/germanamz/tusk/syntax"

// Re-export shared token types so existing consumers compile unchanged.
type TokenType = syntax.TokenType
type Token = syntax.Token

const (
	TokenField      = syntax.TokenField
	TokenTagInclude = syntax.TokenTagInclude
	TokenTagExclude = syntax.TokenTagExclude
	TokenText       = syntax.TokenText
	TokenAnd        = syntax.TokenAnd
	TokenOr         = syntax.TokenOr
	TokenNot        = syntax.TokenNot
	TokenLParen     = syntax.TokenLParen
	TokenRParen     = syntax.TokenRParen
)

// Lex delegates to the shared syntax lexer.
func Lex(input string) ([]Token, []ParseError) {
	return syntax.Lex(input)
}
```

- [ ] **Step 4: Update `filter/parser.go` — split on `=` instead of `:`**

In `filter/parser.go`, change line 59 from:

```go
key, value, _ := strings.Cut(tok.Value, ":")
```

to:

```go
key, value, _ := strings.Cut(tok.Value, "=")
```

- [ ] **Step 5: Update `filter/parse_expr.go` — split on `=` instead of `:`**

In `filter/parse_expr.go`, change line 241 from:

```go
key, value, _ := strings.Cut(tok.Value, ":")
```

to:

```go
key, value, _ := strings.Cut(tok.Value, "=")
```

- [ ] **Step 6: Run `go vet ./filter/...` to verify no compile errors**

Run: `go vet ./filter/...`
Expected: clean output, no errors

- [ ] **Step 7: Commit**

```bash
git add filter/token.go filter/ast.go filter/errors.go filter/parser.go filter/parse_expr.go
git commit -m "$(cat <<'EOF'
refactor(filter): delegate to syntax package, switch separator to =

filter/ now re-exports token, AST, and error types from syntax/.
Field splitting changed from : to = throughout the parser and
expression parser. This is the core separator migration.
EOF
)"
```

---

### Task 5: Update filter package tests for `=` separator

All test files in `filter/` use `key:value` syntax in test inputs. Every occurrence needs to change to `key=value`.

**Files:**
- Modify: `filter/token_test.go`
- Modify: `filter/parser_test.go`
- Modify: `filter/parse_expr_test.go`
- Modify: `filter/resolve_test.go`
- Modify: `filter/resolve_expr_test.go`
- Modify: `filter/resolve_uda_test.go`
- Modify: `filter/integration_test.go`
- Modify: `filter/validators_test.go` (if it has colon-based inputs)

- [ ] **Step 1: Update `filter/token_test.go`**

This file has ~25 occurrences of colon syntax in test inputs and expected values. Perform a search-and-replace across the file:

1. Replace all `key:value` patterns in test `input` strings: `status:active` → `status=active`, `project:backend` → `project=backend`, `priority:3` → `priority=3`, `priority:2..4` → `priority=2..4`, `due:today..friday` → `due=today..friday`, `due:2026-04-10T15:30:00Z` → `due=2026-04-10T15:30:00Z`, `title:"fix the bug"` → `title="fix the bug"`, `title:"say \"hello\""` → `title="say \"hello\""`, `title:"step 1: do things"` → `title="step 1: do things"`
2. Replace all matching expected `Value` strings: `"status:active"` → `"status=active"`, etc.
3. Update test names: `"field key:value"` → `"field key=value"`
4. The `"field with colon in value"` test case becomes important: `due=2026-04-10T15:30:00Z` should still produce a single field token with the colon preserved in the value portion.
5. The `"colon at start is text not field"` test: change to `"equals at start is text not field"` with input `=value`.
6. The `"tag-like token with colon is a field"` test: `+api:test` — this is no longer a field token since `:` is not the separator. Change to `+api=test` which should be a field token with value `+api=test`.
7. Remove the `"field with colon in value"` test case — colons in values are no longer ambiguous since `=` is the separator.

Apply these changes carefully. The exact edits depend on the current content (already read above in Task 3's reference). Apply each replacement.

- [ ] **Step 2: Update `filter/parser_test.go`**

Replace all `key:value` patterns in test inputs with `key=value`. ~15 occurrences. Same mechanical replacement as token_test.go but for the Parser test cases.

- [ ] **Step 3: Update `filter/parse_expr_test.go`**

Replace all `key:value` patterns. ~6 occurrences.

- [ ] **Step 4: Update `filter/resolve_test.go`**

Replace all `key:value` patterns in test input strings.

- [ ] **Step 5: Update `filter/resolve_expr_test.go`**

Replace all `key:value` patterns.

- [ ] **Step 6: Update `filter/resolve_uda_test.go`**

Replace all `uda.key:value` patterns with `uda.key=value`.

- [ ] **Step 7: Update `filter/integration_test.go`**

Replace all `key:value` patterns. ~4 occurrences.

- [ ] **Step 8: Run the full filter test suite**

Run: `go test -v ./filter/...`
Expected: ALL PASS

- [ ] **Step 9: Commit**

```bash
git add filter/*_test.go
git commit -m "$(cat <<'EOF'
test(filter): update all test inputs from key:value to key=value

Mechanical replacement across all filter test files to match the new
= separator from the syntax package migration.
EOF
)"
```

---

## Phase 2: Migrate CLI and MCP Surfaces

### Task 6: Update CLI help text and command descriptions

**Files:**
- Modify: `internal/tui/commands.go:20,29` — `Use:` strings
- Modify: `internal/tui/commands_test.go` — any test referencing `key:value`

- [ ] **Step 1: Update `add` command Use string**

In `internal/tui/commands.go`, line 20, change:

```go
Use:   "add [title] [key:value...] [+tag...]",
```

to:

```go
Use:   "add [title] [key=value...] [+tag...]",
```

- [ ] **Step 2: Update `modify` command Use string**

In `internal/tui/commands.go`, line 29, change:

```go
Use:   "modify <short_id> [key:value...]",
```

to:

```go
Use:   "modify <short_id> [key=value...]",
```

- [ ] **Step 3: Update `internal/tui/commands_test.go`**

Find the comment at line 353 referencing `key:value` and update:

```go
// Only key:value args, no title words
```

to:

```go
// Only key=value args, no title words
```

Also update any test input strings that use `key:value` syntax.

- [ ] **Step 4: Run TUI tests**

Run: `go test -v ./internal/tui/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/commands.go internal/tui/commands_test.go
git commit -m "$(cat <<'EOF'
docs(tui): update CLI help text from key:value to key=value syntax
EOF
)"
```

---

### Task 7: Update MCP tool description examples

**Files:**
- Modify: `internal/mcp/server.go:230,521,535`

- [ ] **Step 1: Update tusk_task_list filter description**

In `internal/mcp/server.go`, line 230, change:

```go
mcp.Description("Filter expression with AND/OR/NOT/parentheses support (e.g. 'status:active OR +urgent'). When provided, other filter parameters are ignored."),
```

to:

```go
mcp.Description("Filter expression with AND/OR/NOT/parentheses support (e.g. 'status=active OR +urgent'). When provided, other filter parameters are ignored."),
```

- [ ] **Step 2: Update tusk_task_available filter description**

In `internal/mcp/server.go`, line 521, change:

```go
mcp.Description("Boolean filter expression (e.g. 'project:backend AND +api')"),
```

to:

```go
mcp.Description("Boolean filter expression (e.g. 'project=backend AND +api')"),
```

- [ ] **Step 3: Update tusk_task_pop filter description**

In `internal/mcp/server.go`, line 535, change:

```go
mcp.Description("Optional boolean filter to narrow candidates (e.g. 'project:backend')"),
```

to:

```go
mcp.Description("Optional boolean filter to narrow candidates (e.g. 'project=backend')"),
```

- [ ] **Step 4: Run MCP tests (if any)**

Run: `go test -v ./internal/mcp/...`
Expected: PASS (or no tests — these are string-only changes)

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/server.go
git commit -m "$(cat <<'EOF'
docs(mcp): update filter examples from key:value to key=value syntax
EOF
)"
```

---

### Task 8: Update E2E tests — filtering, lifecycle, and hierarchy

This is the largest mechanical change. Each E2E test file has `key:value` patterns in Step `Args` arrays. Replace all occurrences with `key=value`.

**Files:**
- Modify: `tests/e2e/filtering_test.go` (21 occurrences)
- Modify: `tests/e2e/hierarchy_test.go` (15 occurrences)
- Modify: `tests/e2e/propagation_test.go` (13 occurrences)
- Modify: `tests/e2e/task_lifecycle_test.go` (7 occurrences)

- [ ] **Step 1: Update `tests/e2e/filtering_test.go`**

Replace all `key:value` filter syntax in Args arrays. Examples of patterns to replace:

- `"status:active"` → `"status=active"`
- `"status:pending,active"` → `"status=pending,active"`
- `"priority:3"` → `"priority=3"`
- `"priority:2..4"` → `"priority=2..4"`
- `"project:_default"` → `"project=_default"`
- `"parent:$0.short_id"` → `"parent=$0.short_id"`
- `"tree:$0.short_id"` → `"tree=$0.short_id"`
- `"claimed_by:agent-filter"` → `"claimed_by=agent-filter"`
- `"unclaimed:true"` → `"unclaimed=true"`
- `"title:\"auth\""` or `title:"auth"` → `title="auth"` (adjust quoting)
- `"description:\"authentication\""` → `description="authentication"` (adjust quoting)
- `"uda.env:prod"` → `"uda.env=prod"`

Search for the pattern `[a-z_.]+:` inside string literals in Args arrays and replace the colon after recognized field names with `=`.

- [ ] **Step 2: Update `tests/e2e/hierarchy_test.go`**

Replace all `parent:` and `tree:` patterns with `=`. Also `priority:` and `status:` if present.

- [ ] **Step 3: Update `tests/e2e/propagation_test.go`**

Replace all `parent:`, `status:`, `priority:` patterns.

- [ ] **Step 4: Update `tests/e2e/task_lifecycle_test.go`**

Replace `status:`, `priority:`, `project:` patterns.

- [ ] **Step 5: Run these E2E tests to verify**

Run: `go test -v ./tests/e2e -run TestFiltering`
Run: `go test -v ./tests/e2e -run TestHierarchy`
Run: `go test -v ./tests/e2e -run TestPropagation`
Run: `go test -v ./tests/e2e -run TestTaskLifecycle`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add tests/e2e/filtering_test.go tests/e2e/hierarchy_test.go tests/e2e/propagation_test.go tests/e2e/task_lifecycle_test.go
git commit -m "$(cat <<'EOF'
test(e2e): migrate filter syntax from key:value to key=value

Updates filtering, hierarchy, propagation, and task lifecycle E2E
tests to use the new = separator.
EOF
)"
```

---

### Task 9: Update E2E tests — remaining files

**Files:**
- Modify: `tests/e2e/urgency_test.go` (7 occurrences)
- Modify: `tests/e2e/output_format_test.go` (7 occurrences)
- Modify: `tests/e2e/task_queue_test.go` (5 occurrences)
- Modify: `tests/e2e/player_test.go` (2 occurrences)
- Modify: `tests/e2e/error_handling_test.go` (2 occurrences)

- [ ] **Step 1: Update `tests/e2e/urgency_test.go`**

Replace `priority:` patterns with `priority=`.

- [ ] **Step 2: Update `tests/e2e/output_format_test.go`**

Replace `priority:` patterns.

- [ ] **Step 3: Update `tests/e2e/task_queue_test.go`**

Replace `priority:` patterns.

- [ ] **Step 4: Update `tests/e2e/player_test.go`**

Replace `claimed_by:` and `unclaimed:` patterns.

- [ ] **Step 5: Update `tests/e2e/error_handling_test.go`**

Replace `priority:` and `project:` patterns.

- [ ] **Step 6: Run the full E2E suite**

Run: `make test-e2e`
Expected: ALL PASS

- [ ] **Step 7: Commit**

```bash
git add tests/e2e/urgency_test.go tests/e2e/output_format_test.go tests/e2e/task_queue_test.go tests/e2e/player_test.go tests/e2e/error_handling_test.go
git commit -m "$(cat <<'EOF'
test(e2e): migrate remaining E2E tests from key:value to key=value

Covers urgency, output format, task queue, player, and error handling
test files.
EOF
)"
```

---

## Phase 3: Full Validation

### Task 10: Run complete test suite and fix any remaining references

**Files:**
- Potentially any file with stale `:` syntax

- [ ] **Step 1: Run full test suite with race detector**

Run: `make test-race`
Expected: ALL PASS

- [ ] **Step 2: Run linter**

Run: `make lint`
Expected: clean

- [ ] **Step 3: Run vet**

Run: `make vet`
Expected: clean

- [ ] **Step 4: Search for any remaining `key:value` filter patterns in Go files**

Run a grep across the codebase for patterns like `status:active`, `priority:`, `project:` etc. in string literals (excluding comments and this plan doc):

```bash
grep -rn '"[a-z_]*:[a-z]' --include='*.go' . | grep -v '_test.go' | grep -v 'docs/' | grep -v 'vendor/'
```

Examine each match. Some legitimate uses of `:` will remain (e.g., time formats like `15:30:00`, Go struct tags, format strings). Only filter/inline-syntax patterns need to change.

- [ ] **Step 5: Build the binary and smoke test**

Run: `make build`

Manually test (or verify via E2E):
```bash
./bin/tusk add "Test task" priority=3 project=_default +api
./bin/tusk list status=active
./bin/tusk list priority=2..4
./bin/tusk list status=pending,active +api
```

- [ ] **Step 6: Commit any remaining fixes (if needed)**

```bash
git add -A
git commit -m "$(cat <<'EOF'
fix: address remaining key:value references found during validation
EOF
)"
```

---

## Dependency Notes

- **Tasks 1-3** are independent and can be parallelized (they create new files in the `syntax/` package).
- **Task 4** depends on Tasks 1-3 (rewires `filter/` to import `syntax/`).
- **Task 5** depends on Task 4 (updates test inputs to match new separator).
- **Tasks 6-7** depend on Task 4 (CLI/MCP string updates).
- **Tasks 8-9** depend on Task 5 (E2E tests rely on filter package working).
- **Task 10** depends on all prior tasks (full validation).

## What This Plan Does NOT Cover

The following items from the ROADMAP are in **separate initiatives** that depend on this plan but are not part of it:

- **Status roles schema** (Workflow Management CLI initiative) — changing `WorkflowConfig.Statuses` from `[]string` to `map[string]StatusConfig`, removing `highlight_statuses`/`dim_statuses`, replacing hardcoded status strings in TaskService. This is a separate initiative with its own plan.
- **Workflow CRUD commands** — `tusk workflow create/modify/delete`. These consume the shared lexer but are a separate initiative.
- **Project CRUD commands** — `tusk project create/modify/delete`. Same.
- **Config package mutation functions** — `config.CreateWorkflow()`, etc. Separate initiative.
- **Local config discovery** — config resolution chain. Separate initiative.
