# Inline Syntax Modifier AST Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Promote the `+`/`-` token prefix into a first-class AST property on `syntax.FieldFilter` via an open, extensible registry — so consumers (filter, task add/modify, workflow config, future project config) stop hand-rolling string prefix parsing and can pattern-match on `FieldFilter.Modifier` instead.

**Architecture:**
- The `syntax` package owns an extensible `Modifiers` registry (byte set). Defaults to `{'+', '-'}`; new runes are added with a one-line constant change plus consumer opt-in.
- `syntax.Token` gains a `Modifier byte` field. The lexer strips a recognized prefix from a token's raw body and records it on the token. The token's `Value` always holds the bare form (no prefix).
- `syntax.FieldFilter` gains a matching `Modifier byte` field, copied from the token by every parser that produces `FieldFilter`s. The syntax package attaches no domain meaning.
- A new `syntax.ParseFields` produces a validation-free `FilterSet` from any input. `filter.Parse` layers task-field validation on top; `internal/tui/workflow_parse.go` switches to the neutral `syntax.ParseFields` and reads `FieldFilter.Modifier` instead of inspecting strings.
- Tag tokens are normalised the same way: their `Value` holds the bare name, `Modifier` records `+`/`-`. `TokenTagInclude` / `TokenTagExclude` classifications are preserved for backwards compatibility with existing switch statements.

**Tech Stack:** Go 1.22+, existing `syntax`/`filter`/`internal/tui` packages, `go test ./...`, table-driven tests.

---

## File Structure

**New files:**
- `syntax/modifier.go` — registry type, `DefaultModifiers`, helper predicates.
- `syntax/modifier_test.go` — registry tests + extensibility proof.
- `syntax/parse_fields.go` — neutral `ParseFields(input)` producing a `FilterSet` with no field validation.
- `syntax/parse_fields_test.go` — unit tests for neutral parsing and modifier propagation.

**Modified files:**
- `syntax/token.go` — add `Modifier byte` to `Token`, rework `Lex` to consult the registry and strip prefixes.
- `syntax/token_test.go` — add lexer cases for modifier extraction, bare tokens, tag prefixes, and custom registry lexing.
- `syntax/ast.go` — add `Modifier byte` to `FieldFilter`.
- `syntax/ast_test.go` — extend existing helpers/tests (`HasField`, `GetField`) to ignore the new field where relevant.
- `filter/parser.go` — strip modifier from token before validator lookup; propagate onto `FieldFilter.Modifier`; update tag parsing to read `tok.Value` directly (not `tok.Value[1:]`).
- `filter/parse_expr.go` — mirror the same changes in the expression term parser.
- `filter/parser_test.go` / `filter/parse_expr_test.go` — assert `Modifier` is set and bare key validates.
- `internal/tui/workflow_parse.go` — drop inline `raw[0] == '+'` inspection; call `syntax.ParseFields` (or inspect `Token.Modifier` on the lexer output) and read `FieldFilter.Modifier`.
- `internal/tui/workflow_parse_test.go` (if present; create if missing during migration task).

No other package is touched. `inmem`, `sqlite`, `service`, `repository`, and `cmd/tusk` all consume `filter.FilterSet` / `filter.Expr` and pattern-match on domain fields that are unchanged.

---

## Task 1: Modifier registry in the syntax package

**Files:**
- Create: `syntax/modifier.go`
- Create: `syntax/modifier_test.go`

- [ ] **Step 1: Write the failing test**

Create `syntax/modifier_test.go`:

```go
package syntax

import "testing"

func TestDefaultModifiersContainsPlusAndMinus(t *testing.T) {
	m := DefaultModifiers()
	if !m.Has('+') {
		t.Errorf("DefaultModifiers should contain '+'")
	}
	if !m.Has('-') {
		t.Errorf("DefaultModifiers should contain '-'")
	}
}

func TestDefaultModifiersRejectsUnknown(t *testing.T) {
	m := DefaultModifiers()
	for _, b := range []byte{'?', '*', '!', 'a', '0', '='} {
		if m.Has(b) {
			t.Errorf("DefaultModifiers should not contain %q", b)
		}
	}
}

func TestModifierSetWithAddsRune(t *testing.T) {
	m := DefaultModifiers().With('?')
	if !m.Has('?') {
		t.Errorf("With('?') should register '?'")
	}
	// Original must remain unchanged (immutability)
	if DefaultModifiers().Has('?') {
		t.Errorf("DefaultModifiers() should not be mutated by With")
	}
}

func TestModifierSetZeroValueHasNothing(t *testing.T) {
	var m ModifierSet
	if m.Has('+') {
		t.Errorf("zero ModifierSet should not contain '+'")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./syntax -run TestDefaultModifiers -v`
