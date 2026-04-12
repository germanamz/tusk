# Phase 2 — Inline Syntax Modifier AST: Lexer Swap + Consumer Migration

> **Implementer directive.** You are executing Phase 2 of the "Inline Syntax Modifier AST" initiative. This doc is authoritative. Use `docs/superpowers/plans/2026-04-12-inline-syntax-modifier-ast.md` only as optional background design context; the phase-1 doc at `…-phase-1.md` is optional reference for what already shipped. Do not invent work outside the tasks below.

## Prerequisites

- **Phase 1 must be fully merged.** Verify before starting:
  - `syntax/modifier.go` exists with `ModifierSet`, `DefaultModifiers`, `With`, `Has`.
  - `syntax.Token` has a `Modifier byte` field.
  - `syntax.FieldFilter` has a `Modifier byte` field.
  - `syntax.LexWithModifiers(input string, modifiers ModifierSet) ([]Token, []ParseError)` exists and `syntax.Lex` delegates to it with `DefaultModifiers()`.
- Run the Phase 1 sanity check: `go test ./... -count=1 && make vet && make lint`. Stop if any fail.

## Inherits From

From Phase 1 the implementer should expect:
- Additive `Modifier byte` fields on `Token` and `FieldFilter`, always zero-valued.
- `LexWithModifiers` exists but the `modifiers` parameter is discarded inside the body (`_ = modifiers`). The body is otherwise identical to legacy `Lex`: tag `Value` still includes the leading `+`/`-`, field tokens starting with `+`/`-` still carry the prefix as part of their raw `Value`, and `filter.Parse` / `filter.ParseExpr` still slice `tok.Value[1:]` for tag names.
- `filter/parser.go`, `filter/parse_expr.go`, and `internal/tui/workflow_parse.go` untouched from `main`. They still hand-roll string prefix inspection.
- `ROADMAP.md` still has the initiative checkboxes unticked.

## Goal

Make the lexer actually strip registered prefixes into `Token.Modifier`, wire every consumer to read `FieldFilter.Modifier`, introduce the neutral `syntax.ParseFields` primitive, migrate `internal/tui/workflow_parse.go` to read the AST field instead of raw strings, and prove extensibility with a `'?'` registration test. Tick the roadmap.

## User-Visible Behavior To Preserve

Every one of these must still hold after this phase, byte-identical to `main` except where the initiative explicitly fixes a gap:
- `tusk list +urgent` still filters on tag `urgent`.
- `tusk list -blocked` still excludes tag `blocked`.
- `tusk workflow modify <name> +status=review(highlight)` still adds the `review` status with the `highlight` role.
- `tusk workflow modify <name> -status=done` still removes the `done` status.
- `tusk workflow modify <name> +transition=pending:active` still adds the transition.
- `tusk workflow modify <name> status=active(start,highlight)` (no prefix) still sets the roles on `active`.
- `tusk workflow create …` output is byte-identical to `main` given identical input.
- All existing e2e tests pass without modification.

Explicit fix (new behavior): `filter.Parse("+priority=3")` and `filter.ParseExpr("-priority=4")` no longer error with `unknown field "+priority"` / `"-priority"`. They produce a valid `FieldFilter{Key:"priority", Value:"3", Modifier:'+', …}`. This is the whole point of the initiative.

## File Structure

**Create:**
- `syntax/parse_fields.go`
- `syntax/parse_fields_test.go`
- `internal/tui/workflow_parse_test.go` (only if it does not already exist; otherwise append to the existing one).

**Modify:**
- `syntax/token.go` — rewrite the classification block inside `LexWithModifiers` to consult the `modifiers` parameter and strip prefixes.
- `syntax/token_test.go` — add modifier-aware lexer cases; repair any existing case that asserted tag `Value` included the leading `+`/`-`.
- `syntax/modifier_test.go` — add extensibility tests using `LexWithModifiers` with a custom registry.
- `filter/parser.go` — fix tag `Name` slicing (`tok.Value`, not `tok.Value[1:]`), propagate `tok.Modifier` onto every `FieldFilter{}` literal.
- `filter/parse_expr.go` — same as above for `parseTerm`.
- `filter/parser_test.go`, `filter/parse_expr_test.go` — add cases asserting `Modifier` propagation.
- `internal/tui/workflow_parse.go` — switch both `parseWorkflowCreate` and `parseWorkflowModify` to `syntax.ParseFields` + `FieldFilter.Modifier`.
- `ROADMAP.md` — tick the "Field modifier AST" story checkboxes.

