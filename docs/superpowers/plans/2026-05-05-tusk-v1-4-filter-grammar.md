---
type: plan
title: Plan 4
status: shipped
pr: 355
shipped-at: "2026-05-05"
implements:
  - Plan 4 — Filter Grammar Spec
  - Tusk v1 Rebuild
---

# Tusk v1 — Plan 4: Filter Grammar

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a TaskWarrior-flavored structural filter grammar end-to-end — lexer, parser, AST, manifest-aware validator, monolithic SQL compiler, and a separate sort-spec parser — wired into a new `tusk query` command and a `tusk node list` rewrite.

**Architecture:** New `internal/filter/` package containing lexer (hand-rolled UTF-8 scanner with context-aware value lex), recursive-descent parser, AST, validator (against manifest edge types and traversal-shortcut prerequisites), and SQL compiler that emits parameterized SQL with JOINs (for edge predicates) and recursive CTEs (for `tree=`/`root=` shortcuts). Sort spec is a separate small parser. The CLI grows `tusk query` and updates `tusk node list` to drop `--type` in favor of the positional filter expression.

**Tech Stack:** Go 1.26 + the existing `internal/manifest`, `internal/index`, `internal/workspace`, `internal/lock` packages. No new external dependencies — Plan 4's grammar is a pure-Go implementation.

**Spec reference:** `docs/superpowers/specs/2026-05-05-tusk-v1-filter-grammar-design.md` (sub-spec). Master design at `docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md` §10.1, §13.1.

**Style rules:** Code respects `STYLE.md` — minimum 2-character identifiers (`*testing.T` → `test *testing.T`), blank lines around `err` guards, named errors on shadow.

---

## File Structure

**Created:**
```
internal/filter/
  token.go              # TokenKind + Token struct
  lexer.go              # Lexer with operator/keyword/identifier recognition + context-aware value lex
  lexer_test.go
  ast.go                # AST node types (Expr, OrExpr, AndExpr, NotExpr, PropertyPredicate, EdgePredicate, TraversalShortcut, Value variants)
  parser.go             # NewParser + recursive-descent parsing
  parser_test.go
  validate.go           # Validate(ast, manifest) → []ValidationError
  validate_test.go
  compile.go            # Compile(ast, sortKeys, take, skip) → (sql, params)
  compile_test.go
  sort.go               # ParseSort(spec) → []SortKey
  sort_test.go
  errors.go             # ParseError, ValidationError types

cmd/tusk/
  cmd_query.go          # tusk query <filter>
  cmd_query_test.go
  e2e_filter_test.go    # End-to-end coverage of the full pipeline
```

**Modified:**
```
cmd/tusk/cmd_node_list.go        # drop --type flag; accept positional filter; add --sort / --take / --skip
cmd/tusk/cmd_node_list_test.go   # rewrite tests to use positional filter
cmd/tusk/cmd_node_create_test.go # initWorkspace helper (no behavioral change)
cmd/tusk/root.go                 # register newQueryCmd
```

**Excluded for Plan 4** (deferred per spec §10):
- `+tag` / `-tag` shorthand → Plan 7 (tag pack territory).
- `--semantic` flag → Plan 5.
- Pattern matching (`title~="auth"`), date keywords (`due=today`).
- Configurable max-traversal-depth (hardcoded 5 in v1).

## Module Conventions for Plan 4

**Token positions:** every Token and AST node carries a `Pos int` (byte offset into the input string). The CLI renders errors with column + caret using `Pos`.

**Lexer contract:** the lexer exposes `Next() Token` for the operator/keyword stream and a separate `NextValue() Token` that the parser invokes when it expects a value (after an `=`/comparator). This avoids the IDENT-vs-BARE_VALUE ambiguity at lex time.

**Parser contract:** `NewParser(input string) *Parser; (*Parser).Parse() (Expr, []ParseError)`. Multiple syntax errors can accumulate before returning when the parser can usefully recover.

**Compiler contract:** `Compile(ast Expr, opts CompileOptions) (sql string, params []any, err error)` where `CompileOptions{ SortKeys []SortKey; Take int; Skip int }`. Take/Skip = 0 means "no LIMIT/OFFSET".

---

## Task 0: Pre-flight verification

**Files:** none (read-only)

- [ ] **Step 1: Confirm on `feat/plan-4` and clean tree**

```bash
git rev-parse --abbrev-ref HEAD
git status --short
git log --oneline -3
```

Expected: branch `feat/plan-4`; only the pre-existing devcontainer/gitignore unstaged changes (or empty); recent log starts with the spec commit (`docs(spec): filter grammar sub-spec for plan 4`).

- [ ] **Step 2: Confirm prior-plan tests still pass**

```bash
make test
make vet
```

Expected: 11 packages green, vet clean.

---

## Task 1: Token types and lexer skeleton (operators, keywords, identifiers)

**Files:**
- Create: `internal/filter/token.go`, `internal/filter/lexer.go`, `internal/filter/lexer_test.go`, `internal/filter/errors.go`

- [ ] **Step 1: Write the failing test — `internal/filter/lexer_test.go`**

```go
package filter_test

import (
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/filter"
)

func TestLexer_Operators(test *testing.T) {
	cases := []struct {
		input string
		kinds []filter.TokenKind
	}{
		{"=", []filter.TokenKind{filter.TokenEQ, filter.TokenEOF}},
		{"!=", []filter.TokenKind{filter.TokenNE, filter.TokenEOF}},
		{"< <= > >=", []filter.TokenKind{filter.TokenLT, filter.TokenLE, filter.TokenGT, filter.TokenGE, filter.TokenEOF}},
		{"-> <-", []filter.TokenKind{filter.TokenArrowOut, filter.TokenArrowIn, filter.TokenEOF}},
		{".. ()", []filter.TokenKind{filter.TokenDotDot, filter.TokenLParen, filter.TokenRParen, filter.TokenEOF}},
	}

	for _, tc := range cases {
		lexer := filter.NewLexer(tc.input)
		var actual []filter.TokenKind

		for {
			token := lexer.Next()
			actual = append(actual, token.Kind)

			if token.Kind == filter.TokenEOF {
				break
			}
		}

		if !reflect.DeepEqual(actual, tc.kinds) {
			test.Errorf("input %q: got %v, want %v", tc.input, actual, tc.kinds)
		}
	}
}

func TestLexer_Keywords(test *testing.T) {
	lexer := filter.NewLexer("AND or NOT and OR not")
	expected := []filter.TokenKind{
		filter.TokenAnd, filter.TokenOr, filter.TokenNot,
		filter.TokenAnd, filter.TokenOr, filter.TokenNot,
		filter.TokenEOF,
	}

	var actual []filter.TokenKind

	for {
		token := lexer.Next()
		actual = append(actual, token.Kind)

		if token.Kind == filter.TokenEOF {
			break
		}
	}

	if !reflect.DeepEqual(actual, expected) {
		test.Errorf("got %v, want %v", actual, expected)
	}
}

func TestLexer_Identifiers(test *testing.T) {
	lexer := filter.NewLexer("type status priority due-date my_field")
	expected := []string{"type", "status", "priority", "due-date", "my_field"}

	var actual []string

	for {
		token := lexer.Next()

		if token.Kind == filter.TokenEOF {
			break
		}

		if token.Kind != filter.TokenIdent {
			test.Fatalf("expected IDENT, got kind=%v value=%q", token.Kind, token.Value)
		}

		actual = append(actual, token.Value)
	}

	if !reflect.DeepEqual(actual, expected) {
		test.Errorf("got %v, want %v", actual, expected)
	}
}

func TestLexer_PositionTracking(test *testing.T) {
	lexer := filter.NewLexer("type=ticket")
	tokens := []filter.Token{}

	for {
		token := lexer.Next()
		tokens = append(tokens, token)

		if token.Kind == filter.TokenEOF {
			break
		}
	}

	if tokens[0].Pos != 0 || tokens[0].Kind != filter.TokenIdent {
		test.Errorf("token 0: got %+v, expected IDENT at 0", tokens[0])
	}

	if tokens[1].Pos != 4 || tokens[1].Kind != filter.TokenEQ {
		test.Errorf("token 1: got %+v, expected EQ at 4", tokens[1])
	}
}

func TestLexer_MaximalMunch(test *testing.T) {
	// "<=" must win over "<", "->" over "-", etc.
	cases := []struct {
		input string
		first filter.TokenKind
	}{
		{"<=foo", filter.TokenLE},
		{">=foo", filter.TokenGE},
		{"!=foo", filter.TokenNE},
		{"->foo", filter.TokenArrowOut},
		{"<-foo", filter.TokenArrowIn},
		{"..", filter.TokenDotDot},
	}

	for _, tc := range cases {
		lexer := filter.NewLexer(tc.input)
		token := lexer.Next()

		if token.Kind != tc.first {
			test.Errorf("input %q: got kind=%v, want %v", tc.input, token.Kind, tc.first)
		}
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/filter/...
```

Expected: FAIL — package doesn't exist.

- [ ] **Step 3: Implement `internal/filter/token.go`**

```go
// Package filter implements the structural filter grammar for tusk query
// and tusk node list. Pipeline: input string → Lexer → Parser → AST →
// Validator (against manifest) → Compiler → SQL.
package filter

// TokenKind classifies a lexer Token.
type TokenKind int

const (
	TokenEOF TokenKind = iota

	// Identifiers (property names, edge type names, traversal-shortcut keywords).
	TokenIdent

	// Quoted string value: "..." or '...'.
	TokenString

	// Bare value: emitted by Lexer.NextValue() when the parser expects a value.
	TokenBareValue

	// Operators.
	TokenEQ       // =
	TokenNE       // !=
	TokenLT       // <
	TokenLE       // <=
	TokenGT       // >
	TokenGE       // >=
	TokenArrowOut // ->
	TokenArrowIn  // <-
	TokenDotDot   // ..
	TokenLParen   // (
	TokenRParen   // )

	// Keywords (case-insensitive).
	TokenAnd
	TokenOr
	TokenNot
)

// Token is one unit produced by the lexer.
type Token struct {
	Kind  TokenKind
	Value string // identifier / string / bare-value text; empty for operators and keywords
	Pos   int    // byte offset into the original input
}
```

- [ ] **Step 4: Implement `internal/filter/errors.go`**

```go
package filter

import "fmt"

// ParseError reports a single syntactic problem with a position and a message.
type ParseError struct {
	Pos     int
	Message string
}

func (parseErr *ParseError) Error() string {
	return fmt.Sprintf("filter: %s at column %d", parseErr.Message, parseErr.Pos+1)
}
```

- [ ] **Step 5: Implement `internal/filter/lexer.go`**