Expected: FAIL — `undefined: DefaultModifiers`, `undefined: ModifierSet`.

- [ ] **Step 3: Implement the registry**

Create `syntax/modifier.go`:

```go
package syntax

// ModifierSet is an immutable set of byte-valued token prefix markers the
// lexer recognises. The syntax package attaches no semantics to any member —
// whether '+' means "add", "include", or "append" is the consumer's call.
//
// Extending the recognised set is deliberately a one-line change: add a byte
// to the slice returned by DefaultModifiers, or call With on an existing set
// at construction time. Consumers then opt into interpreting the new rune.
type ModifierSet struct {
	mask [256]bool
}

// DefaultModifiers returns the built-in set recognised by Lex.
// To register a new global modifier, append its byte here.
func DefaultModifiers() ModifierSet {
	var m ModifierSet
	m.mask['+'] = true
	m.mask['-'] = true
	return m
}

// Has reports whether b is registered in the set.
func (m ModifierSet) Has(b byte) bool {
	return m.mask[b]
}

// With returns a copy of the set with b added. The receiver is not modified.
func (m ModifierSet) With(b byte) ModifierSet {
	out := m
	out.mask[b] = true
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./syntax -run TestDefaultModifiers -v && go test ./syntax -run TestModifierSet -v`
Expected: PASS for all four tests.

- [ ] **Step 5: Commit**

```bash
git add syntax/modifier.go syntax/modifier_test.go
git commit -m "feat(syntax): add ModifierSet registry for token prefix markers"
```

---

## Task 2: Add `Modifier` byte to `Token` and thread it through `Lex` for field tokens

**Files:**
- Modify: `syntax/token.go`
- Modify: `syntax/token_test.go`

- [ ] **Step 1: Write the failing test**

Append to `syntax/token_test.go`:

```go
func TestLexFieldWithPlusModifier(t *testing.T) {
	tokens, errs := Lex("+urgency.blocking-weight=2")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}
	tok := tokens[0]
	if tok.Type != TokenField {
		t.Errorf("expected TokenField, got %v", tok.Type)
	}
	if tok.Modifier != '+' {
		t.Errorf("expected Modifier '+', got %q", tok.Modifier)
	}
	if tok.Value != "urgency.blocking-weight=2" {
		t.Errorf("expected bare value, got %q", tok.Value)
	}
}

func TestLexFieldWithMinusModifier(t *testing.T) {
	tokens, _ := Lex("-status=review")
	if len(tokens) != 1 || tokens[0].Modifier != '-' || tokens[0].Value != "status=review" {
		t.Fatalf("unexpected tokens: %+v", tokens)
	}
}

func TestLexFieldWithoutModifier(t *testing.T) {
	tokens, _ := Lex("status=active")
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}
	if tokens[0].Modifier != 0 {
		t.Errorf("expected zero Modifier, got %q", tokens[0].Modifier)
	}
	if tokens[0].Value != "status=active" {
		t.Errorf("expected unchanged value, got %q", tokens[0].Value)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./syntax -run TestLexField -v`
Expected: FAIL — `tokens[0].Modifier undefined` or value mismatch.

- [ ] **Step 3: Add the field and update `Lex`**

In `syntax/token.go`, update the `Token` struct:

```go
// Token is a single lexed element from an input string.
type Token struct {
	Type     TokenType
	Value    string // raw text of the token, with any recognised prefix modifier stripped
	Modifier byte   // registered prefix marker ('+' / '-' / ...); 0 if none
	Pos      int    // byte offset in the original input
}
```

Still in `syntax/token.go`, replace the field classification block in `Lex`. Locate the switch that currently reads `case raw[0] == '+':` / `case raw[0] == '-':` / `case isFieldToken(raw):` and replace it with the modifier-aware variant:

```go
		// Classify the token
		modifiers := DefaultModifiers()
		var modifier byte
		body := raw

		if len(raw) >= 2 && modifiers.Has(raw[0]) {
			// A registered prefix is only a modifier if what follows is a
			// field or tag body — not a lone "+" or "-".
			modifier = raw[0]
			body = raw[1:]
		}

		switch {
		case len(raw) == 1 && modifiers.Has(raw[0]):
			errs = append(errs, ParseError{
				Pos:     start,
				Message: fmt.Sprintf("bare %q is not a valid token; use %s<name> for tags", raw, raw),
			})

		case hasEquals(body) && isFieldToken(body):
			tokens = append(tokens, Token{
				Type:     TokenField,
				Value:    body,
				Modifier: modifier,
				Pos:      start,
			})

		case modifier == '+':
			tokens = append(tokens, Token{
				Type:     TokenTagInclude,
				Value:    body,
				Modifier: modifier,
				Pos:      start,
			})

		case modifier == '-':
			tokens = append(tokens, Token{
				Type:     TokenTagExclude,
				Value:    body,
				Modifier: modifier,
				Pos:      start,
			})

		case isFieldToken(raw):
			tokens = append(tokens, Token{Type: TokenField, Value: raw, Pos: start})

		case raw == "AND":
			tokens = append(tokens, Token{Type: TokenAnd, Value: raw, Pos: start})

		case raw == "OR":
			tokens = append(tokens, Token{Type: TokenOr, Value: raw, Pos: start})

		case raw == "NOT":
			tokens = append(tokens, Token{Type: TokenNot, Value: raw, Pos: start})

		default:
			tokens = append(tokens, Token{Type: TokenText, Value: raw, Pos: start})
		}
```

Note the two behavioural changes:
- Field tokens carrying a modifier now expose `Value` without the leading `+`/`-` (bare form).
- Tag tokens also expose the bare name in `Value` — the leading rune is moved to `Modifier`.

- [ ] **Step 4: Update existing lexer tests that assumed the prefix was in `Value`**

Run: `go test ./syntax -v`

Any test that currently asserts `tok.Value == "+urgent"` or `tok.Value == "-bug"` for tag tokens must be updated to assert `tok.Value == "urgent"` and `tok.Modifier == '+'`. Read `syntax/token_test.go`, find these cases, and update them. Expected failures after step 3 that must now pass:

- Tag lexing tests that expected `tok.Value[0] == '+'`
- Field lexing tests that expected `tok.Value == "+key=value"` with no modifier concept

Leave the token ordering, positions, and count assertions exactly as-is. Only the prefix/value split changes.

- [ ] **Step 5: Run the full syntax test suite**

Run: `go test ./syntax -v`
Expected: PASS, including the new `TestLexFieldWithPlusModifier` / `TestLexFieldWithMinusModifier` / `TestLexFieldWithoutModifier` cases from step 1 and any repaired pre-existing cases from step 4.

- [ ] **Step 6: Commit**

```bash
git add syntax/token.go syntax/token_test.go
git commit -m "feat(syntax): lex registered prefix modifiers onto Token.Modifier"
```

---

## Task 3: Repair `filter` tag parsing and propagate modifiers through `filter.Parse` / `filter.ParseExpr`

Because Task 2 changed tag `Value` to the bare form, `filter/parser.go` and `filter/parse_expr.go` still slice `tok.Value[1:]` and will now drop the first real character. Fix the slicing and wire `FieldFilter.Modifier` at the same time.

**Files:**
- Modify: `syntax/ast.go`
- Modify: `filter/parser.go`
- Modify: `filter/parse_expr.go`
- Modify: `filter/parser_test.go`
- Modify: `filter/parse_expr_test.go`

- [ ] **Step 1: Write the failing test (field modifier round-trip)**

Append to `filter/parser_test.go`:

```go
func TestParseFieldCarriesPlusModifier(t *testing.T) {
	// Parser currently rejects unknown fields; use a known one.
	fs, errs := Parse("+priority=3")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(fs.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(fs.Fields))
	}
	f := fs.Fields[0]
	if f.Key != "priority" {
		t.Errorf("expected Key=priority, got %q", f.Key)
	}
	if f.Value != "3" {
		t.Errorf("expected Value=3, got %q", f.Value)
	}
	if f.Modifier != '+' {
		t.Errorf("expected Modifier='+', got %q", f.Modifier)
	}
}

func TestParseFieldCarriesMinusModifier(t *testing.T) {
	fs, errs := Parse("-priority=2")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(fs.Fields) != 1 || fs.Fields[0].Modifier != '-' {
		t.Fatalf("expected Modifier='-', got %+v", fs.Fields)
	}
}

func TestParseBareFieldHasZeroModifier(t *testing.T) {
	fs, _ := Parse("priority=3")
	if len(fs.Fields) != 1 || fs.Fields[0].Modifier != 0 {
		t.Fatalf("expected zero modifier, got %+v", fs.Fields)
	}
}

func TestParseTagRoundTrip(t *testing.T) {
	fs, errs := Parse("+urgent -blocked")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(fs.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(fs.Tags))
	}
	if fs.Tags[0].Name != "urgent" || fs.Tags[0].Exclude {
		t.Errorf("tag[0]: %+v", fs.Tags[0])
	}
	if fs.Tags[1].Name != "blocked" || !fs.Tags[1].Exclude {
		t.Errorf("tag[1]: %+v", fs.Tags[1])
	}
}
```