---

## Task 1: Make `LexWithModifiers` actually strip registered prefixes

**Files:**
- Modify: `syntax/token.go`
- Modify: `syntax/token_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `syntax/token_test.go`:

```go
func TestLexStripsPlusModifierFromFieldToken(t *testing.T) {
	tokens, errs := Lex("+priority=3")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}
	tok := tokens[0]
	if tok.Type != TokenField {
		t.Errorf("type = %v, want TokenField", tok.Type)
	}
	if tok.Modifier != '+' {
		t.Errorf("modifier = %q, want '+'", tok.Modifier)
	}
	if tok.Value != "priority=3" {
		t.Errorf("value = %q, want %q", tok.Value, "priority=3")
	}
}

func TestLexStripsMinusModifierFromFieldToken(t *testing.T) {
	tokens, _ := Lex("-priority=3")
	if len(tokens) != 1 || tokens[0].Modifier != '-' || tokens[0].Value != "priority=3" {
		t.Fatalf("unexpected tokens: %+v", tokens)
	}
}

func TestLexBareFieldHasZeroModifier(t *testing.T) {
	tokens, _ := Lex("status=active")
	if len(tokens) != 1 || tokens[0].Modifier != 0 || tokens[0].Value != "status=active" {
		t.Fatalf("unexpected tokens: %+v", tokens)
	}
}

func TestLexStripsPlusModifierFromTagToken(t *testing.T) {
	tokens, errs := Lex("+urgent")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}
	tok := tokens[0]
	if tok.Type != TokenTagInclude {
		t.Errorf("type = %v, want TokenTagInclude", tok.Type)
	}
	if tok.Modifier != '+' {
		t.Errorf("modifier = %q, want '+'", tok.Modifier)
	}
	if tok.Value != "urgent" {
		t.Errorf("value = %q, want %q", tok.Value, "urgent")
	}
}