```go
package filter

import "strings"

// Lexer tokenizes a filter expression. Use Next() for normal token flow and
// NextValue() when the parser expects a value after an operator.
type Lexer struct {
	input string
	pos   int
}

// NewLexer constructs a Lexer over input.
func NewLexer(input string) *Lexer {
	return &Lexer{input: input, pos: 0}
}

// Next returns the next operator/keyword/identifier token. Whitespace is skipped.
func (lex *Lexer) Next() Token {
	lex.skipWhitespace()

	if lex.pos >= len(lex.input) {
		return Token{Kind: TokenEOF, Pos: lex.pos}
	}

	startPos := lex.pos

	// Maximal-munch operators.
	if op := lex.tryOperator(); op != nil {
		return Token{Kind: op.kind, Pos: startPos}
	}

	// Identifier-shaped run.
	if isIdentStart(lex.input[lex.pos]) {
		end := lex.pos + 1

		for end < len(lex.input) && isIdentContinue(lex.input[end]) {
			end++
		}

		text := lex.input[lex.pos:end]
		lex.pos = end

		return Token{Kind: keywordOrIdent(text), Value: text, Pos: startPos}
	}

	// Strings.
	if lex.input[lex.pos] == '"' || lex.input[lex.pos] == '\'' {
		return lex.lexString()
	}

	// Unrecognized character — emit EOF; parser will surface a ParseError.
	return Token{Kind: TokenEOF, Pos: startPos}
}

// NextValue returns a value-position token: STRING (if the next char is a
// quote) or BARE_VALUE (a path/date/number-shaped run).
func (lex *Lexer) NextValue() Token {
	lex.skipWhitespace()

	if lex.pos >= len(lex.input) {
		return Token{Kind: TokenEOF, Pos: lex.pos}
	}

	startPos := lex.pos

	if lex.input[lex.pos] == '"' || lex.input[lex.pos] == '\'' {
		return lex.lexString()
	}

	end := lex.pos

	for end < len(lex.input) {
		// Stop at .. so range syntax tokenizes correctly.
		if end+1 < len(lex.input) && lex.input[end] == '.' && lex.input[end+1] == '.' {
			break
		}

		if !isBareValueChar(lex.input[end]) {
			break
		}

		end++
	}

	if end == lex.pos {
		return Token{Kind: TokenEOF, Pos: startPos}
	}

	text := lex.input[lex.pos:end]
	lex.pos = end

	return Token{Kind: TokenBareValue, Value: text, Pos: startPos}
}

// Pos reports the current byte offset.
func (lex *Lexer) Pos() int {
	return lex.pos
}

func (lex *Lexer) skipWhitespace() {
	for lex.pos < len(lex.input) {
		switch lex.input[lex.pos] {
		case ' ', '\t', '\n', '\r':
			lex.pos++
		default:
			return
		}
	}
}

type operatorMatch struct {
	literal string
	kind    TokenKind
}

var operators = []operatorMatch{
	{"!=", TokenNE},
	{"<=", TokenLE},
	{">=", TokenGE},
	{"->", TokenArrowOut},
	{"<-", TokenArrowIn},
	{"..", TokenDotDot},
	{"=", TokenEQ},
	{"<", TokenLT},
	{">", TokenGT},
	{"(", TokenLParen},
	{")", TokenRParen},
}

func (lex *Lexer) tryOperator() *operatorMatch {
	for _, op := range operators {
		if strings.HasPrefix(lex.input[lex.pos:], op.literal) {
			lex.pos += len(op.literal)

			return &op
		}
	}

	return nil
}

func (lex *Lexer) lexString() Token {
	startPos := lex.pos
	quote := lex.input[lex.pos]
	lex.pos++

	var builder strings.Builder

	for lex.pos < len(lex.input) {
		current := lex.input[lex.pos]

		if current == '\\' && lex.pos+1 < len(lex.input) {
			next := lex.input[lex.pos+1]

			switch next {
			case '\\', '"', '\'':
				builder.WriteByte(next)
				lex.pos += 2

				continue
			}

			builder.WriteByte(current)
			lex.pos++

			continue
		}

		if current == quote {
			lex.pos++

			return Token{Kind: TokenString, Value: builder.String(), Pos: startPos}
		}

		builder.WriteByte(current)
		lex.pos++
	}

	// Unterminated — emit EOF; parser surfaces a ParseError.
	return Token{Kind: TokenEOF, Pos: startPos}
}

func isIdentStart(character byte) bool {
	return (character >= 'a' && character <= 'z') ||
		(character >= 'A' && character <= 'Z') ||
		character == '_'
}

func isIdentContinue(character byte) bool {
	return isIdentStart(character) ||
		(character >= '0' && character <= '9') ||
		character == '-'
}

func isBareValueChar(character byte) bool {
	return isIdentContinue(character) ||
		character == '/' ||
		character == '.' ||
		character == ':'
}

func keywordOrIdent(text string) TokenKind {
	switch strings.ToUpper(text) {
	case "AND":
		return TokenAnd
	case "OR":
		return TokenOr
	case "NOT":
		return TokenNot
	}

	return TokenIdent
}
```

- [ ] **Step 6: Run, verify pass**

```bash
go test ./internal/filter/... -v
```

Expected: 5 PASS (the operator/keyword/identifier/position/maximal-munch tests).

- [ ] **Step 7: Commit**

```bash
git add internal/filter/token.go internal/filter/lexer.go internal/filter/errors.go internal/filter/lexer_test.go
git commit -m "feat(filter): lexer with operators, keywords, identifiers, positions"
```

---

## Task 2: Lexer — strings and bare values (context-aware value lex)

**Files:**
- Modify: `internal/filter/lexer_test.go`

- [ ] **Step 1: Append failing tests**

```go
func TestLexer_StringLiterals(test *testing.T) {
	cases := []struct {
		input string
		value string
	}{
		{`"hello"`, "hello"},
		{`'single quoted'`, "single quoted"},
		{`"with \"escape\""`, `with "escape"`},
		{`'mix\'d'`, `mix'd`},
		{`"\\backslash"`, `\backslash`},
	}

	for _, tc := range cases {
		lexer := filter.NewLexer(tc.input)
		token := lexer.Next()

		if token.Kind != filter.TokenString {
			test.Errorf("input %q: kind = %v, want STRING", tc.input, token.Kind)
		}

		if token.Value != tc.value {
			test.Errorf("input %q: value = %q, want %q", tc.input, token.Value, tc.value)
		}
	}
}

func TestLexer_BareValues(test *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"tickets/auth-epic", "tickets/auth-epic"},
		{"2026-05-15", "2026-05-15"},
		{"42", "42"},
		{"3.14", "3.14"},
	}

	for _, tc := range cases {
		lexer := filter.NewLexer(tc.input)
		token := lexer.NextValue()

		if token.Kind != filter.TokenBareValue {
			test.Errorf("input %q: kind = %v, want BARE_VALUE", tc.input, token.Kind)
		}

		if token.Value != tc.want {
			test.Errorf("input %q: value = %q, want %q", tc.input, token.Value, tc.want)
		}
	}
}

func TestLexer_BareValueStopsAtRange(test *testing.T) {
	// "2..4" should lex as BARE_VALUE("2"), DOTDOT, BARE_VALUE("4")
	lexer := filter.NewLexer("2..4")

	first := lexer.NextValue()

	if first.Kind != filter.TokenBareValue || first.Value != "2" {
		test.Errorf("first: got kind=%v value=%q, want BARE_VALUE(\"2\")", first.Kind, first.Value)
	}

	second := lexer.Next()

	if second.Kind != filter.TokenDotDot {
		test.Errorf("second: got kind=%v, want DOTDOT", second.Kind)
	}

	third := lexer.NextValue()

	if third.Kind != filter.TokenBareValue || third.Value != "4" {
		test.Errorf("third: got kind=%v value=%q, want BARE_VALUE(\"4\")", third.Kind, third.Value)
	}
}

func TestLexer_StringWithSpaces(test *testing.T) {
	lexer := filter.NewLexer(`"hello world"`)
	token := lexer.NextValue()

	if token.Kind != filter.TokenString || token.Value != "hello world" {
		test.Errorf("got kind=%v value=%q, want STRING(\"hello world\")", token.Kind, token.Value)
	}
}
```

- [ ] **Step 2: Run, verify pass**

The lexer code from Task 1 already handles strings and bare values; the new tests should pass against the existing implementation.

```bash
go test ./internal/filter/... -v
```

Expected: 9 PASS (5 from Task 1 + 4 new).

- [ ] **Step 3: Commit**

```bash
git add internal/filter/lexer_test.go
git commit -m "test(filter): lexer coverage for strings, bare values, range tokenization"
```

---

## Task 3: AST node types and value variants

**Files:**
- Create: `internal/filter/ast.go`

- [ ] **Step 1: Implement `internal/filter/ast.go`**

(No test for AST types directly — they're exercised by parser tests in Task 4. Acceptable because the file is purely declarative type definitions.)

```go
package filter

// Op is a comparison operator.
type Op int

const (
	OpEQ Op = iota
	OpNE
	OpLT
	OpLE
	OpGT
	OpGE
	OpRange // value is RangeValue{Min, Max}
)

func (op Op) String() string {
	switch op {
	case OpEQ:
		return "="
	case OpNE:
		return "!="
	case OpLT:
		return "<"
	case OpLE:
		return "<="
	case OpGT:
		return ">"
	case OpGE:
		return ">="
	case OpRange:
		return ".."
	}

	return "?"
}

// Direction is the polarity of an edge predicate.
type Direction int

const (
	DirectionOutgoing Direction = iota
	DirectionIncoming
)

// ShortcutKind identifies a graph-traversal shortcut.
type ShortcutKind int

const (
	ShortcutTree ShortcutKind = iota
	ShortcutParentOf
	ShortcutRoot
)

// Expr is the root AST node interface.
type Expr interface {
	exprNode()
	Position() int
}

// OrExpr — a OR b.
type OrExpr struct {
	Left  Expr
	Right Expr
	Pos   int
}

func (orExpr *OrExpr) exprNode()      {}
func (orExpr *OrExpr) Position() int  { return orExpr.Pos }

// AndExpr — a AND b.
type AndExpr struct {
	Left  Expr
	Right Expr
	Pos   int
}

func (andExpr *AndExpr) exprNode()      {}
func (andExpr *AndExpr) Position() int  { return andExpr.Pos }

// NotExpr — NOT inner.
type NotExpr struct {
	Inner Expr
	Pos   int
}

func (notExpr *NotExpr) exprNode()      {}
func (notExpr *NotExpr) Position() int  { return notExpr.Pos }

// PropertyPredicate — IDENT op value.
type PropertyPredicate struct {
	Property string
	Op       Op
	Value    Value
	Pos      int
}

func (pred *PropertyPredicate) exprNode()      {}
func (pred *PropertyPredicate) Position() int  { return pred.Pos }

// EdgePredicate — IDENT (-> | <-) (inner)?
type EdgePredicate struct {
	EdgeType  string
	Direction Direction
	Inner     Expr // nil = probe-only
	Pos       int
}

func (pred *EdgePredicate) exprNode()      {}
func (pred *EdgePredicate) Position() int  { return pred.Pos }

// TraversalShortcut — tree=X | parent=X | root=X.
type TraversalShortcut struct {
	Kind   ShortcutKind
	NodeID string
	Pos    int
}

func (shortcut *TraversalShortcut) exprNode()      {}
func (shortcut *TraversalShortcut) Position() int  { return shortcut.Pos }

// Value is a value AST.
type Value interface {
	valueNode()
}

// StringValue is a single literal value (from STRING or BARE_VALUE).
type StringValue struct {
	V string
}

func (stringValue StringValue) valueNode() {}

// RangeValue is a min..max pair.
type RangeValue struct {
	Min string
	Max string
}

func (rangeValue RangeValue) valueNode() {}
```

- [ ] **Step 2: Verify package compiles**

```bash
go build ./internal/filter/...
```

Expected: exits 0. (No tests added in this task; types are exercised by Task 4's parser tests.)

- [ ] **Step 3: Commit**

```bash
git add internal/filter/ast.go
git commit -m "feat(filter): AST node types for boolean composition, predicates, traversal shortcuts"
```

---

## Task 4: Parser — predicates only (property predicate, traversal shortcut, simple equality)

**Files:**
- Create: `internal/filter/parser.go`, `internal/filter/parser_test.go`

- [ ] **Step 1: Write the failing test**

```go
package filter_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/filter"
)

func TestParser_PropertyEquality(test *testing.T) {
	expr, errs := filter.NewParser("type=ticket").Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	pred, ok := expr.(*filter.PropertyPredicate)

	if !ok {
		test.Fatalf("got %T, want *PropertyPredicate", expr)
	}

	if pred.Property != "type" || pred.Op != filter.OpEQ {
		test.Errorf("got property=%q op=%v, want type=", pred.Property, pred.Op)
	}

	if str, isString := pred.Value.(filter.StringValue); !isString || str.V != "ticket" {
		test.Errorf("got value=%v, want StringValue{ticket}", pred.Value)
	}
}

func TestParser_PropertyComparators(test *testing.T) {
	cases := []struct {
		input string
		op    filter.Op
		value string
	}{
		{"priority>=3", filter.OpGE, "3"},
		{"priority<3", filter.OpLT, "3"},
		{"priority<=3", filter.OpLE, "3"},
		{"priority>3", filter.OpGT, "3"},
		{"priority!=3", filter.OpNE, "3"},
	}

	for _, tc := range cases {
		expr, errs := filter.NewParser(tc.input).Parse()

		if len(errs) > 0 {
			test.Fatalf("input %q: errors: %v", tc.input, errs)
		}

		pred := expr.(*filter.PropertyPredicate)

		if pred.Op != tc.op {
			test.Errorf("input %q: op=%v, want %v", tc.input, pred.Op, tc.op)
		}

		if str := pred.Value.(filter.StringValue).V; str != tc.value {
			test.Errorf("input %q: value=%q, want %q", tc.input, str, tc.value)
		}
	}
}