Append to `filter/parse_expr_test.go`:

```go
func TestParseExprFieldCarriesModifier(t *testing.T) {
	expr, errs := ParseExpr("+priority=4")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	term, ok := expr.(TermExpr)
	if !ok || term.Field == nil {
		t.Fatalf("expected TermExpr with Field, got %T", expr)
	}
	if term.Field.Modifier != '+' || term.Field.Key != "priority" || term.Field.Value != "4" {
		t.Errorf("unexpected field: %+v", term.Field)
	}
}

func TestParseExprTagRoundTrip(t *testing.T) {
	expr, _ := ParseExpr("+urgent")
	term, ok := expr.(TermExpr)
	if !ok || term.Tag == nil || term.Tag.Name != "urgent" || term.Tag.Exclude {
		t.Fatalf("unexpected: %+v", expr)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./filter -run "TestParseField|TestParseBareField|TestParseTagRoundTrip|TestParseExprField|TestParseExprTagRoundTrip" -v`
Expected: FAIL — `f.Modifier` undefined, or `Tag.Name == "rgent"` (slicing bug introduced by Task 2).

- [ ] **Step 3: Add `Modifier` to `FieldFilter`**

Edit `syntax/ast.go`:

```go
// FieldFilter represents a key=value term.
type FieldFilter struct {
	Key      string // field name (e.g. "status", "project", "uda.env")
	Value    string // raw value string, unparsed
	Modifier byte   // registered prefix marker ('+' / '-' / ...); 0 if none
	Pos      int    // byte offset in input
}
```

- [ ] **Step 4: Update `filter/parser.go`**

Edit `filter/parser.go`. In the `TokenField` branch, propagate `tok.Modifier` to every `FieldFilter{}` literal (both the UDA path and the validated path). In the tag branches, drop the `[1:]` slice. Diff intent:

```go
		case TokenTagInclude:
			fs.Tags = append(fs.Tags, TagFilter{
				Name: tok.Value, // already bare after Task 2
				Pos:  tok.Pos,
			})

		case TokenTagExclude:
			fs.Tags = append(fs.Tags, TagFilter{
				Name:    tok.Value,
				Exclude: true,
				Pos:     tok.Pos,
			})

		case TokenField:
			key, value, _ := strings.Cut(tok.Value, "=")
			if udaKey, ok := strings.CutPrefix(key, "uda."); ok {
				// ... existing validation unchanged ...
				fs.Fields = append(fs.Fields, FieldFilter{
					Key:      key,
					Value:    value,
					Modifier: tok.Modifier,
					Pos:      tok.Pos,
				})
				continue
			}
			validator, known := fieldValidators[key]
			// ... existing validation unchanged ...
			fs.Fields = append(fs.Fields, FieldFilter{
				Key:      key,
				Value:    value,
				Modifier: tok.Modifier,
				Pos:      tok.Pos,
			})
```

- [ ] **Step 5: Update `filter/parse_expr.go`**

Edit `filter/parse_expr.go` `parseTerm` identically: drop `[1:]` on `TokenTagInclude` / `TokenTagExclude` (`Name: tok.Value`), and populate `Modifier: tok.Modifier` on every `FieldFilter` literal (UDA branch and static-validator branch).

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./filter/... -v`
Expected: PASS for the new tests and no regressions in the existing suite.

- [ ] **Step 7: Run the full repo to catch fan-out breakage**

Run: `go test ./... -count=1`
Expected: PASS across all packages. If `internal/tui` compiles against old tag slicing, fix references there (see Task 4).

- [ ] **Step 8: Commit**

```bash
git add syntax/ast.go filter/parser.go filter/parse_expr.go filter/parser_test.go filter/parse_expr_test.go
git commit -m "feat(filter): propagate token modifier onto FieldFilter; fix tag value slicing"
```

---

## Task 4: Neutral `syntax.ParseFields` producing a validation-free `FilterSet`

Workflow config lexes tokens and hand-builds status / transition entries. It needs a parser that produces `FilterSet` / `FieldFilter` without calling task-specific `fieldValidators`. Introduce `syntax.ParseFields` as a shared primitive; `filter.Parse` can still layer validation on top, but Task 5 will migrate workflow parsing to the neutral one.

**Files:**
- Create: `syntax/parse_fields.go`
- Create: `syntax/parse_fields_test.go`

- [ ] **Step 1: Write the failing test**

Create `syntax/parse_fields_test.go`:

```go
package syntax