func TestLexStripsMinusModifierFromTagToken(t *testing.T) {
	tokens, _ := Lex("-blocked")
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}
	if tokens[0].Type != TokenTagExclude || tokens[0].Modifier != '-' || tokens[0].Value != "blocked" {
		t.Fatalf("unexpected token: %+v", tokens[0])
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./syntax -run TestLexStrips -v`
Expected: FAIL — the Phase 1 lexer still emits tag `Value` as `+urgent` / `-blocked` and field `Value` as `+priority=3`.

- [ ] **Step 3: Rewrite the classification block**

Edit `syntax/token.go`. Inside `LexWithModifiers`, find the final classification switch (the block that begins with `// Classify the token` and ends with the `default: tokens = append(..., TokenText ...)` case). Replace that block — and only that block — with the modifier-aware version below. Remove the `_ = modifiers` discard line at the top of the function (it is no longer unused).

```go
		// Classify the token
		var modifier byte
		body := raw

		if len(raw) >= 2 && modifiers.Has(raw[0]) {
			// A registered prefix is only a modifier if what follows is a
			// real field or tag body — not a lone "+" / "-".
			modifier = raw[0]
			body = raw[1:]
		}

		switch {
		case len(raw) == 1 && modifiers.Has(raw[0]):
			errs = append(errs, ParseError{
				Pos:     start,
				Message: fmt.Sprintf("bare %q is not a valid token; use %s<name> for tags", raw, raw),
			})

		case isFieldToken(body):
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

Key semantics:
- `body` holds the raw token with any recognised prefix stripped. `modifier` holds the stripped rune (`0` if none).
- `isFieldToken(body)` is consulted *after* stripping, so `+priority=3` now classifies as `TokenField` with `Value="priority=3"` and `Modifier='+'`.
- Tag detection falls out of `modifier != 0 && !isFieldToken(body)`. No `hasEquals` branch is needed — `isFieldToken` already checks for `=`.
- `len(raw) == 1 && modifiers.Has(raw[0])` still catches the bare `+` / `-` error case. (`len(raw) >= 2` guard above ensures we only strip when something follows.)

Do not change the paren / quote / whitespace handling above the switch.

- [ ] **Step 4: Repair pre-existing lexer tests**

Run: `go test ./syntax -v`

Two categories of pre-existing test need attention:

**(a) Value-shape assertions.** Any test that asserted `tok.Value == "+urgent"`, `tok.Value == "-blocked"`, `tok.Value == "+key=value"`, or similar must be updated to assert the stripped form (`tok.Value == "urgent"` / `"blocked"` / `"key=value"`) plus `tok.Modifier == '+'` / `'-'`. Do not loosen any other assertion — token count, positions, and types stay the same. Read `syntax/token_test.go` end-to-end and update every affected case.

**(b) Phase 1 bridge-code test — delete it.** Phase 1 added a test named `TestLexWithModifiersIgnoredParameterInPhase1` in `syntax/token_test.go`. It asserts that `LexWithModifiers("?priority=3", DefaultModifiers())` and `LexWithModifiers("?priority=3", DefaultModifiers().With('?'))` produce identical tokens. That assertion was only valid while the `modifiers` parameter was discarded in the body. After step 3 rewires the body, the two calls intentionally diverge (`Modifier='?'` vs `Modifier=0`), so the assertion is wrong by design. **Delete the entire `TestLexWithModifiersIgnoredParameterInPhase1` function** from `syntax/token_test.go`. Keep `TestLexWithModifiersDelegatesWithDefaultRegistry` — that one remains correct because the default registry behavior is preserved. The extensibility behavior is re-covered by the new `TestLexWithModifiersRegistersCustomPrefix` test in Task 5 of this phase.

- [ ] **Step 5: Run the syntax suite**

Run: `go test ./syntax -v`
Expected: PASS including the five new `TestLexStrips*` cases and every repaired legacy case.

- [ ] **Step 6: Commit**

```bash
git add syntax/token.go syntax/token_test.go
git commit -m "feat(syntax): strip registered prefix modifiers into Token.Modifier"
```

After this commit `filter/...` and `internal/tui/...` are temporarily broken: `filter/parser.go` still slices `tok.Value[1:]` for tags (which now drops the first real character) and still rejects `+priority=3` because the token's `Value` is now `priority=3` but the parser never reads `Modifier`. Task 2 fixes both.

---

## Task 2: Fix `filter/parser.go` and `filter/parse_expr.go`

**Files:**
- Modify: `filter/parser.go`
- Modify: `filter/parse_expr.go`
- Modify: `filter/parser_test.go`
- Modify: `filter/parse_expr_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `filter/parser_test.go`:

```go
func TestParseFieldCarriesPlusModifier(t *testing.T) {
	fs, errs := Parse("+priority=3")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(fs.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(fs.Fields))
	}
	f := fs.Fields[0]
	if f.Key != "priority" || f.Value != "3" || f.Modifier != '+' {
		t.Errorf("field = %+v", f)
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
		t.Errorf("tag[0] = %+v", fs.Tags[0])
	}
	if fs.Tags[1].Name != "blocked" || !fs.Tags[1].Exclude {
		t.Errorf("tag[1] = %+v", fs.Tags[1])
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

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./filter -run "TestParseField|TestParseBareField|TestParseTagRoundTrip|TestParseExprField|TestParseExprTagRoundTrip" -v`
Expected: FAIL — either "expected 1 field, got 0" (unknown-field error) or "tag[0] = {Name:rgent …}" (broken slicing).

- [ ] **Step 3: Patch `filter/parser.go`**

Open `filter/parser.go`. In the `for _, tok := range tokens` loop, update every case as follows.

Tag cases — drop the `[1:]` slice because `tok.Value` is already bare after Task 1:

```go
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
```

Field case — propagate `tok.Modifier` in both the UDA branch and the static-validator branch. Replace the full `case TokenField:` block with:

```go
		case TokenField:
			key, value, _ := strings.Cut(tok.Value, "=")
			if udaKey, ok := strings.CutPrefix(key, "uda."); ok {
				if udaKey == "" {
					errs = append(errs, ParseError{
						Pos:     tok.Pos,
						Field:   key,
						Message: "empty UDA key name",
					})
					continue
				}
				if err := domain.ValidateUDAKey(udaKey); err != nil {
					errs = append(errs, ParseError{
						Pos:     tok.Pos,
						Field:   key,
						Message: err.Error(),
					})
					continue
				}
				fs.Fields = append(fs.Fields, FieldFilter{
					Key:      key,
					Value:    value,
					Modifier: tok.Modifier,
					Pos:      tok.Pos,
				})
				continue
			}
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
				Key:      key,
				Value:    value,
				Modifier: tok.Modifier,
				Pos:      tok.Pos,
			})
```

- [ ] **Step 4: Patch `filter/parse_expr.go`**

Open `filter/parse_expr.go`. In `parseTerm`:

- `case TokenTagInclude`: change `Name: tok.Value[1:]` → `Name: tok.Value`.
- `case TokenTagExclude`: change `Name: tok.Value[1:]` → `Name: tok.Value`.
- `case TokenField`: in both the UDA success branch and the static-validator success branch, update the `TermExpr{Field: &FieldFilter{…}}` literal to include `Modifier: tok.Modifier`. Example for the static-validator branch:

```go
			return TermExpr{Field: &FieldFilter{
				Key:      key,
				Value:    value,
				Modifier: tok.Modifier,
				Pos:      tok.Pos,
			}}
```

Apply the same literal shape in the UDA branch.

- [ ] **Step 5: Run the filter suite**

Run: `go test ./filter/... -v`
Expected: PASS, including all new `TestParseField* / TestParseExpr*` cases.

- [ ] **Step 6: Run the full repo**

Run: `go test ./... -count=1`
Expected: `syntax`, `filter`, `service`, and `tests/e2e` all PASS. `internal/tui` is currently expected to PASS too, because on `main` there is no `workflow_parse_test.go` unit test and no e2e case depends on the `+`/`-` prefix behavior of workflow commands specifically — a spot-check with `grep -R parseWorkflowModify tests` confirms zero callers outside `internal/tui/workflow_parse.go` itself. If you do see `internal/tui` failures here they are **not** expected — stop and investigate before moving on. Task 4 will rewire `workflow_parse.go` to read `FieldFilter.Modifier`; do not skip ahead to fix it here.

- [ ] **Step 7: Commit**

```bash
git add filter/parser.go filter/parse_expr.go filter/parser_test.go filter/parse_expr_test.go
git commit -m "feat(filter): propagate token modifier onto FieldFilter; fix tag value slicing"
```

---

## Task 3: Add `syntax.ParseFields` neutral primitive

**Files:**
- Create: `syntax/parse_fields.go`
- Create: `syntax/parse_fields_test.go`

- [ ] **Step 1: Write the failing tests**

Create `syntax/parse_fields_test.go`:

```go
package syntax

import "testing"

func TestParseFieldsCarriesBareAndModifiedFields(t *testing.T) {
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

func TestParseFieldsDoesNotValidateDomain(t *testing.T) {
	fs, errs := ParseFields("bogus=yes")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(fs.Fields) != 1 || fs.Fields[0].Key != "bogus" || fs.Fields[0].Value != "yes" {
		t.Fatalf("unexpected fields: %+v", fs.Fields)
	}
}

func TestParseFieldsRejectsMalformedField(t *testing.T) {
	_, errs := ParseFields("=value")
	if len(errs) == 0 {
		t.Errorf("expected error for empty-key field")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./syntax -run TestParseFields -v`
Expected: FAIL — `undefined: ParseFields`.

- [ ] **Step 3: Implement `ParseFields`**

Create `syntax/parse_fields.go`:

```go
package syntax

import "strings"

// ParseFields lexes input and folds the tokens into a FilterSet without any
// domain validation. It is the shared primitive used by filter.Parse (which
// adds task-field validation on top) and by consumer commands such as
// workflow and project config that maintain their own field vocabularies.
//
// Every FieldFilter carries Key, Value, and Modifier exactly as the lexer
// produced them. Boolean keywords (AND/OR/NOT) and parentheses are preserved
// as free text, mirroring filter.Parse behaviour for non-expression uses.
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

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./syntax -v`
Expected: PASS for all `TestParseFields*` cases plus no regressions.

- [ ] **Step 5: Commit**

```bash
git add syntax/parse_fields.go syntax/parse_fields_test.go
git commit -m "feat(syntax): add neutral ParseFields primitive with modifier propagation"
```

---

## Task 4: Migrate `internal/tui/workflow_parse.go`

**Files:**
- Modify: `internal/tui/workflow_parse.go`
- Create or modify: `internal/tui/workflow_parse_test.go`

- [ ] **Step 1: Check whether a test file exists**

Run: `ls internal/tui/workflow_parse_test.go 2>/dev/null || echo missing`

If `missing`, create a new file in step 2. Otherwise append to the existing one.

- [ ] **Step 2: Write the failing tests**

Ensure the file starts with `package tui` and these imports (merge with existing imports if appending):

```go
import (
	"testing"

	"github.com/germanamz/tusk/config"
)
```

Add the following test functions:

```go
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

func TestParseWorkflowCreateAcceptsBareStatuses(t *testing.T) {
	wf, err := parseWorkflowCreate([]string{
		"status=pending(initial)",
		"status=active(start,highlight)",
		"status=done(terminal,done)",
		"+transition=pending:active",
	})
	// Phase 2 note: parseWorkflowCreate rejects modifiers — the +transition
	// above must produce an error, not a parsed create.
	if err == nil {
		t.Fatalf("expected error for modifier on workflow create, got wf=%+v", wf)
	}
}

func TestParseWorkflowCreateHappyPath(t *testing.T) {
	wf, err := parseWorkflowCreate([]string{
		"status=pending(initial)",
		"status=active(start,highlight)",
		"status=done(terminal,done)",
		"transition=pending:active",
		"transition=active:done",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := wf.Statuses["pending"]; !ok {
		t.Errorf("missing pending status: %+v", wf.Statuses)
	}
	if len(wf.Transitions) != 2 {
		t.Errorf("transitions = %+v", wf.Transitions)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/tui -run TestParseWorkflow -v`
Expected: FAIL — after Task 1, `tok.Value` for `+status=review(highlight)` is now `status=review(highlight)` (no leading `+`), so the legacy `raw[0] == '+'` branch in `parseWorkflowModify` never fires. The add/remove tests error out.

- [ ] **Step 4: Rewrite `parseWorkflowModify`**

Edit `internal/tui/workflow_parse.go`. Replace the entire `parseWorkflowModify` function with:

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

- [ ] **Step 5: Rewrite `parseWorkflowCreate` for consistency**

Still in `internal/tui/workflow_parse.go`, replace `parseWorkflowCreate` with:

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

Leave `parseStatusValue` and `parseTransitions` untouched.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/tui -run TestParseWorkflow -v`
Expected: PASS for every case.

- [ ] **Step 7: Run the full repo**

Run: `go test ./... -count=1`
Expected: PASS everywhere including `tests/e2e`.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/workflow_parse.go internal/tui/workflow_parse_test.go
git commit -m "refactor(tui): read FieldFilter.Modifier in workflow parse instead of string prefix"
```

---

## Task 5: Extensibility proof — custom modifier via `LexWithModifiers`

**Files:**
- Modify: `syntax/modifier_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `syntax/modifier_test.go`:

```go
func TestLexWithModifiersRegistersCustomPrefix(t *testing.T) {
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
		t.Errorf("type = %v, want TokenField", tok.Type)
	}
	if tok.Modifier != '?' {
		t.Errorf("modifier = %q, want '?'", tok.Modifier)
	}
	if tok.Value != "priority=3" {
		t.Errorf("value = %q, want %q", tok.Value, "priority=3")
	}
}

