# Filter Parser Phase 1: Lexer and Token Types

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create the `internal/filter` package with token types, error types, and a lexer that tokenizes filter input strings.

**Architecture:** The lexer is the first stage of a three-stage pipeline (lexer -> parser -> resolver). It splits a raw input string into typed tokens by whitespace, classifying each as a field (`key:value`), tag include (`+word`), tag exclude (`-word`), or free text. It records byte positions for error reporting. No external dependencies.

**Tech Stack:** Go standard library only. Module: `github.com/germanamz/tusk`.

**Spec:** `docs/superpowers/specs/2026-04-02-filter-syntax-parser-design.md`

---

### Task 1: Error Types

**Files:**
- Create: `internal/filter/errors.go`
- Test: `internal/filter/errors_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/filter/errors_test.go`:

```go
package filter

import (
	"testing"
)

func TestParseError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  ParseError
		want string
	}{
		{
			name: "with field and position",
			err:  ParseError{Pos: 10, Field: "priority", Message: "expected 0-4"},
			want: `filter error at position 10: field "priority": expected 0-4`,
		},
		{
			name: "with position, no field",
			err:  ParseError{Pos: 5, Field: "", Message: "unexpected token"},
			want: `filter error at position 5: unexpected token`,
		},
		{
			name: "no position",
			err:  ParseError{Pos: -1, Field: "status", Message: "empty value"},
			want: `filter error: field "status": empty value`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatErrors(t *testing.T) {
	errs := []ParseError{
		{Pos: 0, Field: "foo", Message: "unknown field"},
		{Pos: 10, Field: "priority", Message: "invalid value"},
	}
	got := FormatErrors(errs)
	want := "filter error at position 0: field \"foo\": unknown field\nfilter error at position 10: field \"priority\": invalid value"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -run TestParseError -v`
Expected: FAIL — `ParseError` type not defined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/filter/errors.go`:

```go
package filter

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

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -run TestParseError -v && go test ./internal/filter/ -run TestFormatErrors -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/germanamz/projects/tusk
git add internal/filter/errors.go internal/filter/errors_test.go
git commit -m "feat(filter): add ParseError type and FormatErrors helper"
```

---

### Task 2: Token Types

**Files:**
- Create: `internal/filter/token.go`
- Test: `internal/filter/token_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/filter/token_test.go`:

```go
package filter

import (
	"testing"
)