import "testing"

func TestParseFieldsBareAndModified(t *testing.T) {
	fs, errs := ParseFields("status=active +status=review -status=done +urgent -blocked title=\"Hello world\"")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(fs.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(fs.Fields))
	}

	want := []struct {
		key      string
		value    string
		modifier byte
	}{
		{"status", "active", 0},
		{"status", "review", '+'},
		{"status", "done", '-'},
	}
	for i, w := range want {
		got := fs.Fields[i]
		if got.Key != w.key || got.Value != w.value || got.Modifier != w.modifier {
			t.Errorf("field[%d] = %+v, want %+v", i, got, w)
		}
	}

	if len(fs.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(fs.Tags))
	}
	if fs.Tags[0].Name != "urgent" || fs.Tags[0].Exclude {
		t.Errorf("tag[0] = %+v", fs.Tags[0])
	}
	if fs.Tags[1].Name != "blocked" || !fs.Tags[1].Exclude {
		t.Errorf("tag[1] = %+v", fs.Tags[1])
	}

	if len(fs.Text) != 1 || fs.Text[0] != "Hello world" {
		t.Errorf("text = %+v", fs.Text)
	}
}

func TestParseFieldsDoesNotValidate(t *testing.T) {
	// "bogus" is not a known task field; ParseFields must accept it because
	// it's the neutral syntax-level parse with no validator.
	fs, errs := ParseFields("bogus=yes")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(fs.Fields) != 1 || fs.Fields[0].Key != "bogus" || fs.Fields[0].Value != "yes" {
		t.Fatalf("unexpected fields: %+v", fs.Fields)
	}
}