func TestLexDoesNotRecogniseUnregisteredPrefix(t *testing.T) {
	// The default registry does not include '?', so the '?' stays inside the
	// raw token value. `?priority=3` is still a field token because it still
	// contains '=' at a non-zero position.
	tokens, _ := Lex("?priority=3")
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}
	if tokens[0].Modifier != 0 {
		t.Errorf("expected no modifier, got %q", tokens[0].Modifier)
	}
	if tokens[0].Type != TokenField {
		t.Errorf("type = %v, want TokenField", tokens[0].Type)
	}
	if tokens[0].Value != "?priority=3" {
		t.Errorf("value = %q, want %q", tokens[0].Value, "?priority=3")
	}
}
```

- [ ] **Step 2: Run the tests to verify they pass**

Run: `go test ./syntax -run "TestLexWithModifiersRegistersCustomPrefix|TestLexDoesNotRecogniseUnregisteredPrefix" -v`
Expected: PASS. The `LexWithModifiers` body wired in Task 1 already consults the registry; this task is purely a regression-proof that extensibility works without touching any consumer package.

- [ ] **Step 3: Commit**

```bash
git add syntax/modifier_test.go
git commit -m "test(syntax): prove modifier registry extensibility via LexWithModifiers"
```

---

## Task 6: Final verification and roadmap check-off

- [ ] **Step 1: Race suite + linter**

Run: `make test-race`
Expected: PASS.

Run: `make vet && make lint`
Expected: PASS.

- [ ] **Step 2: CLI smoke test for user-visible behavior**

```bash
make build
./bin/tusk workflow list 2>&1 | head -n 20
./bin/tusk list +urgent 2>&1 | head -n 5
```

Expected: identical output to `main` (tag filtering works, workflow list works, no new errors). Also verify the newly-fixed gap by running:

```bash
./bin/tusk list "+priority=3" 2>&1 | head -n 5
```

On `main` this errors with `unknown field "+priority"`. On this phase it must either succeed or produce an empty result set — never the "unknown field" error. If the "unknown field" error still appears, Task 2 is incomplete; go back.

- [ ] **Step 3: Tick the roadmap checkboxes**

Open `ROADMAP.md`. Find `### Initiative: Inline Syntax Modifier AST` → `**Story: Field modifier AST**`. Flip every `- [ ]` bullet in that story to `- [x]`. Do not edit any other line in `ROADMAP.md`.

