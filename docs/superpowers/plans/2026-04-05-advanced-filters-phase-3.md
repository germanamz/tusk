# Phase 3: Boolean Operator Tokens + Expression Tree AST + Pratt Parser Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `AND`, `OR`, `NOT` keyword tokens and `(`, `)` delimiter tokens to the lexer, define an expression tree AST (`Expr`, `AndExpr`, `OrExpr`, `NotExpr`, `TermExpr`), and implement a Pratt precedence-climbing parser (`ParseExpr`) that produces the tree. This phase is purely about parsing — no resolver, SQL, or wiring changes.

**Architecture:** The lexer gains 5 new token types. A new `ParseExpr` function uses Pratt parsing with implicit AND between adjacent terms. The existing `Parse` function and `FilterSet` type are untouched — they continue to serve `tusk add` and `tusk modify`. The new `ParseExpr` is the entry point for boolean filter queries (wired in Phase 4).

**Tech Stack:** Go standard library only (no new dependencies).

**Prerequisites:** Phase 1 must be completed. Phase 1 introduced the character-by-character scanner in `Lex()` which this phase extends with parenthesis delimiters and keyword recognition. Phase 2 is also required (it adds `title` and `description` to `fieldValidators`, which `ParseExpr` reuses).

---

## Inherits From

**Phase 1** modified:
- `internal/filter/token.go` — `Lex()` is now a character scanner with `scanQuoted()`. Whitespace delimits tokens outside quotes. `"` enters quoted mode.

**Phase 2** modified:
- `internal/filter/validators.go` — Added `validateNonEmpty`
- `internal/filter/parser.go` — `fieldValidators` map now includes `"title"` and `"description"` entries
- `internal/filter/resolve.go` — Handles `title` and `description` cases
- `internal/domain/filter.go` — `TaskFilter` has `TitleContains` and `DescriptionContains` fields

The implementer can rely on:
- The lexer is a character scanner (not split-on-whitespace). Adding `(` and `)` as delimiters and keyword detection for `AND`/`OR`/`NOT` fits naturally.
- `fieldValidators` in `parser.go` contains all field validators including `title` and `description`.

---

### Task 1: Add New Token Types to the Lexer

**Files:**
- Modify: `internal/filter/token.go`
- Modify: `internal/filter/token_test.go`

- [ ] **Step 1: Add token type constants**

In `internal/filter/token.go`, add 5 new constants after `TokenText` (line 12):

```go
const (
	TokenField      TokenType = iota // key:value
	TokenTagInclude                  // +word
	TokenTagExclude                  // -word
	TokenText                        // anything else
	TokenAnd                         // AND
	TokenOr                          // OR
	TokenNot                         // NOT
	TokenLParen                      // (
	TokenRParen                      // )
)
```

- [ ] **Step 2: Update TokenType.String()**

Add cases to the `String()` method (inside the switch, after the `TokenText` case):

```go
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
```

- [ ] **Step 3: Write failing lexer tests for new tokens**

Add these test cases to the `tests` slice inside `TestLex` in `internal/filter/token_test.go`:

```go
{
    name:  "AND keyword",
    input: "status:active AND +api",
    want: []Token{
        {Type: TokenField, Value: "status:active", Pos: 0},
        {Type: TokenAnd, Value: "AND", Pos: 14},
        {Type: TokenTagInclude, Value: "+api", Pos: 18},
    },
},
{
    name:  "OR keyword",
    input: "status:active OR status:pending",
    want: []Token{
        {Type: TokenField, Value: "status:active", Pos: 0},
        {Type: TokenOr, Value: "OR", Pos: 14},
        {Type: TokenField, Value: "status:pending", Pos: 17},
    },
},
{
    name:  "NOT keyword",
    input: "NOT status:deleted",
    want: []Token{
        {Type: TokenNot, Value: "NOT", Pos: 0},
        {Type: TokenField, Value: "status:deleted", Pos: 4},
    },
},
{
    name:  "parentheses",
    input: "(status:active OR +urgent)",
    want: []Token{
        {Type: TokenLParen, Value: "(", Pos: 0},
        {Type: TokenField, Value: "status:active", Pos: 1},
        {Type: TokenOr, Value: "OR", Pos: 15},
        {Type: TokenTagInclude, Value: "+urgent", Pos: 18},
        {Type: TokenRParen, Value: ")", Pos: 25},
    },
},
{
    name:  "lowercase and is text not keyword",
    input: "and or not",
    want: []Token{
        {Type: TokenText, Value: "and", Pos: 0},
        {Type: TokenText, Value: "or", Pos: 4},
        {Type: TokenText, Value: "not", Pos: 7},
    },
},
{
    name:  "parens attached to tokens",
    input: "(status:active)",
    want: []Token{
        {Type: TokenLParen, Value: "(", Pos: 0},
        {Type: TokenField, Value: "status:active", Pos: 1},
        {Type: TokenRParen, Value: ")", Pos: 14},
    },
},
```