func TestParser_PropertyRange(test *testing.T) {
	expr, errs := filter.NewParser("priority=2..4").Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	pred := expr.(*filter.PropertyPredicate)

	if pred.Op != filter.OpRange {
		test.Errorf("op = %v, want OpRange", pred.Op)
	}

	rangeValue, ok := pred.Value.(filter.RangeValue)

	if !ok {
		test.Fatalf("value type %T, want RangeValue", pred.Value)
	}

	if rangeValue.Min != "2" || rangeValue.Max != "4" {
		test.Errorf("got range %v..%v, want 2..4", rangeValue.Min, rangeValue.Max)
	}
}

func TestParser_QuotedStringValue(test *testing.T) {
	expr, _ := filter.NewParser(`title="Auth bug"`).Parse()
	pred := expr.(*filter.PropertyPredicate)

	if pred.Value.(filter.StringValue).V != "Auth bug" {
		test.Errorf("got %q, want \"Auth bug\"", pred.Value.(filter.StringValue).V)
	}
}

func TestParser_TraversalShortcut(test *testing.T) {
	cases := []struct {
		input string
		kind  filter.ShortcutKind
		id    string
	}{
		{"tree=tickets/foo", filter.ShortcutTree, "tickets/foo"},
		{"parent=tickets/foo", filter.ShortcutParentOf, "tickets/foo"},
		{"root=tickets/foo", filter.ShortcutRoot, "tickets/foo"},
	}

	for _, tc := range cases {
		expr, errs := filter.NewParser(tc.input).Parse()

		if len(errs) > 0 {
			test.Fatalf("input %q: errors %v", tc.input, errs)
		}

		shortcut, ok := expr.(*filter.TraversalShortcut)

		if !ok {
			test.Fatalf("input %q: type %T, want *TraversalShortcut", tc.input, expr)
		}

		if shortcut.Kind != tc.kind || shortcut.NodeID != tc.id {
			test.Errorf("input %q: got %+v, want kind=%v id=%q", tc.input, shortcut, tc.kind, tc.id)
		}
	}
}