- [ ] **Step 4: Commit**

```bash
git add ROADMAP.md
git commit -m "docs: mark Inline Syntax Modifier AST initiative complete"
```

---

## Changes Introduced

**New files:**
- `syntax/parse_fields.go` — exports `ParseFields(input string) (FilterSet, []ParseError)`.
- `syntax/parse_fields_test.go` — neutral parser unit tests.
- `internal/tui/workflow_parse_test.go` — workflow parser modifier tests (only if absent on `main`).

**Modified interfaces / behavior:**
- `syntax.LexWithModifiers` body now consults the `modifiers` parameter. Field tokens beginning with a registered prefix expose the stripped body in `Value` and the prefix byte in `Modifier`. Tag tokens (`TokenTagInclude`, `TokenTagExclude`) expose the bare name in `Value` and the prefix byte in `Modifier`. Bare `+` / `-` remain an error.
- `syntax.Lex` — wrapper signature unchanged; token output shape differs from `main` as described above.
- `filter.Parse` / `filter.ParseExpr` — `TagFilter.Name` no longer slices the first rune. Every `FieldFilter` they produce carries `Modifier`. `+priority=3` / `-priority=3` are now accepted as valid `priority` fields (previously rejected as unknown fields on `main`).
- `internal/tui.parseWorkflowCreate` / `parseWorkflowModify` — now driven by `syntax.ParseFields` + `FieldFilter.Modifier`; string-prefix inspection removed. `parseWorkflowCreate` now explicitly rejects modifiers; `parseWorkflowModify` interprets `+`/`-`/bare identically to `main`.

**Bridge code removed:**
- The `_ = modifiers` discard in `LexWithModifiers` (introduced as bridge code in Phase 1, Task 4) is deleted by Task 1 of this phase. No bridge code remains after this phase.

**New bridge code:** none.

**Environment variables, schema migrations, dependencies:** none.

**Roadmap:** `ROADMAP.md` `Initiative: Inline Syntax Modifier AST → Story: Field modifier AST` checkboxes flipped to complete by Task 6.