func TestTokenType_String(t *testing.T) {
	tests := []struct {
		tt   TokenType
		want string
	}{
		{TokenField, "Field"},
		{TokenTagInclude, "TagInclude"},
		{TokenTagExclude, "TagExclude"},
		{TokenText, "Text"},
	}
	for _, tc := range tests {
		got := tc.tt.String()
		if got != tc.want {
			t.Fatalf("TokenType(%d).String() = %q, want %q", tc.tt, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -run TestTokenType -v`
Expected: FAIL — `TokenType` not defined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/filter/token.go`:

```go
package filter

// TokenType classifies a lexed token.
type TokenType int

const (
	TokenField      TokenType = iota // key:value
	TokenTagInclude                  // +word
	TokenTagExclude                  // -word
	TokenText                        // anything else
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
	default:
		return "Unknown"
	}
}

// Token is a single lexed element from a filter input string.
type Token struct {
	Type  TokenType
	Value string // raw text of the token
	Pos   int    // byte offset in the original input
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -run TestTokenType -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/germanamz/projects/tusk
git add internal/filter/token.go internal/filter/token_test.go
git commit -m "feat(filter): add Token and TokenType types"
```

---

### Task 3: Lexer

**Files:**
- Modify: `internal/filter/token.go` (add `Lex` function)
- Modify: `internal/filter/token_test.go` (add lexer tests)

- [ ] **Step 1: Write the failing tests**

Append to `internal/filter/token_test.go`:

```go
func TestLex(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   []Token
		errors int // expected number of ParseErrors
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
			name:  "field key:value",
			input: "status:active",
			want: []Token{
				{Type: TokenField, Value: "status:active", Pos: 0},
			},
		},
		{
			name:  "field with colon in value",
			input: "due:2026-04-10T15:30:00Z",
			want: []Token{
				{Type: TokenField, Value: "due:2026-04-10T15:30:00Z", Pos: 0},
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
			input: "My task project:backend +api -docs priority:3",
			want: []Token{
				{Type: TokenText, Value: "My", Pos: 0},
				{Type: TokenText, Value: "task", Pos: 3},
				{Type: TokenField, Value: "project:backend", Pos: 8},
				{Type: TokenTagInclude, Value: "+api", Pos: 24},
				{Type: TokenTagExclude, Value: "-docs", Pos: 29},
				{Type: TokenField, Value: "priority:3", Pos: 35},
			},
		},
		{
			name:   "bare plus sign",
			input:  "title + more",
			want: []Token{
				{Type: TokenText, Value: "title", Pos: 0},
				{Type: TokenText, Value: "more", Pos: 8},
			},
			errors: 1,
		},
		{
			name:   "bare minus sign",
			input:  "title - more",
			want: []Token{
				{Type: TokenText, Value: "title", Pos: 0},
				{Type: TokenText, Value: "more", Pos: 8},
			},
			errors: 1,
		},
		{
			name:  "multiple spaces between tokens",
			input: "status:active   +api",
			want: []Token{
				{Type: TokenField, Value: "status:active", Pos: 0},
				{Type: TokenTagInclude, Value: "+api", Pos: 16},
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

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -run TestLex -v`
Expected: FAIL — `Lex` function not defined.

- [ ] **Step 3: Write minimal implementation**

Add the `Lex` function to `internal/filter/token.go`:

```go
// Lex splits the input string into tokens. It returns all tokens it could
// produce plus any errors encountered (e.g., bare +/- signs). Processing
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

		// Find the end of this token (next whitespace or end of input)
		start := i
		for i < len(input) && input[i] != ' ' && input[i] != '\t' {
			i++
		}
		raw := input[start:i]

		// Classify the token
		switch {
		case len(raw) == 1 && (raw[0] == '+' || raw[0] == '-'):
			errs = append(errs, ParseError{
				Pos:     start,
				Message: fmt.Sprintf("bare %q is not a valid token; use %s<name> for tags", raw, raw),
			})

		case raw[0] == '+':
			tokens = append(tokens, Token{Type: TokenTagInclude, Value: raw, Pos: start})

		case raw[0] == '-' && !isFieldToken(raw):
			tokens = append(tokens, Token{Type: TokenTagExclude, Value: raw, Pos: start})

		case isFieldToken(raw):
			tokens = append(tokens, Token{Type: TokenField, Value: raw, Pos: start})

		default:
			tokens = append(tokens, Token{Type: TokenText, Value: raw, Pos: start})
		}
	}

	return tokens, errs
}

// isFieldToken returns true if the raw token contains a colon and has a
// non-empty key (i.e., it's not just ":value").
func isFieldToken(raw string) bool {
	idx := indexByte(raw, ':')
	return idx > 0
}

// indexByte returns the index of the first occurrence of c in s, or -1.
func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -run TestLex -v`
Expected: PASS

- [ ] **Step 5: Run all filter package tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
cd /Users/germanamz/projects/tusk
git add internal/filter/token.go internal/filter/token_test.go
git commit -m "feat(filter): add Lex function to tokenize filter input strings"
```

---

### Task 4: Lexer Edge Cases

**Files:**
- Modify: `internal/filter/token.go` (if fixes needed)
- Modify: `internal/filter/token_test.go` (add edge case tests)

- [ ] **Step 1: Write the failing tests**

Append to `internal/filter/token_test.go`:

```go
func TestLex_EdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   []Token
		errors int
	}{
		{
			name:  "field with empty value",
			input: "status:",
			want: []Token{
				{Type: TokenField, Value: "status:", Pos: 0},
			},
		},
		{
			name:  "colon at start is text not field",
			input: ":value",
			want: []Token{
				{Type: TokenText, Value: ":value", Pos: 0},
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
			name:  "tag-like token with colon is a field",
			input: "+api:test",
			want: []Token{
				{Type: TokenField, Value: "+api:test", Pos: 0},
			},
		},
		{
			name:   "multiple errors collected",
			input:  "+ text -",
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
			name:  "priority range is a field",
			input: "priority:2..4",
			want: []Token{
				{Type: TokenField, Value: "priority:2..4", Pos: 0},
			},
		},
		{
			name:  "due date range is a field",
			input: "due:today..friday",
			want: []Token{
				{Type: TokenField, Value: "due:today..friday", Pos: 0},
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

- [ ] **Step 2: Run tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -run TestLex_EdgeCases -v`

If any tests fail, fix the `Lex` function to handle:
- `+api:test` should be classified as `TokenField` (it has a colon with a non-empty key). Update the classification order in `Lex`: check `isFieldToken` *before* checking `+`/`-` prefixes.
- `:value` (colon at position 0) should be `TokenText` since there's no key. `isFieldToken` already handles this (requires `idx > 0`).

If the field-before-tag check is needed, update the `switch` in `Lex` so the `isFieldToken(raw)` case comes first:

```go
		switch {
		case len(raw) == 1 && (raw[0] == '+' || raw[0] == '-'):
			errs = append(errs, ParseError{
				Pos:     start,
				Message: fmt.Sprintf("bare %q is not a valid token; use %s<name> for tags", raw, raw),
			})

		case isFieldToken(raw):
			tokens = append(tokens, Token{Type: TokenField, Value: raw, Pos: start})

		case raw[0] == '+':
			tokens = append(tokens, Token{Type: TokenTagInclude, Value: raw, Pos: start})

		case raw[0] == '-':
			tokens = append(tokens, Token{Type: TokenTagExclude, Value: raw, Pos: start})

		default:
			tokens = append(tokens, Token{Type: TokenText, Value: raw, Pos: start})
		}
```

- [ ] **Step 3: Run all filter package tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/filter/ -v`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
cd /Users/germanamz/projects/tusk
git add internal/filter/token.go internal/filter/token_test.go
git commit -m "test(filter): add lexer edge case tests and fix classification order"
```