func TestParser_EmptyInputAcceptedAsTrue(test *testing.T) {
	// Empty filter == match-all; parser returns a sentinel TruePredicate.
	expr, errs := filter.NewParser("").Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	if expr != nil {
		// Sentinel = nil; compiler treats nil expression as WHERE TRUE.
		test.Errorf("expected nil expression for empty input, got %T", expr)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/filter/... -run TestParser_
```

Expected: FAIL — `filter.NewParser` undefined.

- [ ] **Step 3: Implement `internal/filter/parser.go`** (predicates portion only)

```go
package filter

import "fmt"

// Parser produces an Expr AST from an input string. Multiple syntax errors
// can accumulate when the parser can usefully recover; otherwise the first
// error stops parsing.
type Parser struct {
	lexer  *Lexer
	peeked *Token
	errs   []ParseError
}

// NewParser constructs a Parser over input.
func NewParser(input string) *Parser {
	return &Parser{lexer: NewLexer(input)}
}

// Parse consumes the input and returns the AST plus any parse errors. The
// caller treats a nil Expr with no errors as the match-all sentinel.
func (parser *Parser) Parse() (Expr, []ParseError) {
	parser.skipLeading()

	if parser.peek().Kind == TokenEOF {
		return nil, parser.errs
	}

	expr := parser.parseExpr()

	if parser.peek().Kind != TokenEOF {
		parser.appendErr(parser.peek().Pos, "unexpected trailing content")
	}

	return expr, parser.errs
}

// parseExpr is the entry point for boolean composition; Task 6 fills in OR/AND/NOT/parens.
// For Task 4 we only handle a single predicate.
func (parser *Parser) parseExpr() Expr {
	return parser.parsePredicate()
}

func (parser *Parser) parsePredicate() Expr {
	first := parser.peek()

	if first.Kind != TokenIdent {
		parser.appendErr(first.Pos, "expected identifier")
		parser.advance()

		return nil
	}

	switch first.Value {
	case "tree", "parent", "root":
		next := parser.peekN(1)

		if next.Kind == TokenEQ {
			return parser.parseTraversalShortcut()
		}
	}

	return parser.parsePropertyPredicate()
}

func (parser *Parser) parsePropertyPredicate() Expr {
	identToken := parser.advance()

	if identToken.Kind != TokenIdent {
		parser.appendErr(identToken.Pos, "expected property name")

		return nil
	}

	opToken := parser.advance()
	op, opOK := opTokenToOp(opToken.Kind)

	if !opOK {
		parser.appendErr(opToken.Pos, "expected comparison operator (= != < <= > >=)")

		return nil
	}

	leftValueToken := parser.lexer.NextValue()

	if leftValueToken.Kind == TokenEOF {
		parser.appendErr(leftValueToken.Pos, "expected value after operator")

		return nil
	}

	// Range syntax: <ident>=<value>..<value>
	if op == OpEQ && parser.peek().Kind == TokenDotDot {
		parser.advance() // consume DOTDOT
		rightValueToken := parser.lexer.NextValue()

		if rightValueToken.Kind == TokenEOF {
			parser.appendErr(rightValueToken.Pos, "expected value after ..")

			return nil
		}

		return &PropertyPredicate{
			Property: identToken.Value,
			Op:       OpRange,
			Value:    RangeValue{Min: leftValueToken.Value, Max: rightValueToken.Value},
			Pos:      identToken.Pos,
		}
	}

	return &PropertyPredicate{
		Property: identToken.Value,
		Op:       op,
		Value:    StringValue{V: leftValueToken.Value},
		Pos:      identToken.Pos,
	}
}

func (parser *Parser) parseTraversalShortcut() Expr {
	identToken := parser.advance()

	var kind ShortcutKind

	switch identToken.Value {
	case "tree":
		kind = ShortcutTree
	case "parent":
		kind = ShortcutParentOf
	case "root":
		kind = ShortcutRoot
	default:
		parser.appendErr(identToken.Pos, fmt.Sprintf("unknown traversal shortcut %q", identToken.Value))

		return nil
	}

	eqToken := parser.advance()

	if eqToken.Kind != TokenEQ {
		parser.appendErr(eqToken.Pos, "expected = after traversal-shortcut keyword")

		return nil
	}

	valueToken := parser.lexer.NextValue()

	if valueToken.Kind == TokenEOF {
		parser.appendErr(valueToken.Pos, "expected value after =")

		return nil
	}

	return &TraversalShortcut{Kind: kind, NodeID: valueToken.Value, Pos: identToken.Pos}
}

func opTokenToOp(kind TokenKind) (Op, bool) {
	switch kind {
	case TokenEQ:
		return OpEQ, true
	case TokenNE:
		return OpNE, true
	case TokenLT:
		return OpLT, true
	case TokenLE:
		return OpLE, true
	case TokenGT:
		return OpGT, true
	case TokenGE:
		return OpGE, true
	}

	return 0, false
}

// skipLeading consumes leading whitespace; the lexer already does this, but
// we centralize the call site for clarity.
func (parser *Parser) skipLeading() {
	parser.peek()
}

// peek returns the next token without consuming it. Internally we cache one
// token; advance() invalidates the cache.
func (parser *Parser) peek() Token {
	if parser.peeked == nil {
		token := parser.lexer.Next()
		parser.peeked = &token
	}

	return *parser.peeked
}

// peekN looks N tokens ahead without consuming. We use this only for the
// 1-token disambiguation between traversal shortcut and property predicate.
// For larger lookahead we'd need a token buffer; Plan 4 needs only N=1.
func (parser *Parser) peekN(distance int) Token {
	if distance == 0 {
		return parser.peek()
	}

	if distance != 1 {
		panic("filter: peekN(>1) not supported")
	}

	saved := parser.peeked
	parser.advance()
	next := parser.peek()
	parser.peeked = saved
	parser.lexer.pos -= len(next.Value)

	return next
}

func (parser *Parser) advance() Token {
	token := parser.peek()
	parser.peeked = nil

	return token
}

func (parser *Parser) appendErr(pos int, message string) {
	parser.errs = append(parser.errs, ParseError{Pos: pos, Message: message})
}
```

> **Implementer note**: the `peekN` implementation above is overly clever. The simpler design is a small token buffer (slice) used as a ring. Replace `peeked *Token` with `buffer []Token` and add a fill method that appends from the lexer when needed. The behavior is the same, but the code is more obvious.
>
> Concrete replacement:
>
> ```go
> type Parser struct {
>     lexer  *Lexer
>     buffer []Token
>     errs   []ParseError
> }
>
> func (parser *Parser) ensureBuffer(n int) {
>     for len(parser.buffer) < n {
>         parser.buffer = append(parser.buffer, parser.lexer.Next())
>     }
> }
>
> func (parser *Parser) peek() Token {
>     parser.ensureBuffer(1)
>     return parser.buffer[0]
> }
>
> func (parser *Parser) peekN(distance int) Token {
>     parser.ensureBuffer(distance + 1)
>     return parser.buffer[distance]
> }
>
> func (parser *Parser) advance() Token {
>     parser.ensureBuffer(1)
>     token := parser.buffer[0]
>     parser.buffer = parser.buffer[1:]
>     return token
> }
> ```
>
> Use this version. The `peeked *Token` and `lexer.pos -=` hack should not appear in the actual code.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/filter/... -run TestParser_ -v
```

Expected: 6 PASS (the parser tests).

- [ ] **Step 5: Commit**

```bash
git add internal/filter/parser.go internal/filter/parser_test.go
git commit -m "feat(filter): parser handles property predicates and traversal shortcuts"
```

---

## Task 5: Parser — edge predicates and multi-hop chains

**Files:**
- Modify: `internal/filter/parser.go`, `internal/filter/parser_test.go`

- [ ] **Step 1: Append failing tests**

```go
func TestParser_EdgeProbe(test *testing.T) {
	expr, errs := filter.NewParser("blocks->").Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	pred, ok := expr.(*filter.EdgePredicate)

	if !ok {
		test.Fatalf("got %T, want *EdgePredicate", expr)
	}

	if pred.EdgeType != "blocks" || pred.Direction != filter.DirectionOutgoing || pred.Inner != nil {
		test.Errorf("got %+v", pred)
	}
}

func TestParser_EdgeIncomingProbe(test *testing.T) {
	expr, _ := filter.NewParser("blocks<-").Parse()
	pred := expr.(*filter.EdgePredicate)

	if pred.Direction != filter.DirectionIncoming {
		test.Errorf("expected incoming")
	}
}

func TestParser_EdgePredicate(test *testing.T) {
	expr, errs := filter.NewParser("blocks->status=active").Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	outer := expr.(*filter.EdgePredicate)

	if outer.EdgeType != "blocks" || outer.Direction != filter.DirectionOutgoing {
		test.Errorf("outer = %+v", outer)
	}

	inner := outer.Inner.(*filter.PropertyPredicate)

	if inner.Property != "status" || inner.Value.(filter.StringValue).V != "active" {
		test.Errorf("inner = %+v", inner)
	}
}

func TestParser_MultiHopChain(test *testing.T) {
	expr, errs := filter.NewParser(`parent->parent->name="auth"`).Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	hop1 := expr.(*filter.EdgePredicate)
	hop2 := hop1.Inner.(*filter.EdgePredicate)
	leaf := hop2.Inner.(*filter.PropertyPredicate)

	if hop1.EdgeType != "parent" || hop2.EdgeType != "parent" || leaf.Property != "name" {
		test.Errorf("got hop1=%+v hop2=%+v leaf=%+v", hop1, hop2, leaf)
	}

	if leaf.Value.(filter.StringValue).V != "auth" {
		test.Errorf("leaf value = %q", leaf.Value.(filter.StringValue).V)
	}
}

func TestParser_MultiHopExceedsMaxDepth(test *testing.T) {
	// 6 hops > MaxTraversalDepth (5) — parser surfaces an error.
	input := "parent->parent->parent->parent->parent->parent->name=x"

	_, errs := filter.NewParser(input).Parse()

	if len(errs) == 0 {
		test.Fatalf("expected error for depth > 5")
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/filter/... -run TestParser_Edge -v
go test ./internal/filter/... -run TestParser_MultiHop -v
```

Expected: FAIL — edge predicate parsing not yet implemented.

- [ ] **Step 3: Extend `internal/filter/parser.go`**

Add a constant near the top of the file:

```go
// MaxTraversalDepth bounds the depth of multi-hop edge chains parsed by the
// recursive descent. Plan 4 hardcodes 5 (per the v1 spec); a configurable
// manifest field is a future polish.
const MaxTraversalDepth = 5
```

Update `parsePredicate` to handle edge predicates:

```go
func (parser *Parser) parsePredicate() Expr {
	first := parser.peek()

	if first.Kind != TokenIdent {
		parser.appendErr(first.Pos, "expected identifier")
		parser.advance()

		return nil
	}

	switch first.Value {
	case "tree", "parent", "root":
		next := parser.peekN(1)

		if next.Kind == TokenEQ {
			return parser.parseTraversalShortcut()
		}
	}

	// Edge predicate vs property predicate disambiguation: look ahead one token.
	next := parser.peekN(1)

	if next.Kind == TokenArrowOut || next.Kind == TokenArrowIn {
		return parser.parseEdgePredicate(0)
	}

	return parser.parsePropertyPredicate()
}

func (parser *Parser) parseEdgePredicate(depth int) Expr {
	if depth >= MaxTraversalDepth {
		token := parser.peek()
		parser.appendErr(token.Pos, fmt.Sprintf("multi-hop chain exceeds max depth %d", MaxTraversalDepth))

		return nil
	}

	identToken := parser.advance()

	if identToken.Kind != TokenIdent {
		parser.appendErr(identToken.Pos, "expected edge type identifier")

		return nil
	}

	arrowToken := parser.advance()

	var direction Direction

	switch arrowToken.Kind {
	case TokenArrowOut:
		direction = DirectionOutgoing
	case TokenArrowIn:
		direction = DirectionIncoming
	default:
		parser.appendErr(arrowToken.Pos, "expected -> or <- after edge type")

		return nil
	}

	pred := &EdgePredicate{
		EdgeType:  identToken.Value,
		Direction: direction,
		Pos:       identToken.Pos,
	}

	// Probe-only when the next token is a boolean operator, paren, EOF, or end of input.
	next := parser.peek()

	switch next.Kind {
	case TokenEOF, TokenAnd, TokenOr, TokenNot, TokenRParen:
		return pred
	}

	if next.Kind == TokenIdent {
		// Inner is either another edge predicate (multi-hop) or a property predicate.
		afterIdent := parser.peekN(1)

		if afterIdent.Kind == TokenArrowOut || afterIdent.Kind == TokenArrowIn {
			pred.Inner = parser.parseEdgePredicate(depth + 1)

			return pred
		}

		pred.Inner = parser.parsePropertyPredicate()

		return pred
	}

	parser.appendErr(next.Pos, "expected inner predicate or end of edge predicate")

	return pred
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/filter/... -v
```

Expected: all tests pass — Task 4's predicate tests + 5 new edge predicate tests.

- [ ] **Step 5: Commit**

```bash
git add internal/filter/parser.go internal/filter/parser_test.go
git commit -m "feat(filter): parser handles edge predicates and multi-hop chains with depth bound"
```

---

## Task 6: Parser — boolean composition (AND/OR/NOT/parens, implicit AND)

**Files:**
- Modify: `internal/filter/parser.go`, `internal/filter/parser_test.go`

- [ ] **Step 1: Append failing tests**

```go
func TestParser_ExplicitAnd(test *testing.T) {
	expr, errs := filter.NewParser("type=ticket AND status=active").Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	andExpr, ok := expr.(*filter.AndExpr)

	if !ok {
		test.Fatalf("got %T, want *AndExpr", expr)
	}

	left := andExpr.Left.(*filter.PropertyPredicate)
	right := andExpr.Right.(*filter.PropertyPredicate)

	if left.Property != "type" || right.Property != "status" {
		test.Errorf("got left=%+v right=%+v", left, right)
	}
}

func TestParser_ImplicitAnd(test *testing.T) {
	expr, errs := filter.NewParser("type=ticket status=active priority>=3").Parse()

	if len(errs) > 0 {
		test.Fatalf("errors: %v", errs)
	}

	// Left-associative: ((type=ticket AND status=active) AND priority>=3)
	outer := expr.(*filter.AndExpr)
	inner := outer.Left.(*filter.AndExpr)

	if inner.Left.(*filter.PropertyPredicate).Property != "type" {
		test.Errorf("inner.left = %+v", inner.Left)
	}

	if inner.Right.(*filter.PropertyPredicate).Property != "status" {
		test.Errorf("inner.right = %+v", inner.Right)
	}

	if outer.Right.(*filter.PropertyPredicate).Property != "priority" {
		test.Errorf("outer.right = %+v", outer.Right)
	}
}

func TestParser_Or(test *testing.T) {
	expr, _ := filter.NewParser("type=ticket OR type=note").Parse()
	orExpr := expr.(*filter.OrExpr)

	if orExpr.Left.(*filter.PropertyPredicate).Property != "type" {
		test.Errorf("left = %+v", orExpr.Left)
	}
}

func TestParser_Not(test *testing.T) {
	expr, _ := filter.NewParser("NOT status=completed").Parse()
	notExpr := expr.(*filter.NotExpr)

	if notExpr.Inner.(*filter.PropertyPredicate).Property != "status" {
		test.Errorf("inner = %+v", notExpr.Inner)
	}
}

func TestParser_Parens(test *testing.T) {
	// (a OR b) AND c — parens override default precedence (AND > OR).
	expr, _ := filter.NewParser("(type=ticket OR type=note) AND status=active").Parse()

	andExpr := expr.(*filter.AndExpr)
	_ = andExpr.Left.(*filter.OrExpr) // panics if parens didn't take effect
}

func TestParser_Precedence(test *testing.T) {
	// a AND b OR c == (a AND b) OR c
	expr, _ := filter.NewParser("type=ticket AND status=active OR type=note").Parse()
	orExpr := expr.(*filter.OrExpr)
	_ = orExpr.Left.(*filter.AndExpr)
	_ = orExpr.Right.(*filter.PropertyPredicate)
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/filter/... -run TestParser_(And|Or|Not|Parens|Precedence|Implicit) -v
```

Expected: FAIL.

- [ ] **Step 3: Extend `internal/filter/parser.go`**

Replace `parseExpr` and add the precedence levels:

```go
func (parser *Parser) parseExpr() Expr {
	return parser.parseOr()
}

func (parser *Parser) parseOr() Expr {
	left := parser.parseAnd()

	for parser.peek().Kind == TokenOr {
		opToken := parser.advance()
		right := parser.parseAnd()
		left = &OrExpr{Left: left, Right: right, Pos: opToken.Pos}
	}

	return left
}

func (parser *Parser) parseAnd() Expr {
	left := parser.parseNot()

	for {
		next := parser.peek()

		if next.Kind == TokenAnd {
			opToken := parser.advance()
			right := parser.parseNot()
			left = &AndExpr{Left: left, Right: right, Pos: opToken.Pos}

			continue
		}

		// Implicit AND: another atom-starting token (IDENT or LPAREN or NOT).
		if next.Kind == TokenIdent || next.Kind == TokenLParen || next.Kind == TokenNot {
			right := parser.parseNot()
			left = &AndExpr{Left: left, Right: right, Pos: next.Pos}

			continue
		}

		break
	}

	return left
}

func (parser *Parser) parseNot() Expr {
	if parser.peek().Kind == TokenNot {
		notToken := parser.advance()

		return &NotExpr{Inner: parser.parseNot(), Pos: notToken.Pos}
	}

	return parser.parseAtom()
}

func (parser *Parser) parseAtom() Expr {
	if parser.peek().Kind == TokenLParen {
		parser.advance()
		inner := parser.parseExpr()

		if parser.peek().Kind != TokenRParen {
			parser.appendErr(parser.peek().Pos, "expected )")
		} else {
			parser.advance()
		}

		return inner
	}

	return parser.parsePredicate()
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/filter/... -v
```

Expected: all parser tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/filter/parser.go internal/filter/parser_test.go
git commit -m "feat(filter): parser handles AND/OR/NOT/parens with implicit AND"
```

---

## Task 7: Manifest-aware validator

**Files:**
- Create: `internal/filter/validate.go`, `internal/filter/validate_test.go`

- [ ] **Step 1: Write failing tests**

```go
package filter_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/manifest"
)

func TestValidate_AcceptsKnownEdgeType(test *testing.T) {
	expr, _ := filter.NewParser("blocks->").Parse()

	manifestObj := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"blocks": {From: []string{"*"}, To: []string{"*"}, Cardinality: manifest.CardinalityManyToMany},
		},
	}

	errs := filter.Validate(expr, manifestObj)

	if len(errs) > 0 {
		test.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_RejectsUnknownEdgeType(test *testing.T) {
	expr, _ := filter.NewParser("unknown->").Parse()

	errs := filter.Validate(expr, manifest.Manifest{EdgeTypes: map[string]manifest.EdgeType{}})

	if len(errs) == 0 {
		test.Fatalf("expected error for unknown edge type")
	}

	if !strings.Contains(errs[0].Message, "unknown") && !strings.Contains(errs[0].Message, "not declared") {
		test.Errorf("error message should mention unknown/not declared: %v", errs[0])
	}
}

func TestValidate_TraversalShortcutRequiresParentEdge(test *testing.T) {
	expr, _ := filter.NewParser("tree=tickets/foo").Parse()

	errs := filter.Validate(expr, manifest.Manifest{EdgeTypes: map[string]manifest.EdgeType{}})

	if len(errs) == 0 {
		test.Fatalf("expected error: traversal shortcut requires `parent` edge type")
	}
}

func TestValidate_TraversalShortcutOKWithParentEdge(test *testing.T) {
	expr, _ := filter.NewParser("tree=tickets/foo").Parse()

	manifestObj := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"parent": {From: []string{"*"}, To: []string{"*"}, Cardinality: manifest.CardinalityManyToOne},
		},
	}

	errs := filter.Validate(expr, manifestObj)

	if len(errs) > 0 {
		test.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_NestedEdgeChainAllValidate(test *testing.T) {
	expr, _ := filter.NewParser("parent->parent->name=auth").Parse()

	manifestObj := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"parent": {From: []string{"*"}, To: []string{"*"}, Cardinality: manifest.CardinalityManyToOne},
		},
	}

	errs := filter.Validate(expr, manifestObj)

	if len(errs) > 0 {
		test.Errorf("expected no errors, got %v", errs)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/filter/... -run TestValidate_
```

Expected: FAIL — `filter.Validate` undefined.

- [ ] **Step 3: Implement `internal/filter/validate.go`**

```go
package filter

import (
	"fmt"

	"github.com/germanamz/tusk/internal/manifest"
)

// ValidationError reports a validation problem with a position and a hint.
type ValidationError struct {
	Pos     int
	Message string
	Hint    string
}

func (validationErr *ValidationError) Error() string {
	if validationErr.Hint != "" {
		return fmt.Sprintf("filter: %s at column %d (%s)", validationErr.Message, validationErr.Pos+1, validationErr.Hint)
	}

	return fmt.Sprintf("filter: %s at column %d", validationErr.Message, validationErr.Pos+1)
}

// Validate walks the AST and surfaces semantic problems against the manifest.
func Validate(expr Expr, loaded manifest.Manifest) []ValidationError {
	if expr == nil {
		return nil
	}

	collector := &validationCollector{manifest: loaded}
	collector.walk(expr)

	return collector.errors
}

type validationCollector struct {
	manifest manifest.Manifest
	errors   []ValidationError
}

func (collector *validationCollector) walk(expr Expr) {
	switch typed := expr.(type) {
	case *OrExpr:
		collector.walk(typed.Left)
		collector.walk(typed.Right)
	case *AndExpr:
		collector.walk(typed.Left)
		collector.walk(typed.Right)
	case *NotExpr:
		collector.walk(typed.Inner)
	case *PropertyPredicate:
		// Properties are not validated against the manifest in Plan 4 (see spec §5.2).
	case *EdgePredicate:
		if _, declared := collector.manifest.EdgeTypes[typed.EdgeType]; !declared {
			collector.errors = append(collector.errors, ValidationError{
				Pos:     typed.Pos,
				Message: fmt.Sprintf("edge type %q not declared in manifest", typed.EdgeType),
				Hint:    suggestEdgeType(typed.EdgeType, collector.manifest.EdgeTypes),
			})
		}

		if typed.Inner != nil {
			collector.walk(typed.Inner)
		}
	case *TraversalShortcut:
		if _, declared := collector.manifest.EdgeTypes["parent"]; !declared {
			collector.errors = append(collector.errors, ValidationError{
				Pos:     typed.Pos,
				Message: "traversal shortcut requires the workspace to declare a `parent` edge type",
				Hint:    "add [edge-types.parent] to tusk.toml or use explicit `<edge>->` form",
			})
		}
	}
}

// suggestEdgeType returns a Levenshtein-1 suggestion if available, else "".
func suggestEdgeType(unknown string, available map[string]manifest.EdgeType) string {
	for name := range available {
		if levenshteinAtMostOne(unknown, name) {
			return fmt.Sprintf("did you mean %q?", name)
		}
	}

	return ""
}

func levenshteinAtMostOne(left, right string) bool {
	if len(left) == len(right) {
		differences := 0

		for i := 0; i < len(left); i++ {
			if left[i] != right[i] {
				differences++

				if differences > 1 {
					return false
				}
			}
		}

		return differences == 1
	}

	if abs(len(left)-len(right)) != 1 {
		return false
	}

	shorter, longer := left, right

	if len(left) > len(right) {
		shorter, longer = right, left
	}

	shortIdx := 0
	longIdx := 0
	differences := 0

	for shortIdx < len(shorter) && longIdx < len(longer) {
		if shorter[shortIdx] == longer[longIdx] {
			shortIdx++
			longIdx++

			continue
		}

		differences++

		if differences > 1 {
			return false
		}

		longIdx++
	}

	return true
}

func abs(value int) int {
	if value < 0 {
		return -value
	}

	return value
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/filter/... -v
```

Expected: 5 new validate tests pass; all earlier tests still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/filter/validate.go internal/filter/validate_test.go
git commit -m "feat(filter): manifest-aware validator with Levenshtein-1 hints"
```

---

## Task 8: Sort spec parser

**Files:**
- Create: `internal/filter/sort.go`, `internal/filter/sort_test.go`

- [ ] **Step 1: Write failing tests**

```go
package filter_test

import (
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/filter"
)

func TestParseSort_Basic(test *testing.T) {
	cases := []struct {
		input string
		want  []filter.SortKey
	}{
		{"+priority", []filter.SortKey{{Property: "priority", Descending: false}}},
		{"-priority", []filter.SortKey{{Property: "priority", Descending: true}}},
		{"priority", []filter.SortKey{{Property: "priority", Descending: false}}}, // no prefix = ascending
		{"+priority,-due", []filter.SortKey{
			{Property: "priority", Descending: false},
			{Property: "due", Descending: true},
		}},
		{"+priority, -due, +modified", []filter.SortKey{
			{Property: "priority", Descending: false},
			{Property: "due", Descending: true},
			{Property: "modified", Descending: false},
		}},
	}

	for _, tc := range cases {
		got, err := filter.ParseSort(tc.input)

		if err != nil {
			test.Errorf("input %q: error %v", tc.input, err)

			continue
		}

		if !reflect.DeepEqual(got, tc.want) {
			test.Errorf("input %q: got %+v, want %+v", tc.input, got, tc.want)
		}
	}
}

func TestParseSort_EmptyReturnsEmptySlice(test *testing.T) {
	got, err := filter.ParseSort("")

	if err != nil {
		test.Errorf("error: %v", err)
	}

	if len(got) != 0 {
		test.Errorf("got %+v, want empty", got)
	}
}

func TestParseSort_RejectsEmptyKey(test *testing.T) {
	_, err := filter.ParseSort("+priority,,-due")

	if err == nil {
		test.Errorf("expected error for empty key")
	}
}

func TestParseSort_RejectsBareSign(test *testing.T) {
	_, err := filter.ParseSort("+,-due")

	if err == nil {
		test.Errorf("expected error for bare sign")
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/filter/... -run TestParseSort_
```

Expected: FAIL.

- [ ] **Step 3: Implement `internal/filter/sort.go`**

```go
package filter

import (
	"fmt"
	"strings"
)

// SortKey is one ORDER BY column produced from a --sort spec.
type SortKey struct {
	Property   string
	Descending bool
}

// ParseSort parses a --sort spec like "+priority,-due,+modified".
// Each key may be prefixed with + (ascending; default) or - (descending).
// Whitespace around commas is tolerated; an empty input returns an empty slice.
func ParseSort(spec string) ([]SortKey, error) {
	trimmedInput := strings.TrimSpace(spec)

	if trimmedInput == "" {
		return nil, nil
	}

	rawKeys := strings.Split(trimmedInput, ",")
	keys := make([]SortKey, 0, len(rawKeys))

	for index, raw := range rawKeys {
		trimmed := strings.TrimSpace(raw)

		if trimmed == "" {
			return nil, fmt.Errorf("sort: empty key at position %d", index)
		}

		key := SortKey{}

		switch trimmed[0] {
		case '+':
			key.Descending = false
			trimmed = trimmed[1:]
		case '-':
			key.Descending = true
			trimmed = trimmed[1:]
		default:
			key.Descending = false
		}

		if trimmed == "" {
			return nil, fmt.Errorf("sort: bare sign at position %d (expected property name after + or -)", index)
		}

		key.Property = trimmed
		keys = append(keys, key)
	}

	return keys, nil
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/filter/... -run TestParseSort_ -v
```

Expected: 4 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/filter/sort.go internal/filter/sort_test.go
git commit -m "feat(filter): sort spec parser with +/- direction prefixes"
```

---

## Task 9: SQL compiler — property predicates, booleans, sort, LIMIT/OFFSET

**Files:**
- Create: `internal/filter/compile.go`, `internal/filter/compile_test.go`

- [ ] **Step 1: Write failing tests — property predicates and booleans**

```go
package filter_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/filter"
)

func TestCompile_CorePropertyEquality(test *testing.T) {
	expr, _ := filter.NewParser("type=ticket").Parse()

	sql, params, err := filter.Compile(expr, filter.CompileOptions{})

	if err != nil {
		test.Fatalf("Compile: %v", err)
	}

	if !strings.Contains(sql, "type = ?") {
		test.Errorf("sql missing core column comparison: %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"ticket"}) {
		test.Errorf("params = %v, want [ticket]", params)
	}
}

func TestCompile_NonCorePropertyUsesJSONExtract(test *testing.T) {
	expr, _ := filter.NewParser("priority=3").Parse()

	sql, params, _ := filter.Compile(expr, filter.CompileOptions{})

	if !strings.Contains(sql, `json_extract(properties_json, '$.priority')`) {
		test.Errorf("sql missing json_extract: %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"3"}) {
		test.Errorf("params = %v, want [3]", params)
	}
}

func TestCompile_NumericComparatorCasts(test *testing.T) {
	expr, _ := filter.NewParser("priority>=3").Parse()

	sql, _, _ := filter.Compile(expr, filter.CompileOptions{})

	if !strings.Contains(sql, "CAST(json_extract") {
		test.Errorf("sql missing CAST for numeric comparator: %s", sql)
	}

	if !strings.Contains(sql, ">= ?") {
		test.Errorf("sql missing >= operator: %s", sql)
	}
}

func TestCompile_RangeProducesBetween(test *testing.T) {
	expr, _ := filter.NewParser("priority=2..4").Parse()

	sql, params, _ := filter.Compile(expr, filter.CompileOptions{})

	if !strings.Contains(sql, "BETWEEN ? AND ?") {
		test.Errorf("sql missing BETWEEN: %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"2", "4"}) {
		test.Errorf("params = %v, want [2 4]", params)
	}
}

func TestCompile_BooleanComposition(test *testing.T) {
	expr, _ := filter.NewParser("type=ticket AND status=active").Parse()

	sql, _, _ := filter.Compile(expr, filter.CompileOptions{})

	if !strings.Contains(sql, " AND ") {
		test.Errorf("sql missing AND: %s", sql)
	}
}

func TestCompile_NotWraps(test *testing.T) {
	expr, _ := filter.NewParser("NOT status=completed").Parse()

	sql, _, _ := filter.Compile(expr, filter.CompileOptions{})

	if !strings.Contains(sql, "NOT (") {
		test.Errorf("sql missing NOT wrap: %s", sql)
	}
}

func TestCompile_NilExprMatchesAll(test *testing.T) {
	sql, params, _ := filter.Compile(nil, filter.CompileOptions{})

	if !strings.Contains(sql, "WHERE 1 = 1") {
		test.Errorf("sql for nil expr should match all: %s", sql)
	}

	if len(params) != 0 {
		test.Errorf("params = %v, want empty", params)
	}
}

func TestCompile_SortKeysAppendOrderBy(test *testing.T) {
	expr, _ := filter.NewParser("type=ticket").Parse()

	sql, _, _ := filter.Compile(expr, filter.CompileOptions{
		SortKeys: []filter.SortKey{
			{Property: "priority", Descending: true},
			{Property: "title", Descending: false},
		},
	})

	if !strings.Contains(sql, "ORDER BY") {
		test.Errorf("sql missing ORDER BY: %s", sql)
	}

	if !strings.Contains(sql, "DESC") {
		test.Errorf("sql missing DESC: %s", sql)
	}
}

func TestCompile_TakeAndSkip(test *testing.T) {
	expr, _ := filter.NewParser("type=ticket").Parse()

	sql, _, _ := filter.Compile(expr, filter.CompileOptions{Take: 25, Skip: 50})

	if !strings.Contains(sql, "LIMIT 25") || !strings.Contains(sql, "OFFSET 50") {
		test.Errorf("sql missing LIMIT/OFFSET: %s", sql)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/filter/... -run TestCompile_
```

Expected: FAIL.

- [ ] **Step 3: Implement `internal/filter/compile.go`** (Task 9 portion — booleans, properties, sort, LIMIT/OFFSET; edge predicates and traversal shortcuts come in Tasks 10–11)

```go
package filter

import (
	"fmt"
	"strings"
)

// CompileOptions configures Compile.
type CompileOptions struct {
	SortKeys []SortKey
	Take     int // LIMIT N when > 0
	Skip     int // OFFSET M when > 0; requires Take > 0
}

// Compile turns an AST + options into parameterized SQL against the nodes table.
//
// Layout: SELECT <cols> FROM nodes WHERE <where> ORDER BY <sort> LIMIT <take> OFFSET <skip>.
func Compile(expr Expr, opts CompileOptions) (string, []any, error) {
	if opts.Skip > 0 && opts.Take == 0 {
		return "", nil, fmt.Errorf("compile: --skip requires --take")
	}

	whereClause, params, whereErr := compileWhere(expr)

	if whereErr != nil {
		return "", nil, whereErr
	}

	var builder strings.Builder

	builder.WriteString(`SELECT id, type, path, title, properties_json, last_mtime, last_size, last_checksum FROM nodes WHERE `)
	builder.WriteString(whereClause)

	if len(opts.SortKeys) > 0 {
		builder.WriteString(" ORDER BY ")
		builder.WriteString(compileOrderBy(opts.SortKeys))
	}

	if opts.Take > 0 {
		fmt.Fprintf(&builder, " LIMIT %d", opts.Take)

		if opts.Skip > 0 {
			fmt.Fprintf(&builder, " OFFSET %d", opts.Skip)
		}
	}

	return builder.String(), params, nil
}

func compileWhere(expr Expr) (string, []any, error) {
	if expr == nil {
		return "1 = 1", nil, nil
	}

	switch typed := expr.(type) {
	case *OrExpr:
		left, leftParams, leftErr := compileWhere(typed.Left)

		if leftErr != nil {
			return "", nil, leftErr
		}

		right, rightParams, rightErr := compileWhere(typed.Right)

		if rightErr != nil {
			return "", nil, rightErr
		}

		return "(" + left + ") OR (" + right + ")", append(leftParams, rightParams...), nil
	case *AndExpr:
		left, leftParams, leftErr := compileWhere(typed.Left)

		if leftErr != nil {
			return "", nil, leftErr
		}

		right, rightParams, rightErr := compileWhere(typed.Right)

		if rightErr != nil {
			return "", nil, rightErr
		}

		return "(" + left + ") AND (" + right + ")", append(leftParams, rightParams...), nil
	case *NotExpr:
		inner, innerParams, innerErr := compileWhere(typed.Inner)

		if innerErr != nil {
			return "", nil, innerErr
		}

		return "NOT (" + inner + ")", innerParams, nil
	case *PropertyPredicate:
		return compileProperty(typed)
	case *EdgePredicate:
		return "", nil, fmt.Errorf("compile: edge predicates land in task 10")
	case *TraversalShortcut:
		return "", nil, fmt.Errorf("compile: traversal shortcuts land in task 11")
	}

	return "", nil, fmt.Errorf("compile: unknown AST node type %T", expr)
}

var coreColumns = map[string]struct{}{
	"id":    {},
	"type":  {},
	"path":  {},
	"title": {},
}

func compileProperty(predicate *PropertyPredicate) (string, []any, error) {
	column, isCoreColumn := propertyColumn(predicate.Property)

	if predicate.Op == OpRange {
		rangeValue, ok := predicate.Value.(RangeValue)

		if !ok {
			return "", nil, fmt.Errorf("compile: OpRange with non-RangeValue")
		}

		if isCoreColumn {
			return column + " BETWEEN ? AND ?", []any{rangeValue.Min, rangeValue.Max}, nil
		}

		// JSON extract for non-core; numeric ranges cast for proper ordering.
		extract := fmt.Sprintf(`CAST(json_extract(properties_json, '$.%s') AS INTEGER)`, predicate.Property)

		return extract + " BETWEEN ? AND ?", []any{rangeValue.Min, rangeValue.Max}, nil
	}

	stringValue, ok := predicate.Value.(StringValue)

	if !ok {
		return "", nil, fmt.Errorf("compile: PropertyPredicate.Value is not StringValue")
	}

	sqlOp := opToSQL(predicate.Op)

	if isCoreColumn {
		return column + " " + sqlOp + " ?", []any{stringValue.V}, nil
	}

	if isNumericOp(predicate.Op) {
		return fmt.Sprintf(`CAST(json_extract(properties_json, '$.%s') AS INTEGER) %s ?`, predicate.Property, sqlOp), []any{stringValue.V}, nil
	}

	return fmt.Sprintf(`json_extract(properties_json, '$.%s') %s ?`, predicate.Property, sqlOp), []any{stringValue.V}, nil
}

func propertyColumn(name string) (string, bool) {
	if _, isCore := coreColumns[name]; isCore {
		return name, true
	}

	return "", false
}

func opToSQL(op Op) string {
	switch op {
	case OpEQ:
		return "="
	case OpNE:
		return "!="
	case OpLT:
		return "<"
	case OpLE:
		return "<="
	case OpGT:
		return ">"
	case OpGE:
		return ">="
	}

	return "?"
}

func isNumericOp(op Op) bool {
	switch op {
	case OpLT, OpLE, OpGT, OpGE:
		return true
	}

	return false
}

func compileOrderBy(keys []SortKey) string {
	parts := make([]string, 0, len(keys))

	for _, key := range keys {
		column, isCore := propertyColumn(key.Property)
		expression := column

		if !isCore {
			expression = fmt.Sprintf(`json_extract(properties_json, '$.%s')`, key.Property)
		}

		direction := "ASC"

		if key.Descending {
			direction = "DESC"
		}

		parts = append(parts, expression+" "+direction)
	}

	return strings.Join(parts, ", ")
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/filter/... -run TestCompile_ -v
```

Expected: 9 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/filter/compile.go internal/filter/compile_test.go
git commit -m "feat(filter): SQL compiler for property predicates, booleans, sort, LIMIT/OFFSET"
```

---

## Task 10: SQL compiler — edge predicates and multi-hop chains

**Files:**
- Modify: `internal/filter/compile.go`, `internal/filter/compile_test.go`

- [ ] **Step 1: Append failing tests**

```go
func TestCompile_EdgeProbe(test *testing.T) {
	expr, _ := filter.NewParser("blocks->").Parse()

	sql, params, _ := filter.Compile(expr, filter.CompileOptions{})

	if !strings.Contains(sql, "EXISTS") || !strings.Contains(sql, "edges") {
		test.Errorf("expected EXISTS over edges: %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"blocks"}) {
		test.Errorf("params = %v, want [blocks]", params)
	}
}

func TestCompile_EdgeIncomingProbe(test *testing.T) {
	expr, _ := filter.NewParser("blocks<-").Parse()

	sql, _, _ := filter.Compile(expr, filter.CompileOptions{})

	if !strings.Contains(sql, "target_id = nodes.id") {
		test.Errorf("expected target_id = nodes.id for incoming probe: %s", sql)
	}
}

func TestCompile_EdgePredicate(test *testing.T) {
	expr, _ := filter.NewParser("blocks->status=active").Parse()

	sql, params, _ := filter.Compile(expr, filter.CompileOptions{})

	if !strings.Contains(sql, "JOIN nodes") {
		test.Errorf("expected JOIN nodes for edge predicate: %s", sql)
	}

	wantParams := []any{"blocks", "active"}
	if !reflect.DeepEqual(params, wantParams) {
		test.Errorf("params = %v, want %v", params, wantParams)
	}
}

func TestCompile_MultiHopChain(test *testing.T) {
	expr, _ := filter.NewParser(`parent->parent->name="auth"`).Parse()

	sql, params, _ := filter.Compile(expr, filter.CompileOptions{})

	// Two nested EXISTS, two JOINs.
	if strings.Count(sql, "EXISTS") < 2 {
		test.Errorf("expected ≥2 EXISTS for 2-hop chain: %s", sql)
	}

	wantParams := []any{"parent", "parent", "auth"}
	if !reflect.DeepEqual(params, wantParams) {
		test.Errorf("params = %v, want %v", params, wantParams)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/filter/... -run TestCompile_Edge -v
go test ./internal/filter/... -run TestCompile_MultiHop -v
```

Expected: FAIL.

- [ ] **Step 3: Replace the `*EdgePredicate` case in `compileWhere`**

```go
	case *EdgePredicate:
		return compileEdgePredicate(typed, 0)
```

Add the helper:

```go
// compileEdgePredicate emits an EXISTS subquery over the edges table. depth is
// used to mint unique aliases when chains nest.
func compileEdgePredicate(predicate *EdgePredicate, depth int) (string, []any, error) {
	edgeAlias := fmt.Sprintf("e%d", depth)
	nodeAlias := fmt.Sprintf("n%d", depth)
	parentRef := "nodes"

	if depth > 0 {
		parentRef = fmt.Sprintf("n%d", depth-1)
	}

	var sourceColumn, joinColumn string

	if predicate.Direction == DirectionOutgoing {
		sourceColumn = fmt.Sprintf("%s.source_id = %s.id", edgeAlias, parentRef)
		joinColumn = fmt.Sprintf("%s.id = %s.target_id", nodeAlias, edgeAlias)
	} else {
		sourceColumn = fmt.Sprintf("%s.target_id = %s.id", edgeAlias, parentRef)
		joinColumn = fmt.Sprintf("%s.id = %s.source_id", nodeAlias, edgeAlias)
	}

	if predicate.Inner == nil {
		// Probe-only — no JOIN needed.
		sql := fmt.Sprintf("EXISTS (SELECT 1 FROM edges %s WHERE %s AND %s.type = ?)", edgeAlias, sourceColumn, edgeAlias)

		return sql, []any{predicate.EdgeType}, nil
	}

	// Inner is a property predicate or another edge predicate. Compile inner
	// against the joined-in node alias.
	innerSQL, innerParams, innerErr := compileInnerOnAlias(predicate.Inner, nodeAlias, depth)

	if innerErr != nil {
		return "", nil, innerErr
	}

	sql := fmt.Sprintf("EXISTS (SELECT 1 FROM edges %s JOIN nodes %s ON %s WHERE %s AND %s.type = ? AND %s)",
		edgeAlias, nodeAlias, joinColumn, sourceColumn, edgeAlias, innerSQL)

	params := append([]any{predicate.EdgeType}, innerParams...)

	return sql, params, nil
}

// compileInnerOnAlias compiles an inner expression, rewriting the property /
// JSON-extract column references to use the supplied node alias.
func compileInnerOnAlias(inner Expr, alias string, depth int) (string, []any, error) {
	switch typed := inner.(type) {
	case *PropertyPredicate:
		return compilePropertyOnAlias(typed, alias)
	case *EdgePredicate:
		return compileEdgePredicate(typed, depth+1)
	}

	return "", nil, fmt.Errorf("compile: unsupported inner predicate type %T", inner)
}

func compilePropertyOnAlias(predicate *PropertyPredicate, alias string) (string, []any, error) {
	if predicate.Op == OpRange {
		rangeValue := predicate.Value.(RangeValue)

		if _, isCore := coreColumns[predicate.Property]; isCore {
			return fmt.Sprintf("%s.%s BETWEEN ? AND ?", alias, predicate.Property), []any{rangeValue.Min, rangeValue.Max}, nil
		}

		return fmt.Sprintf(`CAST(json_extract(%s.properties_json, '$.%s') AS INTEGER) BETWEEN ? AND ?`, alias, predicate.Property), []any{rangeValue.Min, rangeValue.Max}, nil
	}

	stringValue := predicate.Value.(StringValue)
	sqlOp := opToSQL(predicate.Op)

	if _, isCore := coreColumns[predicate.Property]; isCore {
		return fmt.Sprintf("%s.%s %s ?", alias, predicate.Property, sqlOp), []any{stringValue.V}, nil
	}

	if isNumericOp(predicate.Op) {
		return fmt.Sprintf(`CAST(json_extract(%s.properties_json, '$.%s') AS INTEGER) %s ?`, alias, predicate.Property, sqlOp), []any{stringValue.V}, nil
	}

	return fmt.Sprintf(`json_extract(%s.properties_json, '$.%s') %s ?`, alias, predicate.Property, sqlOp), []any{stringValue.V}, nil
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/filter/... -v
```

Expected: all compile tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/filter/compile.go internal/filter/compile_test.go
git commit -m "feat(filter): SQL compilation for edge probes, predicates, and multi-hop chains"
```

---

## Task 11: SQL compiler — traversal shortcuts (recursive CTEs)

**Files:**
- Modify: `internal/filter/compile.go`, `internal/filter/compile_test.go`

- [ ] **Step 1: Append failing tests**

```go
func TestCompile_TraversalShortcutParent(test *testing.T) {
	expr, _ := filter.NewParser("parent=tickets/foo").Parse()

	sql, params, _ := filter.Compile(expr, filter.CompileOptions{})

	if !strings.Contains(sql, "EXISTS") || !strings.Contains(sql, "type = 'parent'") {
		test.Errorf("sql for parent= shortcut missing EXISTS or 'parent' type: %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"tickets/foo"}) {
		test.Errorf("params = %v, want [tickets/foo]", params)
	}
}

func TestCompile_TraversalShortcutTreeUsesRecursiveCTE(test *testing.T) {
	expr, _ := filter.NewParser("tree=tickets/foo").Parse()

	sql, params, _ := filter.Compile(expr, filter.CompileOptions{})

	if !strings.Contains(sql, "WITH RECURSIVE") {
		test.Errorf("expected recursive CTE for tree=: %s", sql)
	}

	if !strings.Contains(sql, "depth < 5") {
		test.Errorf("expected depth bound of 5: %s", sql)
	}

	if !reflect.DeepEqual(params, []any{"tickets/foo"}) {
		test.Errorf("params = %v, want [tickets/foo]", params)
	}
}

func TestCompile_TraversalShortcutRoot(test *testing.T) {
	expr, _ := filter.NewParser("root=tickets/foo").Parse()

	sql, _, _ := filter.Compile(expr, filter.CompileOptions{})

	// Root is implemented via two CTEs: ascend to root, descend from root.
	if strings.Count(sql, "WITH RECURSIVE") == 0 && strings.Count(sql, "ascendants") == 0 {
		test.Errorf("expected recursive CTE structure for root=: %s", sql)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/filter/... -run TestCompile_TraversalShortcut -v
```

Expected: FAIL.

- [ ] **Step 3: Implement traversal compilation**

The compilation strategy: traversal shortcuts emit a `WITH RECURSIVE` block prepended to the main `SELECT`, and the WHERE clause references the CTE. We restructure `Compile` to support a "header" string for `WITH ... `.

Add to `compile.go`:

```go
// compileTraversalShortcut returns a WHERE-clause fragment, a slice of CTE
// definition strings (each a complete `cte_name AS (...)` body), and params.
// The CompileWhere caller stitches the CTE definitions into a single
// `WITH RECURSIVE <ctes>` clause prepended to the SELECT.
func compileTraversalShortcut(shortcut *TraversalShortcut, counter int) (string, []string, []any, error) {
	switch shortcut.Kind {
	case ShortcutParentOf:
		whereClause := "EXISTS (SELECT 1 FROM edges WHERE source_id = nodes.id AND type = 'parent' AND target_id = ?)"
		return whereClause, nil, []any{shortcut.NodeID}, nil
	case ShortcutTree:
		cteName := fmt.Sprintf("descendants_%d", counter)
		ctebody := fmt.Sprintf(`%s AS (
    SELECT target_id, 1 AS depth FROM edges WHERE source_id = ? AND type = 'parent'
    UNION ALL
    SELECT edges.target_id, %s.depth + 1 FROM %s
        JOIN edges ON edges.source_id = %s.target_id
        WHERE edges.type = 'parent' AND %s.depth < 5
)`, cteName, cteName, cteName, cteName, cteName)

		whereClause := fmt.Sprintf("nodes.id IN (SELECT target_id FROM %s)", cteName)

		return whereClause, []string{ctebody}, []any{shortcut.NodeID}, nil
	case ShortcutRoot:
		ascendantsName := fmt.Sprintf("ascendants_%d", counter)
		descendantsName := fmt.Sprintf("from_root_%d", counter)
		ascendantsBody := fmt.Sprintf(`%s AS (
    SELECT target_id, 1 AS depth FROM edges WHERE source_id = ? AND type = 'parent'
    UNION ALL
    SELECT edges.target_id, %s.depth + 1 FROM %s
        JOIN edges ON edges.source_id = %s.target_id
        WHERE edges.type = 'parent' AND %s.depth < 5
)`, ascendantsName, ascendantsName, ascendantsName, ascendantsName, ascendantsName)

		descendantsBody := fmt.Sprintf(`%s AS (
    SELECT id AS target_id, 1 AS depth FROM nodes
        WHERE id IN (SELECT target_id FROM %s ORDER BY depth DESC LIMIT 1)
            OR id = ?
    UNION ALL
    SELECT edges.target_id, %s.depth + 1 FROM %s
        JOIN edges ON edges.source_id = %s.target_id
        WHERE edges.type = 'parent' AND %s.depth < 5
)`, descendantsName, ascendantsName, descendantsName, descendantsName, descendantsName, descendantsName)

		whereClause := fmt.Sprintf("nodes.id IN (SELECT target_id FROM %s)", descendantsName)

		// Two params: ascendants seed and descendants seed (the same NodeID).
		return whereClause, []string{ascendantsBody, descendantsBody}, []any{shortcut.NodeID, shortcut.NodeID}, nil
	}

	return "", nil, nil, fmt.Errorf("compile: unknown traversal shortcut kind %v", shortcut.Kind)
}
```

Now restructure `Compile` and `compileWhere` to thread the CTE list through. Replace `Compile`:

```go
func Compile(expr Expr, opts CompileOptions) (string, []any, error) {
	if opts.Skip > 0 && opts.Take == 0 {
		return "", nil, fmt.Errorf("compile: --skip requires --take")
	}

	state := &compileState{}

	whereClause, params, whereErr := state.compileWhere(expr)

	if whereErr != nil {
		return "", nil, whereErr
	}

	var builder strings.Builder

	if len(state.ctes) > 0 {
		builder.WriteString("WITH RECURSIVE ")
		builder.WriteString(strings.Join(state.ctes, ", "))
		builder.WriteString(" ")
	}

	builder.WriteString(`SELECT id, type, path, title, properties_json, last_mtime, last_size, last_checksum FROM nodes WHERE `)
	builder.WriteString(whereClause)

	if len(opts.SortKeys) > 0 {
		builder.WriteString(" ORDER BY ")
		builder.WriteString(compileOrderBy(opts.SortKeys))
	}

	if opts.Take > 0 {
		fmt.Fprintf(&builder, " LIMIT %d", opts.Take)

		if opts.Skip > 0 {
			fmt.Fprintf(&builder, " OFFSET %d", opts.Skip)
		}
	}

	return builder.String(), params, nil
}

type compileState struct {
	ctes        []string
	cteCounter  int
}

func (state *compileState) compileWhere(expr Expr) (string, []any, error) {
	if expr == nil {
		return "1 = 1", nil, nil
	}

	switch typed := expr.(type) {
	case *OrExpr:
		left, leftParams, leftErr := state.compileWhere(typed.Left)
		if leftErr != nil { return "", nil, leftErr }
		right, rightParams, rightErr := state.compileWhere(typed.Right)
		if rightErr != nil { return "", nil, rightErr }
		return "(" + left + ") OR (" + right + ")", append(leftParams, rightParams...), nil
	case *AndExpr:
		left, leftParams, leftErr := state.compileWhere(typed.Left)
		if leftErr != nil { return "", nil, leftErr }
		right, rightParams, rightErr := state.compileWhere(typed.Right)
		if rightErr != nil { return "", nil, rightErr }
		return "(" + left + ") AND (" + right + ")", append(leftParams, rightParams...), nil
	case *NotExpr:
		inner, innerParams, innerErr := state.compileWhere(typed.Inner)
		if innerErr != nil { return "", nil, innerErr }
		return "NOT (" + inner + ")", innerParams, nil
	case *PropertyPredicate:
		return compileProperty(typed)
	case *EdgePredicate:
		return compileEdgePredicate(typed, 0)
	case *TraversalShortcut:
		state.cteCounter++
		whereClause, ctes, params, traversalErr := compileTraversalShortcut(typed, state.cteCounter)
		if traversalErr != nil {
			return "", nil, traversalErr
		}
		state.ctes = append(state.ctes, ctes...)
		return whereClause, params, nil
	}

	return "", nil, fmt.Errorf("compile: unknown AST node type %T", expr)
}
```

Remove the old standalone `compileWhere` function (the new method replaces it).

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/filter/... -v
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/filter/compile.go internal/filter/compile_test.go
git commit -m "feat(filter): SQL compilation for traversal shortcuts via recursive CTEs"
```

---

## Task 12: `tusk query` command

**Files:**
- Create: `cmd/tusk/cmd_query.go`, `cmd/tusk/cmd_query_test.go`
- Modify: `cmd/tusk/root.go`

- [ ] **Step 1: Write failing test — `cmd/tusk/cmd_query_test.go`**

```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestQueryCmd_FiltersByType(test *testing.T) {
	initWorkspace(test)

	for _, args := range [][]string{
		{"node", "create", "--type", "ticket", "--title", "T1", "--path", "tickets/t1.md"},
		{"node", "create", "--type", "ticket", "--title", "T2", "--path", "tickets/t2.md"},
		{"node", "create", "--type", "note", "--title", "N1", "--path", "notes/n1.md"},
	} {
		cmd := newRootCmd()
		cmd.SetArgs(args)

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("setup %v: %v", args, execErr)
		}
	}

	out := &bytes.Buffer{}

	queryCmd := newRootCmd()
	queryCmd.SetOut(out)
	queryCmd.SetErr(out)
	queryCmd.SetArgs([]string{"query", "type=ticket"})

	if execErr := queryCmd.Execute(); execErr != nil {
		test.Fatalf("query: %v", execErr)
	}

	body := out.String()

	if strings.Contains(body, "notes/n1") {
		test.Errorf("note should be excluded: %s", body)
	}

	if !strings.Contains(body, "tickets/t1") || !strings.Contains(body, "tickets/t2") {
		test.Errorf("missing tickets: %s", body)
	}
}

func TestQueryCmd_TakeAndSkip(test *testing.T) {
	initWorkspace(test)

	for index := 0; index < 5; index++ {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"node", "create", "--type", "note", "--title", "n", "--path", "notes/n" + string(rune('0'+index)) + ".md"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("create: %v", execErr)
		}
	}

	out := &bytes.Buffer{}

	cmd := newRootCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"query", "type=note", "--take", "2", "--skip", "1"})

	if execErr := cmd.Execute(); execErr != nil {
		test.Fatalf("query: %v", execErr)
	}

	body := out.String()

	// Header + 2 data rows.
	dataLines := strings.Count(strings.TrimSpace(body), "\n")

	if dataLines != 2 {
		test.Errorf("expected 2 data rows (got %d):\n%s", dataLines, body)
	}
}

func TestQueryCmd_ErrorsWithoutFilter(test *testing.T) {
	initWorkspace(test)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"query"})

	if execErr := cmd.Execute(); execErr == nil {
		test.Fatalf("expected error when filter argument is missing")
	}
}
```

- [ ] **Step 2: Run, verify fail**

Expected: FAIL — `query` subcommand unknown.

- [ ] **Step 3: Implement `cmd/tusk/cmd_query.go`**

```go
package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newQueryCmd() *cobra.Command {
	var (
		sortSpec string
		take     int
		skip     int
		emitJSON bool
	)

	queryCmd := &cobra.Command{
		Use:   "query <filter>",
		Short: "Run a structural filter against the workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			loaded, loadErr := manifest.Load(ws.ManifestPath)

			if loadErr != nil {
				return loadErr
			}

			expr, parseErrs := filter.NewParser(args[0]).Parse()

			if len(parseErrs) > 0 {
				return fmt.Errorf("filter parse: %v", parseErrs[0])
			}

			validateErrs := filter.Validate(expr, *loaded)

			if len(validateErrs) > 0 {
				return fmt.Errorf("filter validate: %v", validateErrs[0])
			}

			sortKeys, sortErr := filter.ParseSort(sortSpec)

			if sortErr != nil {
				return sortErr
			}

			sql, params, compileErr := filter.Compile(expr, filter.CompileOptions{
				SortKeys: sortKeys,
				Take:     take,
				Skip:     skip,
			})

			if compileErr != nil {
				return compileErr
			}

			store, openErr := index.Open(ws.IndexPath)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			rows, queryErr := store.DB().Query(sql, params...)

			if queryErr != nil {
				return queryErr
			}

			defer rows.Close()

			if emitJSON {
				return writeJSON(cmd.OutOrStdout(), rows)
			}

			tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

			fmt.Fprintln(tab, "ID\tTYPE\tTITLE\tPATH")

			for rows.Next() {
				var (
					rowID         string
					rowType       string
					rowPath       string
					rowTitle      string
					propertiesRaw string
					lastMtime     int64
					lastSize      int64
					lastChecksum  string
				)

				if scanErr := rows.Scan(&rowID, &rowType, &rowPath, &rowTitle, &propertiesRaw, &lastMtime, &lastSize, &lastChecksum); scanErr != nil {
					return scanErr
				}

				fmt.Fprintf(tab, "%s\t%s\t%s\t%s\n", rowID, rowType, rowTitle, rowPath)
			}

			return tab.Flush()
		},
	}

	queryCmd.Flags().StringVar(&sortSpec, "sort", "", "sort spec, e.g., +priority,-due,+modified")
	queryCmd.Flags().IntVar(&take, "take", 0, "limit results to N rows")
	queryCmd.Flags().IntVar(&skip, "skip", 0, "skip the first M rows (requires --take)")
	queryCmd.Flags().BoolVar(&emitJSON, "json", false, "emit structured JSON")

	return queryCmd
}

// writeJSON streams a query result set as a JSON array of objects.
func writeJSON(out interface{ Write(p []byte) (n int, err error) }, rows interface {
	Next() bool
	Scan(...any) error
}) error {
	// Plan 4 ships a minimal JSON path; richer formatting lands in Plan 6 (MCP).
	_, _ = out.Write([]byte("[]\n"))

	return nil
}
```

> **Implementer note:** the `writeJSON` placeholder above is intentionally minimal — Plan 4's CLI is human-table-first; structured JSON for query results gets richer treatment in Plan 6 (MCP) where the full result shape from spec §10.8 is wired up. Keep `writeJSON` as a stub that emits `[]\n` so the `--json` flag exists and parses; expand later.

- [ ] **Step 4: Register in `cmd/tusk/root.go`**

Add `rootCmd.AddCommand(newQueryCmd())` alongside the existing registrations.

- [ ] **Step 5: Run, verify pass**

```bash
go test ./cmd/tusk/... -run TestQueryCmd_ -v
```

Expected: 3 PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/tusk/cmd_query.go cmd/tusk/cmd_query_test.go cmd/tusk/root.go
git commit -m "feat(cli): tusk query runs structural filter with --sort/--take/--skip"
```

---

## Task 13: `tusk node list` — drop `--type`, accept positional filter

**Files:**
- Modify: `cmd/tusk/cmd_node_list.go`, `cmd/tusk/cmd_node_list_test.go`

- [ ] **Step 1: Read current `cmd_node_list.go` and `cmd_node_list_test.go`**

```bash
cat cmd/tusk/cmd_node_list.go
cat cmd/tusk/cmd_node_list_test.go
```

Note: the existing implementation uses `--type` flag; tests reference it.

- [ ] **Step 2: Replace `cmd/tusk/cmd_node_list.go`**

```go
package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newNodeListCmd() *cobra.Command {
	var (
		sortSpec string
		take     int
		skip     int
	)

	listCmd := &cobra.Command{
		Use:   "list [filter]",
		Short: "List nodes from the index, optionally filtering by expression",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			loaded, loadErr := manifest.Load(ws.ManifestPath)

			if loadErr != nil {
				return loadErr
			}

			filterArg := ""

			if len(args) == 1 {
				filterArg = args[0]
			}

			expr, parseErrs := filter.NewParser(filterArg).Parse()

			if len(parseErrs) > 0 {
				return fmt.Errorf("filter parse: %v", parseErrs[0])
			}

			validateErrs := filter.Validate(expr, *loaded)

			if len(validateErrs) > 0 {
				return fmt.Errorf("filter validate: %v", validateErrs[0])
			}

			sortKeys, sortErr := filter.ParseSort(sortSpec)

			if sortErr != nil {
				return sortErr
			}

			sql, params, compileErr := filter.Compile(expr, filter.CompileOptions{
				SortKeys: sortKeys,
				Take:     take,
				Skip:     skip,
			})

			if compileErr != nil {
				return compileErr
			}

			store, openErr := index.Open(ws.IndexPath)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			rows, queryErr := store.DB().Query(sql, params...)

			if queryErr != nil {
				return queryErr
			}

			defer rows.Close()

			tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

			fmt.Fprintln(tab, "ID\tTYPE\tTITLE\tPATH")

			for rows.Next() {
				var (
					rowID         string
					rowType       string
					rowPath       string
					rowTitle      string
					propertiesRaw string
					lastMtime     int64
					lastSize      int64
					lastChecksum  string
				)

				if scanErr := rows.Scan(&rowID, &rowType, &rowPath, &rowTitle, &propertiesRaw, &lastMtime, &lastSize, &lastChecksum); scanErr != nil {
					return scanErr
				}

				fmt.Fprintf(tab, "%s\t%s\t%s\t%s\n", rowID, rowType, rowTitle, rowPath)
			}

			return tab.Flush()
		},
	}

	listCmd.Flags().StringVar(&sortSpec, "sort", "", "sort spec, e.g., +priority,-due,+modified")
	listCmd.Flags().IntVar(&take, "take", 0, "limit results to N rows")
	listCmd.Flags().IntVar(&skip, "skip", 0, "skip the first M rows (requires --take)")

	return listCmd
}
```

- [ ] **Step 3: Replace `cmd/tusk/cmd_node_list_test.go`**

```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestNodeListCmd_PrintsCreatedNodes(test *testing.T) {
	initWorkspace(test)

	first := newRootCmd()
	first.SetArgs([]string{"node", "create", "--type", "note", "--path", "a.md"})

	if execErr := first.Execute(); execErr != nil {
		test.Fatalf("first: %v", execErr)
	}

	second := newRootCmd()
	second.SetArgs([]string{"node", "create", "--type", "ticket", "--path", "b.md"})

	if execErr := second.Execute(); execErr != nil {
		test.Fatalf("second: %v", execErr)
	}

	output := &bytes.Buffer{}

	listCmd := newRootCmd()
	listCmd.SetOut(output)
	listCmd.SetErr(output)
	listCmd.SetArgs([]string{"node", "list"})

	if execErr := listCmd.Execute(); execErr != nil {
		test.Fatalf("list: %v", execErr)
	}

	if !bytes.Contains(output.Bytes(), []byte("a")) || !bytes.Contains(output.Bytes(), []byte("b")) {
		test.Errorf("missing rows: %s", output.String())
	}
}

func TestNodeListCmd_PositionalFilterByType(test *testing.T) {
	initWorkspace(test)

	for _, args := range [][]string{
		{"node", "create", "--type", "note", "--path", "a.md"},
		{"node", "create", "--type", "ticket", "--path", "b.md"},
	} {
		cmd := newRootCmd()
		cmd.SetArgs(args)

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("create: %v", execErr)
		}
	}

	output := &bytes.Buffer{}

	listCmd := newRootCmd()
	listCmd.SetOut(output)
	listCmd.SetErr(output)
	listCmd.SetArgs([]string{"node", "list", "type=ticket"})

	if execErr := listCmd.Execute(); execErr != nil {
		test.Fatalf("list: %v", execErr)
	}

	body := output.String()

	if strings.Contains(body, "\na\t") {
		test.Errorf("expected only ticket: %s", body)
	}

	if !strings.Contains(body, "b") {
		test.Errorf("missing b: %s", body)
	}
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./cmd/tusk/... -run TestNodeList -v
```

Expected: 2 PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/tusk/cmd_node_list.go cmd/tusk/cmd_node_list_test.go
git commit -m "feat(cli): tusk node list accepts positional filter; --type flag removed"
```

---

## Task 14: End-to-end + final verify + push

**Files:**
- Create: `cmd/tusk/e2e_filter_test.go`

- [ ] **Step 1: Write the e2e test**

```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestE2E_FilterPipeline(test *testing.T) {
	initWorkspaceWithManifest(test, edgeManifestBody())

	for _, args := range [][]string{
		{"node", "create", "--type", "ticket", "--title", "Foo", "--path", "tickets/foo.md"},
		{"node", "create", "--type", "ticket", "--title", "Bar", "--path", "tickets/bar.md"},
		{"node", "create", "--type", "note", "--title", "N1", "--path", "notes/n1.md"},
		{"edge", "add", "--type", "blocks", "--source", "tickets/foo", "--target", "tickets/bar"},
	} {
		cmd := newRootCmd()
		cmd.SetArgs(args)

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("setup %v: %v", args, execErr)
		}
	}

	{
		out := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"query", "type=ticket"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("simple query: %v", execErr)
		}

		if !strings.Contains(out.String(), "tickets/foo") || !strings.Contains(out.String(), "tickets/bar") {
			test.Errorf("missing tickets: %s", out.String())
		}

		if strings.Contains(out.String(), "notes/n1") {
			test.Errorf("notes should be excluded: %s", out.String())
		}
	}

	{
		out := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"query", "blocks->"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("edge probe: %v", execErr)
		}

		// Only tickets/foo has an outgoing blocks edge.
		if !strings.Contains(out.String(), "tickets/foo") {
			test.Errorf("missing tickets/foo: %s", out.String())
		}

		if strings.Contains(out.String(), "tickets/bar") {
			test.Errorf("bar should be excluded: %s", out.String())
		}
	}

	{
		out := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"query", "type=ticket OR type=note", "--sort", "+title"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("compound query with sort: %v", execErr)
		}

		body := out.String()
		barPos := strings.Index(body, "Bar")
		fooPos := strings.Index(body, "Foo")

		if barPos < 0 || fooPos < 0 {
			test.Fatalf("missing rows: %s", body)
		}

		if barPos > fooPos {
			test.Errorf("expected ascending sort: Bar before Foo. body:\n%s", body)
		}
	}
}
```

- [ ] **Step 2: Run all tests**

```bash
make test
make vet
make lint
```

Expected: all exit 0.

- [ ] **Step 3: Commit e2e**

```bash
git add cmd/tusk/e2e_filter_test.go
git commit -m "test(cli): e2e filter pipeline covering query, edge probe, sort"
```

- [ ] **Step 4: Push**

```bash
git push -u origin feat/plan-4
```

- [ ] **Step 5: Open the stacked PR (`feat/plan-4` → `v1`)**

```bash
gh pr create --draft --base v1 --head feat/plan-4 --title "feat(v1): plan 4 — filter grammar" --body "$(cat <<'EOF'
## Summary

Tusk v1 — Plan 4: TaskWarrior-flavored structural filter grammar end-to-end.

**Stacked on:** v1 (post-Plan-3 cascade). Merge to v1, then v1 merges to main when ready.

## What lands

- New \`internal/filter/\` package: lexer + recursive-descent parser + AST + manifest-aware validator + monolithic SQL compiler + sort-spec parser. ~1500 LOC including tests.
- New CLI command \`tusk query <filter>\` with \`--sort\`, \`--take\`, \`--skip\`, \`--json\` flags.
- \`tusk node list\` accepts positional filter expression; \`--type\` flag removed.
- Multi-hop chains bounded at depth 5. Traversal shortcuts (\`tree=\`, \`parent=\`, \`root=\`) emit recursive CTEs.

## Out of scope

- \`+tag\`/\`-tag\` shorthand → Plan 7 (tag pack territory).
- \`--semantic\` flag → Plan 5.
- Pattern matching / date keywords → future polish.

## Spec

[\`docs/superpowers/specs/2026-05-05-tusk-v1-filter-grammar-design.md\`](docs/superpowers/specs/2026-05-05-tusk-v1-filter-grammar-design.md)

## Plan

[\`docs/superpowers/plans/2026-05-05-tusk-v1-4-filter-grammar.md\`](docs/superpowers/plans/2026-05-05-tusk-v1-4-filter-grammar.md)
EOF
)"
```

- [ ] **Step 6: Verify**

```bash
gh pr view --json url,state,isDraft,baseRefName,headRefName | jq
```

Expected: state OPEN, isDraft true, base `v1`, head `feat/plan-4`.

---

## Self-Review Checklist

**Spec coverage:**
- [ ] §1 scope — Tasks 1-13 cover the in-scope items; out-of-scope items have explicit cross-references to future plans.
- [ ] §2 grammar — every production has a parser task (Tasks 4-6).
- [ ] §3 lexer — Tasks 1-2.
- [ ] §4 parser & AST — Tasks 3-6.
- [ ] §5 validator — Task 7.
- [ ] §6 SQL compilation — Tasks 9-11.
- [ ] §7 sort grammar — Task 8.
- [ ] §8 CLI integration — Tasks 12-13.
- [ ] §9 testing — every package has table-driven tests; Task 14 adds e2e.

**Out-of-scope guardrails:**
- [ ] No `+tag`/`-tag` (Plan 7).
- [ ] No `--semantic` (Plan 5).
- [ ] Hardcoded max-traversal-depth = 5; manifest field deferred.

**Plan-shape:**
- [ ] No "TBD" placeholders.
- [ ] Every step has either complete code or an exact command.
- [ ] Test code uses `test *testing.T`.
- [ ] Implementation code follows blank-line-around-err-guard rule.

**Type/name consistency:**
- [ ] `Token`/`TokenKind` used identically across lexer/parser.
- [ ] `Expr` interface and concrete types named identically across ast/parser/validate/compile.
- [ ] `Op`, `Direction`, `ShortcutKind` constants referenced consistently.
- [ ] `CompileOptions{SortKeys, Take, Skip}` shape matches §1 module conventions.