func TestParseFieldsRejectsMalformedField(t *testing.T) {
	// A standalone "=" has no key — a lexer-level error, surfaced through ParseFields.
	_, errs := ParseFields("=value")
	if len(errs) == 0 {
		t.Errorf("expected error for malformed field")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./syntax -run TestParseFields -v`
Expected: FAIL — `undefined: ParseFields`.

- [ ] **Step 3: Implement `ParseFields`**

Create `syntax/parse_fields.go`:

```go
package syntax

import "strings"

// ParseFields lexes input and folds the tokens into a FilterSet without any
// domain validation. It is the shared primitive used by filter.Parse
// (which adds task-field validation) and by consumer commands like workflow
// and project config that maintain their own field vocabularies.
//
// Each FieldFilter carries Key, Value, and Modifier exactly as the lexer
// produced them. Boolean keywords (AND/OR/NOT) and parens are preserved as
// free text, mirroring filter.Parse behaviour for non-expression uses.
func ParseFields(input string) (FilterSet, []ParseError) {
	tokens, errs := Lex(input)
	var fs FilterSet

	for _, tok := range tokens {
		switch tok.Type {
		case TokenField:
			key, value, ok := strings.Cut(tok.Value, "=")
			if !ok || key == "" {
				errs = append(errs, ParseError{
					Pos:     tok.Pos,
					Message: "malformed field: missing key",
				})
				continue
			}
			fs.Fields = append(fs.Fields, FieldFilter{
				Key:      key,
				Value:    value,
				Modifier: tok.Modifier,
				Pos:      tok.Pos,
			})

		case TokenTagInclude:
			fs.Tags = append(fs.Tags, TagFilter{
				Name: tok.Value,
				Pos:  tok.Pos,
			})

		case TokenTagExclude:
			fs.Tags = append(fs.Tags, TagFilter{
				Name:    tok.Value,
				Exclude: true,
				Pos:     tok.Pos,
			})

		case TokenText, TokenAnd, TokenOr, TokenNot, TokenLParen, TokenRParen:
			fs.Text = append(fs.Text, tok.Value)
		}
	}

	return fs, errs
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./syntax -v`
Expected: PASS for all `TestParseFields*` cases plus the existing suite.

- [ ] **Step 5: Commit**

```bash
git add syntax/parse_fields.go syntax/parse_fields_test.go
git commit -m "feat(syntax): add neutral ParseFields primitive with modifier propagation"
```

---

## Task 5: Migrate `internal/tui/workflow_parse.go` to read `FieldFilter.Modifier`

Drop the string-prefix inspection. The workflow parser now calls `syntax.ParseFields`, enforces its own field vocabulary (`status`, `transition`), and interprets `Modifier` as add/remove.

**Files:**
- Modify: `internal/tui/workflow_parse.go`
- Create: `internal/tui/workflow_parse_test.go` (only if no unit test file exists for workflow_parse; otherwise append to the existing one).

- [ ] **Step 1: Check for existing test file**

Run: `ls internal/tui/workflow_parse_test.go 2>/dev/null || echo missing`

If the result is `missing`, create a new file; otherwise append to the existing one in step 2. The step 2 test cases work in either file.

- [ ] **Step 2: Write the failing tests**

Use these tests (append, or drop them into the new file):

```go
package tui

import (
	"testing"

	"github.com/germanamz/tusk/config"
)

func TestParseWorkflowModifyAddsStatusWithPlus(t *testing.T) {
	mut, err := parseWorkflowModify([]string{"+status=review(highlight)"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := mut.AddStatuses["review"]
	if !ok {
		t.Fatalf("expected review in AddStatuses, got %+v", mut.AddStatuses)
	}
	if len(got.Roles) != 1 || got.Roles[0] != "highlight" {
		t.Errorf("roles = %+v, want [highlight]", got.Roles)
	}
	if _, hit := mut.SetStatuses["review"]; hit {
		t.Errorf("SetStatuses should be empty, got %+v", mut.SetStatuses)
	}
}

func TestParseWorkflowModifyRemovesStatusWithMinus(t *testing.T) {
	mut, err := parseWorkflowModify([]string{"-status=done"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mut.RemoveStatuses) != 1 || mut.RemoveStatuses[0] != "done" {
		t.Errorf("RemoveStatuses = %+v", mut.RemoveStatuses)
	}
}

func TestParseWorkflowModifySetsBareStatus(t *testing.T) {
	mut, err := parseWorkflowModify([]string{"status=active(start,highlight)"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := mut.SetStatuses["active"]
	if !ok {
		t.Fatalf("expected active in SetStatuses, got %+v", mut.SetStatuses)
	}
	if len(got.Roles) != 2 || got.Roles[0] != "start" || got.Roles[1] != "highlight" {
		t.Errorf("roles = %+v", got.Roles)
	}
}

func TestParseWorkflowModifyTransitionRequiresModifier(t *testing.T) {
	_, err := parseWorkflowModify([]string{"transition=pending:active"})
	if err == nil {
		t.Fatalf("expected error for bare transition")
	}
}

func TestParseWorkflowModifyAddAndRemoveTransitions(t *testing.T) {
	mut, err := parseWorkflowModify([]string{
		"+transition=pending:active",
		"-transition=active:done",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mut.AddTransitions) != 1 ||
		mut.AddTransitions[0] != (config.WorkflowTransitionConfig{From: "pending", To: "active"}) {
		t.Errorf("AddTransitions = %+v", mut.AddTransitions)
	}
	if len(mut.RemoveTransitions) != 1 ||
		mut.RemoveTransitions[0] != (config.WorkflowTransitionConfig{From: "active", To: "done"}) {
		t.Errorf("RemoveTransitions = %+v", mut.RemoveTransitions)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail or (more likely) pass for the wrong reason**

Run: `go test ./internal/tui -run TestParseWorkflowModify -v`

Expected: the **add/remove** test cases may already pass (existing string-prefix logic), but after Task 2 the `tok.Value` for `+status=review(highlight)` is `status=review(highlight)` (no leading `+`), so the legacy `raw[0] == '+'` branch never fires and these tests should start FAILING with "expected review in AddStatuses" or equivalent. Confirm the regression before fixing — it proves Task 2 is observable here.

- [ ] **Step 4: Replace the body of `parseWorkflowModify`**

Edit `internal/tui/workflow_parse.go`. Replace the `parseWorkflowModify` function with the modifier-aware version (note: uses `syntax.ParseFields` and reads `f.Modifier`):

```go
// parseWorkflowModify parses inline args into a WorkflowMutation.
func parseWorkflowModify(args []string) (config.WorkflowMutation, error) {
	input := strings.Join(args, " ")
	fs, parseErrs := syntax.ParseFields(input)
	if len(parseErrs) > 0 {
		return config.WorkflowMutation{}, fmt.Errorf("parse error: %s", parseErrs[0].Message)
	}

	mut := config.WorkflowMutation{
		SetStatuses: make(map[string]config.StatusConfig),
		AddStatuses: make(map[string]config.StatusConfig),
	}

	for _, f := range fs.Fields {
		if f.Value == "" {
			return config.WorkflowMutation{}, fmt.Errorf("invalid field %q", f.Key)
		}

		switch f.Key {
		case "status":
			name, roles := parseStatusValue(f.Value)
			switch f.Modifier {
			case '+':
				mut.AddStatuses[name] = config.StatusConfig{Roles: roles}
			case '-':
				mut.RemoveStatuses = append(mut.RemoveStatuses, name)
			case 0:
				mut.SetStatuses[name] = config.StatusConfig{Roles: roles}
			default:
				return config.WorkflowMutation{}, fmt.Errorf("unsupported modifier %q on status", f.Modifier)
			}

		case "transition":
			transitions, err := parseTransitions(f.Value)
			if err != nil {
				return config.WorkflowMutation{}, err
			}
			switch f.Modifier {
			case '+':
				mut.AddTransitions = append(mut.AddTransitions, transitions...)
			case '-':
				mut.RemoveTransitions = append(mut.RemoveTransitions, transitions...)
			default:
				return config.WorkflowMutation{}, fmt.Errorf("transition requires + or - modifier (e.g., +transition=from:to)")
			}

		default:
			return config.WorkflowMutation{}, fmt.Errorf("unknown field %q (expected 'status' or 'transition')", f.Key)
		}
	}

	return mut, nil
}
```

Also update `parseWorkflowCreate` to use `syntax.ParseFields` for consistency (no modifier semantics, but it benefits from a single code path). Replace its body with:

```go
// parseWorkflowCreate parses inline args into a WorkflowConfig.
func parseWorkflowCreate(args []string) (config.WorkflowConfig, error) {
	input := strings.Join(args, " ")
	fs, parseErrs := syntax.ParseFields(input)
	if len(parseErrs) > 0 {
		return config.WorkflowConfig{}, fmt.Errorf("parse error: %s", parseErrs[0].Message)
	}

	wf := config.WorkflowConfig{Statuses: make(map[string]config.StatusConfig)}

	for _, f := range fs.Fields {
		if f.Modifier != 0 {
			return config.WorkflowConfig{}, fmt.Errorf("workflow create does not accept modifier %q on %q", f.Modifier, f.Key)
		}
		if f.Value == "" {
			return config.WorkflowConfig{}, fmt.Errorf("invalid field %q", f.Key)
		}
		switch f.Key {
		case "status":
			name, roles := parseStatusValue(f.Value)
			wf.Statuses[name] = config.StatusConfig{Roles: roles}
		case "transition":
			transitions, err := parseTransitions(f.Value)
			if err != nil {
				return config.WorkflowConfig{}, err
			}
			wf.Transitions = append(wf.Transitions, transitions...)
		default:
			return config.WorkflowConfig{}, fmt.Errorf("unknown field %q (expected 'status' or 'transition')", f.Key)
		}
	}

	if len(wf.Statuses) == 0 {
		return config.WorkflowConfig{}, fmt.Errorf("at least one status is required")
	}
	return wf, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui -run TestParseWorkflow -v`
Expected: PASS for both create and modify, including all step-2 cases.

- [ ] **Step 6: Run the full repo to catch any remaining regressions**

Run: `go test ./... -count=1`
Expected: PASS. Likely fallout includes any workflow-related e2e cases that construct args differently — none are expected to break because the grammar is unchanged from the user's perspective.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/workflow_parse.go internal/tui/workflow_parse_test.go
git commit -m "refactor(tui): read FieldFilter.Modifier in workflow parse instead of string prefix"
```

---

## Task 6: Extensibility proof — register a new modifier and verify the lexer path

The registry is the contract for "one-line change adds a prefix". Prove it with a test that plugs `'?'` into the registry and asserts the lexer yields it, without touching `filter`, `internal/tui`, or any consumer. The test needs a lexer entry point that accepts a custom registry.

**Files:**
- Modify: `syntax/token.go`
- Modify: `syntax/modifier_test.go`

- [ ] **Step 1: Write the failing test**

Append to `syntax/modifier_test.go`:

```go
func TestLexWithModifiersRegistersNewPrefix(t *testing.T) {
	set := DefaultModifiers().With('?')
	tokens, errs := LexWithModifiers("?priority=3", set)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}
	tok := tokens[0]
	if tok.Type != TokenField {
		t.Errorf("expected TokenField, got %v", tok.Type)
	}
	if tok.Modifier != '?' {
		t.Errorf("expected Modifier='?', got %q", tok.Modifier)
	}
	if tok.Value != "priority=3" {
		t.Errorf("expected bare value, got %q", tok.Value)
	}
}

func TestLexDoesNotRecogniseUnregisteredPrefix(t *testing.T) {
	// With the default registry, '?' is not a modifier — it stays inside the value.
	tokens, _ := Lex("?priority=3")
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}
	if tokens[0].Modifier != 0 {
		t.Errorf("expected no modifier, got %q", tokens[0].Modifier)
	}
	// Without '?' in the registry, the leading '?' is still part of raw, so the
	// token classifies as Text (no leading '+'/'-', no '=' at position > 0 after
	// the '?'... actually the '=' is present, so it's still a Field).
	if tokens[0].Type != TokenField {
		t.Errorf("expected TokenField with ? in key, got %v (value=%q)", tokens[0].Type, tokens[0].Value)
	}
	if tokens[0].Value != "?priority=3" {
		t.Errorf("expected '?priority=3', got %q", tokens[0].Value)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./syntax -run TestLex -v`
Expected: FAIL — `undefined: LexWithModifiers`.

- [ ] **Step 3: Refactor `Lex` into `LexWithModifiers` + thin wrapper**

Edit `syntax/token.go`. Rename the existing `Lex` body to `LexWithModifiers(input string, modifiers ModifierSet)`, accepting the registry as a parameter. Inside the function body, replace the `modifiers := DefaultModifiers()` line from Task 2 with the parameter. Add back a public `Lex` that delegates:

```go
// Lex splits the input string into tokens using the default modifier registry.
func Lex(input string) ([]Token, []ParseError) {
	return LexWithModifiers(input, DefaultModifiers())
}

// LexWithModifiers is Lex with an explicit modifier registry, for consumers
// that opt into additional prefix markers beyond the default '+' and '-'.
func LexWithModifiers(input string, modifiers ModifierSet) ([]Token, []ParseError) {
	// ... existing Lex body, using `modifiers` instead of DefaultModifiers() ...
}
```

Keep every other line of the current `Lex` body byte-for-byte identical. The only change is hoisting `modifiers` up to the parameter.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./syntax -v`
Expected: PASS for `TestLexWithModifiersRegistersNewPrefix`, `TestLexDoesNotRecogniseUnregisteredPrefix`, plus no regressions.

- [ ] **Step 5: Commit**

```bash
git add syntax/token.go syntax/modifier_test.go
git commit -m "feat(syntax): expose LexWithModifiers for consumer-level registry extension"
```

---

## Task 7: Full repo verification and roadmap check-off

- [ ] **Step 1: Run the full test suite with the race detector**

Run: `make test-race`
Expected: PASS. Investigate any failure; do not move on until green.

- [ ] **Step 2: Run the linter**

Run: `make vet && make lint`
Expected: PASS. Address any findings by editing the specific files the linter calls out.

- [ ] **Step 3: Tick the roadmap checkboxes**

Edit `ROADMAP.md`. Under `### Initiative: Inline Syntax Modifier AST → Story: Field modifier AST`, flip every `- [ ]` bullet to `- [x]`. Do not edit any other section.

- [ ] **Step 4: Commit the roadmap update**

```bash
git add ROADMAP.md
git commit -m "docs: mark Inline Syntax Modifier AST initiative complete"
```

---

## Self-Review Checklist

**Spec coverage:**
- [x] `FieldFilter.Modifier` — Task 3 (`syntax/ast.go`).
- [x] Open, extensible registry — Task 1 (`ModifierSet`) + Task 6 (`LexWithModifiers`).
- [x] Lexer consults registry on first char — Task 2.
- [x] `FieldFilter.Key` / `Value` always expose the bare form — Task 2 (field) + Task 3 (parser wiring).
- [x] Syntax package attaches no semantics — enforced by `ParseFields` being validator-free (Task 4) and the doc comments on `ModifierSet` / `Token.Modifier` (Tasks 1-2).
- [x] Migrate `internal/tui/workflow_parse.go` — Task 5.
- [x] Migrate filter + task add/modify parsers — Task 3 (`filter.Parse` is used by both filter queries and task add/modify).
- [x] Unit tests per registered modifier with no semantic interpretation — Task 2 (field) + Task 6 (extensibility).
- [x] Consumer-level tests in their own packages — Task 3 (`filter/`) + Task 5 (`internal/tui/`).

**Placeholder scan:** none — every code step contains complete code or a clearly scoped repeat instruction pointing at the exact file and symbol.

**Type consistency:**
- `ModifierSet` / `DefaultModifiers()` / `With` / `Has` — defined Task 1, used Tasks 2, 6.
- `Token.Modifier byte` — added Task 2, read Tasks 3, 4, 5.
- `FieldFilter.Modifier byte` — added Task 3, read Tasks 4, 5.
- `syntax.ParseFields(string) (FilterSet, []ParseError)` — added Task 4, used Task 5.
- `LexWithModifiers(string, ModifierSet) ([]Token, []ParseError)` — added Task 6; existing `Lex` keeps its signature for Tasks 2-5 (they call `Lex`, which now delegates after Task 6).

All identifiers line up across tasks.
