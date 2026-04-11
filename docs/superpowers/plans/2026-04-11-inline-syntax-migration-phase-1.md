# Inline Syntax Migration — Phase 1: Create Shared `syntax/` Package

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a new `syntax/` package containing shared error types, AST types, and a lexer that uses `=` as the field separator with first-class modifier support.

**Architecture:** This phase is purely additive — no existing code is modified. The `syntax/` package provides the foundation that Phase 2 will wire into the `filter/` package. The lexer uses `=` as the field separator (freeing `:` for use as a value modifier), preserves modifiers (`,`, `:`, `..`, `()`, `+`, `-`) in raw token values, treats quoted strings as opaque literals, and disambiguates group parentheses from boolean grouping by position.

**Tech Stack:** Go standard library only.

**Prerequisites:** None — this phase operates on the base codebase only.

**Task count:** 3 (intentionally narrow — this phase is purely additive, creates no bridge code, and produces an independently testable package with zero modifications to existing code. Merging with Phase 2 would mix new-package creation with existing-code modification.)

---

## Context

Tusk is a Go CLI task manager. The `filter/` package (`/Users/germanamz/projects/tusk/filter/`) currently owns all tokenization, parsing, and resolution for inline syntax. It uses `:` as the field separator (e.g., `status:active`, `priority:3`). The ROADMAP calls for migrating to `=` as the separator so `:` can be used in values (e.g., `transition=pending:active` for workflow transitions).

This phase creates the shared parsing primitives. Phase 2 wires them into `filter/`. Phase 3 migrates all consumers and removes the legacy `:` support.

**Reference files (read for context, do not modify):**
- `filter/errors.go` — current error types (source for Task 1)
- `filter/ast.go` — current AST types (source for Task 2)
- `filter/token.go` — current lexer (source for Task 3)

---

## Task 1: Create `syntax/errors.go` — shared error types

**Files:**
- Create: `syntax/errors.go`
- Create: `syntax/errors_test.go`

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
Expected: FAIL — package does not exist yet

- [ ] **Step 3: Write the implementation**

Create `syntax/errors.go`:

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

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -v ./syntax/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add syntax/errors.go syntax/errors_test.go
git commit -m "$(cat <<'EOF'
refactor(syntax): extract ParseError and FormatErrors from filter package

Foundation for the shared syntax package that will be used by both
filter expressions and future inline syntax consumers (workflow and
project management commands).
EOF
)"
```

---

## Task 2: Create `syntax/ast.go` — shared AST types

**Files:**
- Create: `syntax/ast.go`
- Create: `syntax/ast_test.go`

- [ ] **Step 1: Write the failing test**

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
Expected: FAIL — `FilterSet` not defined

- [ ] **Step 3: Write the implementation**

Create `syntax/ast.go`:

```go
package syntax

import "strings"

// FilterSet is a collection of parsed inline syntax terms, implicitly AND'd.
// Used by filter expressions and task creation/modification commands.
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

FilterSet, FieldFilter, and TagFilter are the building blocks for
both filter expressions and inline task creation/modification syntax.
EOF
)"
```

---

## Task 3: Create `syntax/token.go` — shared lexer with `=` separator

This is the core lexer. It uses `=` as the field separator and understands modifiers as first-class primitives preserved in raw token values.

**Files:**
- Create: `syntax/token.go`
- Create: `syntax/token_test.go`

- [ ] **Step 1: Write the failing test**

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
			name:  "boolean grouping parentheses",
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
			name:  "colon in value is preserved",
			input: "due=2026-04-10T15:30:00Z",
			want: []Token{
				{Type: TokenField, Value: "due=2026-04-10T15:30:00Z", Pos: 0},
			},
		},
		{
			name:  "colon-only token is text not field",
			input: "status:active",
			want: []Token{
				{Type: TokenText, Value: "status:active", Pos: 0},
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
			name:  "parens attached to tokens are boolean grouping",
			input: "(status=active)",
			want: []Token{
				{Type: TokenLParen, Value: "(", Pos: 0},
				{Type: TokenField, Value: "status=active", Pos: 1},
				{Type: TokenRParen, Value: ")", Pos: 14},
			},
		},
		{
			name:  "nested groups in field value",
			input: "status=done(terminal,done,dim)",
			want: []Token{
				{Type: TokenField, Value: "status=done(terminal,done,dim)", Pos: 0},
			},
		},
		{
			name:  "multiple fields with groups",
			input: "status=pending(initial) status=active(start,highlight)",
			want: []Token{
				{Type: TokenField, Value: "status=pending(initial)", Pos: 0},
				{Type: TokenField, Value: "status=active(start,highlight)", Pos: 23},
			},
		},
		{
			name:  "comma-separated transitions with colon sequence",
			input: "transition=pending:active,active:completed",
			want: []Token{
				{Type: TokenField, Value: "transition=pending:active,active:completed", Pos: 0},
			},
		},
		{
			name:  "due date range",
			input: "due=today..friday",
			want: []Token{
				{Type: TokenField, Value: "due=today..friday", Pos: 0},
			},
		},
		{
			name:  "tag without equals is not a field",
			input: "+api:test",
			want: []Token{
				{Type: TokenTagInclude, Value: "+api:test", Pos: 0},
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
// Parentheses immediately after a value (no whitespace) are part of the value
// (group modifier); parentheses preceded by whitespace are boolean grouping.
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

		// Parentheses preceded by whitespace (or at start of input) are boolean grouping
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

		// Scan a token: characters until whitespace.
		// Quotes mid-token (key="value") are inlined.
		// ( immediately after prior chars is a group modifier (tracked via parenDepth).
		// ) at depth 0 is a boolean close — stop.
		var buf []byte
		unclosedQuote := false
		parenDepth := 0
		for i < len(input) && input[i] != ' ' && input[i] != '\t' {
			if input[i] == ')' && parenDepth == 0 {
				break
			}
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
			if hasEquals(raw[1:]) {
				tokens = append(tokens, Token{Type: TokenField, Value: raw, Pos: start})
			} else {
				tokens = append(tokens, Token{Type: TokenTagInclude, Value: raw, Pos: start})
			}

		case raw[0] == '-':
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

// isFieldToken returns true if raw contains = with a non-empty key.
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

Run: `go test -v ./syntax/...`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add syntax/token.go syntax/token_test.go
git commit -m "$(cat <<'EOF'
feat(syntax): shared lexer with = separator and modifier support

Uses = as the field separator (freeing : for use in values like
workflow transitions). Modifiers (, : .. () + -) are preserved in
raw token values. Quoted strings are opaque. Group parens are
position-disambiguated from boolean grouping parens.
EOF
)"
```

---

## Changes Introduced

| Category | Details |
|----------|---------|
| **New files** | `syntax/errors.go`, `syntax/errors_test.go`, `syntax/ast.go`, `syntax/ast_test.go`, `syntax/token.go`, `syntax/token_test.go` |
| **Modified files** | None |
| **New dependencies** | None |
| **New interfaces** | None |
| **Schema migrations** | None |
| **Bridge code** | None |
| **User-visible behavior changes** | None — this phase is purely additive |

The `syntax/` package is self-contained and has no consumers yet. All existing code, tests, and behavior are unchanged.