- [ ] **Step 4: Run to verify failure**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/filter/ -run "TestLex$" -count=1`
Expected: FAIL — lexer doesn't recognize new token types.

- [ ] **Step 5: Modify Lex() to recognize keywords and parentheses**

In `internal/filter/token.go`, modify the `Lex` function. The changes are:

1. Before scanning an unquoted token, check if the current character is `(` or `)` and emit `TokenLParen`/`TokenRParen` immediately:

Add this block after the standalone quoted string handling (the `if input[i] == '"'` block) and before the unquoted scanning loop:

```go
		// Parentheses are always single-character tokens
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
```

2. In the unquoted scanning loop, also break on `(` and `)`:

Change the inner loop condition from:
```go
for i < len(input) && input[i] != ' ' && input[i] != '\t' {
```
to:
```go
for i < len(input) && input[i] != ' ' && input[i] != '\t' && input[i] != '(' && input[i] != ')' {
```

3. In the classification switch at the end, add keyword detection before the default case. After the `case raw[0] == '-':` block, add:

```go
		case raw == "AND":
			tokens = append(tokens, Token{Type: TokenAnd, Value: raw, Pos: start})

		case raw == "OR":
			tokens = append(tokens, Token{Type: TokenOr, Value: raw, Pos: start})

		case raw == "NOT":
			tokens = append(tokens, Token{Type: TokenNot, Value: raw, Pos: start})
```

These cases must come after the `isFieldToken(raw)` check (so `AND:value` is still a field) and after the tag checks (so `+AND` is still a tag), but before the `default:` text fallback.

- [ ] **Step 6: Run all lexer tests**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/filter/ -run "TestLex|TestTokenType"`
Expected: ALL PASS.

- [ ] **Step 7: Run the full test suite**

Run: `cd /Users/germanamz/projects/tusk && make test`
Expected: ALL PASS. Existing tests should not break because `AND`, `OR`, `NOT` were previously `TokenText` and the existing `Parse` function treats unknown text as `TokenText` anyway — they just become `fs.Text` entries. The `Parse` function ignores unknown token types gracefully.

**Important:** If any existing test expects `AND`, `OR`, or `NOT` to be `TokenText`, update it to expect the new token type. Check `TestLex` and `TestLex_EdgeCases` test cases — if any include these exact strings as text inputs, they may need updating.

- [ ] **Step 8: Commit**

```bash
git add internal/filter/token.go internal/filter/token_test.go
git commit -m "$(cat <<'EOF'
feat(filter): add AND/OR/NOT keyword tokens and parenthesis delimiters

Extend lexer with TokenAnd, TokenOr, TokenNot, TokenLParen, TokenRParen.
Keywords are all-caps only. Parentheses are implicit delimiters.
EOF
)"
```

---

### Task 2: Define Expression Tree AST Types

**Files:**
- Create: `internal/filter/expr.go`
- Test: `internal/filter/expr_test.go`

- [ ] **Step 1: Create the expression tree types**

Create `internal/filter/expr.go`:

```go
package filter

// Expr is the interface for all filter expression nodes.
type Expr interface {
	exprNode() // marker method — prevents external implementations
}

// AndExpr groups child expressions with AND semantics.
// All children must match for the expression to match.
type AndExpr struct {
	Children []Expr
}

// OrExpr groups child expressions with OR semantics.
// At least one child must match for the expression to match.
type OrExpr struct {
	Children []Expr
}

// NotExpr negates a single child expression.
type NotExpr struct {
	Child Expr
}

// TermExpr wraps a single filter term — exactly one of Field, Tag, or Text
// is set.
type TermExpr struct {
	Field *FieldFilter // non-nil for field terms (e.g. status:active)
	Tag   *TagFilter   // non-nil for tag terms (e.g. +api, -docs)
	Text  string       // non-empty for free text terms
}

func (AndExpr) exprNode()  {}
func (OrExpr) exprNode()   {}
func (NotExpr) exprNode()  {}
func (TermExpr) exprNode() {}
```

- [ ] **Step 2: Write basic AST construction tests**

Create `internal/filter/expr_test.go`:

```go
package filter

import "testing"

func TestExpr_Marker(t *testing.T) {
	// Verify all types satisfy the Expr interface at compile time
	var _ Expr = AndExpr{}
	var _ Expr = OrExpr{}
	var _ Expr = NotExpr{}
	var _ Expr = TermExpr{}
}

func TestAndExpr_Children(t *testing.T) {
	expr := AndExpr{
		Children: []Expr{
			TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
			TermExpr{Tag: &TagFilter{Name: "api"}},
		},
	}
	if len(expr.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(expr.Children))
	}
}

func TestOrExpr_Children(t *testing.T) {
	expr := OrExpr{
		Children: []Expr{
			TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
			TermExpr{Field: &FieldFilter{Key: "status", Value: "pending"}},
		},
	}
	if len(expr.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(expr.Children))
	}
}

func TestNotExpr_Child(t *testing.T) {
	expr := NotExpr{
		Child: TermExpr{Field: &FieldFilter{Key: "status", Value: "deleted"}},
	}
	term, ok := expr.Child.(TermExpr)
	if !ok {
		t.Fatal("expected TermExpr child")
	}
	if term.Field.Value != "deleted" {
		t.Fatalf("expected value deleted, got %q", term.Field.Value)
	}
}

func TestTermExpr_Variants(t *testing.T) {
	// Field term
	ft := TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}}
	if ft.Field == nil {
		t.Fatal("expected non-nil field")
	}

	// Tag term
	tt := TermExpr{Tag: &TagFilter{Name: "api"}}
	if tt.Tag == nil {
		t.Fatal("expected non-nil tag")
	}

	// Text term
	txt := TermExpr{Text: "hello"}
	if txt.Text != "hello" {
		t.Fatalf("expected text hello, got %q", txt.Text)
	}
}
```

- [ ] **Step 3: Run AST tests**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/filter/ -run "TestExpr|TestAndExpr|TestOrExpr|TestNotExpr|TestTermExpr"`
Expected: ALL PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/filter/expr.go internal/filter/expr_test.go
git commit -m "$(cat <<'EOF'
feat(filter): add expression tree AST types for boolean filters

Define Expr interface with AndExpr, OrExpr, NotExpr, and TermExpr
implementations. These will be used by the Pratt parser in ParseExpr.
EOF
)"
```

---

### Task 3: Implement ParseExpr with Pratt Precedence Climbing

**Files:**
- Create: `internal/filter/parse_expr.go`
- Test: `internal/filter/parse_expr_test.go`

- [ ] **Step 1: Write failing tests for ParseExpr**

Create `internal/filter/parse_expr_test.go`:

```go
package filter

import "testing"

// exprEqual is a test helper that compares two Expr trees structurally.
func exprEqual(a, b Expr) bool {
	switch a := a.(type) {
	case TermExpr:
		b, ok := b.(TermExpr)
		if !ok {
			return false
		}
		if a.Text != b.Text {
			return false
		}
		if (a.Field == nil) != (b.Field == nil) {
			return false
		}
		if a.Field != nil && (a.Field.Key != b.Field.Key || a.Field.Value != b.Field.Value) {
			return false
		}
		if (a.Tag == nil) != (b.Tag == nil) {
			return false
		}
		if a.Tag != nil && (a.Tag.Name != b.Tag.Name || a.Tag.Exclude != b.Tag.Exclude) {
			return false
		}
		return true
	case AndExpr:
		b, ok := b.(AndExpr)
		if !ok || len(a.Children) != len(b.Children) {
			return false
		}
		for i := range a.Children {
			if !exprEqual(a.Children[i], b.Children[i]) {
				return false
			}
		}
		return true
	case OrExpr:
		b, ok := b.(OrExpr)
		if !ok || len(a.Children) != len(b.Children) {
			return false
		}
		for i := range a.Children {
			if !exprEqual(a.Children[i], b.Children[i]) {
				return false
			}
		}
		return true
	case NotExpr:
		b, ok := b.(NotExpr)
		if !ok {
			return false
		}
		return exprEqual(a.Child, b.Child)
	default:
		return false
	}
}

func TestParseExpr_SingleTerm(t *testing.T) {
	expr, errs := ParseExpr("status:active")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}}
	if !exprEqual(expr, want) {
		t.Fatalf("expected %+v, got %+v", want, expr)
	}
}

func TestParseExpr_ImplicitAnd(t *testing.T) {
	expr, errs := ParseExpr("status:active +api")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := AndExpr{Children: []Expr{
		TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		TermExpr{Tag: &TagFilter{Name: "api"}},
	}}
	if !exprEqual(expr, want) {
		t.Fatalf("expected AndExpr with 2 children, got %+v", expr)
	}
}

func TestParseExpr_ExplicitAnd(t *testing.T) {
	expr, errs := ParseExpr("status:active AND +api")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := AndExpr{Children: []Expr{
		TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		TermExpr{Tag: &TagFilter{Name: "api"}},
	}}
	if !exprEqual(expr, want) {
		t.Fatalf("expected AndExpr with 2 children, got %+v", expr)
	}
}

func TestParseExpr_Or(t *testing.T) {
	expr, errs := ParseExpr("status:active OR status:pending")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := OrExpr{Children: []Expr{
		TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		TermExpr{Field: &FieldFilter{Key: "status", Value: "pending"}},
	}}
	if !exprEqual(expr, want) {
		t.Fatalf("expected OrExpr with 2 children, got %+v", expr)
	}
}

func TestParseExpr_Not(t *testing.T) {
	expr, errs := ParseExpr("NOT status:deleted")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := NotExpr{
		Child: TermExpr{Field: &FieldFilter{Key: "status", Value: "deleted"}},
	}
	if !exprEqual(expr, want) {
		t.Fatalf("expected NotExpr, got %+v", expr)
	}
}

func TestParseExpr_Precedence_AndBeforeOr(t *testing.T) {
	// "a OR b AND c" should parse as "a OR (b AND c)"
	// because AND binds tighter than OR
	expr, errs := ParseExpr("status:active OR +api priority:3")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := OrExpr{Children: []Expr{
		TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		AndExpr{Children: []Expr{
			TermExpr{Tag: &TagFilter{Name: "api"}},
			TermExpr{Field: &FieldFilter{Key: "priority", Value: "3"}},
		}},
	}}
	if !exprEqual(expr, want) {
		t.Fatalf("precedence wrong: expected OR(term, AND(term, term)), got %+v", expr)
	}
}

func TestParseExpr_Parentheses(t *testing.T) {
	// "(a OR b) AND c" — parens override default precedence
	expr, errs := ParseExpr("(status:active OR status:pending) +api")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := AndExpr{Children: []Expr{
		OrExpr{Children: []Expr{
			TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
			TermExpr{Field: &FieldFilter{Key: "status", Value: "pending"}},
		}},
		TermExpr{Tag: &TagFilter{Name: "api"}},
	}}
	if !exprEqual(expr, want) {
		t.Fatalf("expected AND(OR(...), term), got %+v", expr)
	}
}

func TestParseExpr_NestedNot(t *testing.T) {
	expr, errs := ParseExpr("NOT NOT status:active")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := NotExpr{
		Child: NotExpr{
			Child: TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		},
	}
	if !exprEqual(expr, want) {
		t.Fatalf("expected NOT(NOT(term)), got %+v", expr)
	}
}

func TestParseExpr_EmptyInput(t *testing.T) {
	expr, errs := ParseExpr("")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if expr != nil {
		t.Fatalf("expected nil expr for empty input, got %+v", expr)
	}
}

func TestParseExpr_MismatchedParen(t *testing.T) {
	_, errs := ParseExpr("(status:active")
	if len(errs) == 0 {
		t.Fatal("expected error for unclosed paren")
	}
}

func TestParseExpr_UnexpectedRParen(t *testing.T) {
	_, errs := ParseExpr("status:active)")
	if len(errs) == 0 {
		t.Fatal("expected error for unexpected )")
	}
}

func TestParseExpr_TagExclude(t *testing.T) {
	expr, errs := ParseExpr("-docs")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := TermExpr{Tag: &TagFilter{Name: "docs", Exclude: true}}
	if !exprEqual(expr, want) {
		t.Fatalf("expected exclude tag term, got %+v", expr)
	}
}

func TestParseExpr_TextTerm(t *testing.T) {
	expr, errs := ParseExpr("sometext")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := TermExpr{Text: "sometext"}
	if !exprEqual(expr, want) {
		t.Fatalf("expected text term, got %+v", expr)
	}
}

func TestParseExpr_ComplexExpression(t *testing.T) {
	// (project:backend OR project:frontend) AND +api AND NOT status:deleted
	expr, errs := ParseExpr(`(project:backend OR project:frontend) AND +api AND NOT status:deleted`)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := AndExpr{Children: []Expr{
		OrExpr{Children: []Expr{
			TermExpr{Field: &FieldFilter{Key: "project", Value: "backend"}},
			TermExpr{Field: &FieldFilter{Key: "project", Value: "frontend"}},
		}},
		TermExpr{Tag: &TagFilter{Name: "api"}},
		NotExpr{Child: TermExpr{Field: &FieldFilter{Key: "status", Value: "deleted"}}},
	}}
	if !exprEqual(expr, want) {
		t.Fatalf("complex expression mismatch, got %+v", expr)
	}
}

func TestParseExpr_FieldValidation(t *testing.T) {
	// Unknown field should produce an error but continue
	_, errs := ParseExpr("foo:bar OR status:active")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for unknown field, got %d: %v", len(errs), errs)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/filter/ -run "TestParseExpr"`
Expected: FAIL — `ParseExpr` doesn't exist yet.

- [ ] **Step 3: Implement ParseExpr**

Create `internal/filter/parse_expr.go`:

```go
package filter

import (
	"fmt"
	"strings"

	"github.com/germanamz/tusk/internal/domain"
)

// ParseExpr parses a filter string into a boolean expression tree.
// It supports AND, OR, NOT operators and parenthesized grouping.
// Adjacent terms without an explicit operator are implicitly AND'd.
// Returns nil Expr for empty input. Errors are collected, not fail-fast.
func ParseExpr(input string) (Expr, []ParseError) {
	tokens, lexErrs := Lex(input)

	var errs []ParseError
	errs = append(errs, lexErrs...)

	if len(tokens) == 0 {
		return nil, errs
	}

	p := &exprParser{
		tokens: tokens,
		pos:    0,
		errs:   errs,
	}

	expr := p.parseOr()

	// Check for leftover tokens (e.g., unmatched ")")
	if p.pos < len(p.tokens) {
		tok := p.tokens[p.pos]
		if tok.Type == TokenRParen {
			p.errs = append(p.errs, ParseError{
				Pos:     tok.Pos,
				Message: "unexpected ')'",
			})
		}
	}

	return expr, p.errs
}

type exprParser struct {
	tokens []Token
	pos    int
	errs   []ParseError
}

func (p *exprParser) peek() (Token, bool) {
	if p.pos >= len(p.tokens) {
		return Token{}, false
	}
	return p.tokens[p.pos], true
}

func (p *exprParser) advance() Token {
	tok := p.tokens[p.pos]
	p.pos++
	return tok
}

// parseOr: or_expr = and_expr ("OR" and_expr)*
func (p *exprParser) parseOr() Expr {
	left := p.parseAnd()
	if left == nil {
		return nil
	}

	children := []Expr{left}
	for {
		tok, ok := p.peek()
		if !ok || tok.Type != TokenOr {
			break
		}
		p.advance() // consume OR
		right := p.parseAnd()
		if right == nil {
			p.errs = append(p.errs, ParseError{
				Pos:     tok.Pos,
				Message: "expected expression after OR",
			})
			break
		}
		children = append(children, right)
	}

	if len(children) == 1 {
		return children[0]
	}
	return OrExpr{Children: children}
}

// parseAnd: and_expr = unary (("AND")? unary)*
// Adjacent terms without explicit AND are implicit AND.
func (p *exprParser) parseAnd() Expr {
	left := p.parseUnary()
	if left == nil {
		return nil
	}

	children := []Expr{left}
	for {
		tok, ok := p.peek()
		if !ok {
			break
		}

		// Explicit AND
		if tok.Type == TokenAnd {
			p.advance() // consume AND
			right := p.parseUnary()
			if right == nil {
				p.errs = append(p.errs, ParseError{
					Pos:     tok.Pos,
					Message: "expected expression after AND",
				})
				break
			}
			children = append(children, right)
			continue
		}

		// Implicit AND: if the next token is a term, NOT, or LParen, it's implicitly AND'd.
		// Stop on OR, RParen, or end of tokens.
		if tok.Type == TokenOr || tok.Type == TokenRParen {
			break
		}

		// Must be a term-starting token (Field, TagInclude, TagExclude, Text, Not, LParen)
		right := p.parseUnary()
		if right == nil {
			break
		}
		children = append(children, right)
	}

	if len(children) == 1 {
		return children[0]
	}
	return AndExpr{Children: children}
}

// parseUnary: unary = "NOT" unary | primary
func (p *exprParser) parseUnary() Expr {
	tok, ok := p.peek()
	if !ok {
		return nil
	}

	if tok.Type == TokenNot {
		p.advance() // consume NOT
		child := p.parseUnary()
		if child == nil {
			p.errs = append(p.errs, ParseError{
				Pos:     tok.Pos,
				Message: "expected expression after NOT",
			})
			return nil
		}
		return NotExpr{Child: child}
	}

	return p.parsePrimary()
}

// parsePrimary: primary = "(" expr ")" | term
func (p *exprParser) parsePrimary() Expr {
	tok, ok := p.peek()
	if !ok {
		return nil
	}

	if tok.Type == TokenLParen {
		p.advance() // consume (
		expr := p.parseOr()

		// Expect closing paren
		closeTok, closeOk := p.peek()
		if !closeOk || closeTok.Type != TokenRParen {
			p.errs = append(p.errs, ParseError{
				Pos:     tok.Pos,
				Message: "unclosed '('",
			})
			return expr // return what we have
		}
		p.advance() // consume )
		return expr
	}

	return p.parseTerm()
}

// parseTerm: term = field | tag_include | tag_exclude | text
func (p *exprParser) parseTerm() Expr {
	tok, ok := p.peek()
	if !ok {
		return nil
	}

	switch tok.Type {
	case TokenField:
		p.advance()
		key, value, _ := strings.Cut(tok.Value, ":")

		// Validate field — same logic as Parse() in parser.go
		if udaKey, ok := strings.CutPrefix(key, "uda."); ok {
			if udaKey == "" {
				p.errs = append(p.errs, ParseError{
					Pos:     tok.Pos,
					Field:   key,
					Message: "empty UDA key name",
				})
				return nil
			}
			if err := domain.ValidateUDAKey(udaKey); err != nil {
				p.errs = append(p.errs, ParseError{
					Pos:     tok.Pos,
					Field:   key,
					Message: err.Error(),
				})
				return nil
			}
			return TermExpr{Field: &FieldFilter{Key: key, Value: value, Pos: tok.Pos}}
		}

		validator, known := fieldValidators[key]
		if !known {
			p.errs = append(p.errs, ParseError{
				Pos:     tok.Pos,
				Field:   key,
				Message: "unknown field",
			})
			return nil
		}
		if err := validator(value); err != nil {
			p.errs = append(p.errs, ParseError{
				Pos:     tok.Pos,
				Field:   key,
				Message: err.Error(),
			})
			return nil
		}
		return TermExpr{Field: &FieldFilter{Key: key, Value: value, Pos: tok.Pos}}

	case TokenTagInclude:
		p.advance()
		return TermExpr{Tag: &TagFilter{Name: tok.Value[1:], Pos: tok.Pos}}

	case TokenTagExclude:
		p.advance()
		return TermExpr{Tag: &TagFilter{Name: tok.Value[1:], Exclude: true, Pos: tok.Pos}}

	case TokenText:
		p.advance()
		return TermExpr{Text: tok.Value}

	default:
		// Unexpected token (e.g., AND/OR at start without a preceding term).
		// Don't consume — let the caller handle it.
		return nil
	}
}
```

- [ ] **Step 4: Run ParseExpr tests**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/filter/ -run "TestParseExpr"`
Expected: ALL PASS.

- [ ] **Step 5: Run the full test suite**

Run: `cd /Users/germanamz/projects/tusk && make test`
Expected: ALL PASS — `ParseExpr` is new code, not wired to anything yet.

- [ ] **Step 6: Commit**

```bash
git add internal/filter/parse_expr.go internal/filter/parse_expr_test.go
git commit -m "$(cat <<'EOF'
feat(filter): implement ParseExpr with Pratt precedence-climbing parser

ParseExpr produces a boolean expression tree (AndExpr, OrExpr, NotExpr,
TermExpr) from filter input. Supports AND/OR/NOT keywords, parenthesized
grouping, and implicit AND between adjacent terms.
EOF
)"
```

---

### Task 4: Verify Full Suite and Handle Parse() Compatibility

**Files:**
- Modify: `internal/filter/parser.go` (if needed)
- Modify: `internal/filter/parser_test.go` (if needed)

The existing `Parse()` function may now receive tokens of type `TokenAnd`, `TokenOr`, `TokenNot`, `TokenLParen`, `TokenRParen` from the updated lexer. Since `Parse()` uses a `switch tok.Type` that only handles `TokenField`, `TokenTagInclude`, `TokenTagExclude`, and `TokenText`, the new token types fall through silently (they're not in any case). This means they're ignored, which is correct for `Parse()` — it's used for input building (`tusk add`, `tusk modify`), not for queries.

- [ ] **Step 1: Verify Parse() handles new tokens gracefully**

Add at the end of `internal/filter/parser_test.go`:

```go
func TestParse_IgnoresKeywordsAndParens(t *testing.T) {
	// Parse is for input building (tusk add/modify). Boolean keywords
	// should be ignored — they'll be treated as unknown tokens and silently skipped.
	fs, errs := Parse("My AND task")
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	// AND is a keyword token, not TokenText, so Parse ignores it
	if fs.Title() != "My task" {
		t.Fatalf("expected title %q, got %q", "My task", fs.Title())
	}
}
```

- [ ] **Step 2: Run to check**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/filter/ -run "TestParse_IgnoresKeywordsAndParens"`
Expected: PASS — `Parse()` ignores tokens it doesn't recognize.

If it fails because the token falls through to a case that misbehaves, add an explicit no-op handling for the new token types in the `switch tok.Type` block in `Parse()` (parser.go line 32):

```go
		case TokenAnd, TokenOr, TokenNot, TokenLParen, TokenRParen:
			// Ignored in Parse — these are for ParseExpr
```

- [ ] **Step 3: Run the full test suite including E2E**

Run: `cd /Users/germanamz/projects/tusk && make test`
Expected: ALL PASS.

- [ ] **Step 4: Commit if changes were needed**

```bash
git add internal/filter/parser.go internal/filter/parser_test.go
git commit -m "$(cat <<'EOF'
test(filter): verify Parse ignores boolean keyword tokens

Parse is for input building (tusk add/modify), not queries.
AND/OR/NOT/parens tokens are silently ignored.
EOF
)"
```

---

## Changes Introduced

**New files:**
- `internal/filter/expr.go` — `Expr` interface, `AndExpr`, `OrExpr`, `NotExpr`, `TermExpr` types
- `internal/filter/expr_test.go` — AST construction tests
- `internal/filter/parse_expr.go` — `ParseExpr` function with Pratt parser, `exprParser` type
- `internal/filter/parse_expr_test.go` — Comprehensive ParseExpr tests (precedence, parens, errors)

**Modified files:**
- `internal/filter/token.go` — 5 new `TokenType` constants (`TokenAnd`, `TokenOr`, `TokenNot`, `TokenLParen`, `TokenRParen`), updated `String()`, updated `Lex()` with keyword detection and paren delimiters
- `internal/filter/token_test.go` — Test cases for new token types
- `internal/filter/parser.go` — May have explicit no-op case for new token types (if needed)
- `internal/filter/parser_test.go` — Test for Parse ignoring keywords

**No new dependencies, migrations, or environment variables.**

**No bridge code introduced.** `ParseExpr` is new functionality, not wired to any consumer yet. Phase 4 wires it into CLI, resolver, SQL, and MCP.

**User-visible behaviors preserved:**
- All existing CLI commands work unchanged
- `tusk add`, `tusk modify` work unchanged (Parse/FilterSet path)
- `tusk list` still uses `Parse` → `Resolve` → `List` path (Phase 4 switches it)
- All existing MCP tools work unchanged
- All E2E tests pass unchanged
