# Phase 1 — Inline Syntax Modifier AST: Additive Foundation

> **Implementer directive.** You are executing Phase 1 of the "Inline Syntax Modifier AST" initiative. This doc is authoritative. Use `docs/superpowers/plans/2026-04-12-inline-syntax-modifier-ast.md` only as optional background design context. Do not implement anything outside the tasks below in this phase. Phase 2 exists and will complete the behavior change — do not race ahead.

## Prerequisites

- Base repository on `main` at the commit where this phase is dispatched. No prior phases.
- Go toolchain available. `make test`, `make vet`, `make lint` all pass on `main` before you begin. Run them once up front and stop if they do not.

## Goal

Introduce the additive building blocks for the modifier initiative with **zero user-visible or behavioral change**:

1. A `syntax.ModifierSet` registry type (default contains `'+'` and `'-'`).
2. A `Modifier byte` field on `syntax.Token` (always `0` after this phase).
3. A `Modifier byte` field on `syntax.FieldFilter` (always `0` after this phase).
4. A refactor of `syntax.Lex` into `syntax.LexWithModifiers(input, ModifierSet)` with the public `Lex` delegating, body otherwise unchanged.

After this phase, `Lex` still produces the exact same tokens it does on `main`: tag values still include the leading `+`/`-`, field tokens still include the leading `+`/`-` in their raw `Value`, and no parser reads `Modifier`. Phase 2 flips the lexer to actually strip the prefix and wires consumers to read `Modifier`. Keeping those changes out of this phase is the whole point — it preserves compile-safety and user behavior.

## User-Visible Behavior To Preserve

After this phase every one of these must still hold, byte-identical to `main`:
- `tusk list +urgent` filters tag `urgent` exactly as before.
- `tusk list +priority=3` and `tusk list -priority=3` produce the same results they do on `main` (which is: an `unknown field "+priority"` / `unknown field "-priority"` error — do **not** fix this in Phase 1; Phase 2 owns it).
- `tusk workflow modify <name> +status=review(highlight)` and `-status=done` behave exactly as they do on `main`. The existing string-prefix inspection in `internal/tui/workflow_parse.go` is untouched in this phase.
- `filter.Parse`, `filter.ParseExpr`, and all e2e tests produce identical output.
- `make test`, `make test-race`, `make vet`, `make lint` all pass.

## File Structure

**Create:**
- `syntax/modifier.go`
- `syntax/modifier_test.go`

**Modify:**
- `syntax/token.go` — add `Modifier byte` to `Token`; rename existing `Lex` body into `LexWithModifiers(input string, modifiers ModifierSet)`; add a thin `Lex` wrapper calling `LexWithModifiers(input, DefaultModifiers())`. Do **not** change lexing logic otherwise.
- `syntax/ast.go` — add `Modifier byte` to `FieldFilter`.

**Do not modify in this phase:**
- `filter/*.go`
- `internal/tui/*.go`
- `syntax/parse_fields.go` (does not exist yet; Phase 2 creates it)
- Any test file other than `syntax/modifier_test.go`, `syntax/token_test.go`, `syntax/ast_test.go`

---

## Task 1: `ModifierSet` registry

**Files:**
- Create: `syntax/modifier.go`
- Create: `syntax/modifier_test.go`

- [ ] **Step 1: Write the failing tests**

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

func TestModifierSetWithAddsRuneImmutably(t *testing.T) {
	base := DefaultModifiers()
	extended := base.With('?')
	if !extended.Has('?') {
		t.Errorf("With('?') should register '?'")
	}
	if base.Has('?') {
		t.Errorf("original set must not be mutated by With")
	}
}

func TestModifierSetZeroValueHasNothing(t *testing.T) {
	var m ModifierSet
	if m.Has('+') {
		t.Errorf("zero ModifierSet should not contain '+'")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./syntax -run "TestDefaultModifiers|TestModifierSet" -v`
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
// in DefaultModifiers, or call With on an existing set to build a custom
// registry at the call site.
type ModifierSet struct {
	mask [256]bool
}

// DefaultModifiers returns the built-in set recognised by the default Lex.
// To register a new global modifier, add its byte here.
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

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./syntax -run "TestDefaultModifiers|TestModifierSet" -v`
Expected: PASS for all four cases.

- [ ] **Step 5: Commit**

```bash
git add syntax/modifier.go syntax/modifier_test.go
git commit -m "feat(syntax): add ModifierSet registry for token prefix markers"
```

---

## Task 2: Add `Modifier byte` to `Token` (additive only)

**Files:**
- Modify: `syntax/token.go`
- Modify: `syntax/token_test.go`

- [ ] **Step 1: Read the current `Token` definition**

Open `syntax/token.go` and locate the `Token` struct (lines ~45–50 on `main`):

```go
type Token struct {
    Type  TokenType
    Value string
    Pos   int
}
```

- [ ] **Step 2: Write a compile-only regression test first**

Append to `syntax/token_test.go`:

```go
func TestTokenModifierFieldExistsAndDefaultsToZero(t *testing.T) {
	// Every token produced by the default Lex in Phase 1 must have Modifier == 0.
	// Phase 2 will change this behavior; until then, the field is additive only.
	cases := []string{
		"status=active",
		"+urgent",
		"-blocked",
		"+priority=3",
		"-priority=3",
		"title=\"hello world\"",
		"project=backend AND status=pending",
	}
	for _, input := range cases {
		tokens, _ := Lex(input)
		for i, tok := range tokens {
			if tok.Modifier != 0 {
				t.Errorf("Lex(%q) tokens[%d].Modifier = %q, want 0", input, i, tok.Modifier)
			}
		}
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./syntax -run TestTokenModifierFieldExistsAndDefaultsToZero -v`
Expected: FAIL — `tok.Modifier undefined`.

- [ ] **Step 4: Add the field to the struct**

Edit `syntax/token.go`. Replace the `Token` struct definition with:

```go
// Token is a single lexed element from an input string.
//
// Modifier is populated by LexWithModifiers when a registered prefix rune
// (e.g. '+', '-') is stripped off the token body. In Phase 1 of the modifier
// initiative Modifier is always 0 — the lexer does not strip prefixes yet —
// but the field is present so downstream consumers can compile against it.
type Token struct {
	Type     TokenType
	Value    string
	Modifier byte
	Pos      int
}
```

Do **not** change any other line of `syntax/token.go` in this task.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./syntax -v`
Expected: PASS for `TestTokenModifierFieldExistsAndDefaultsToZero` plus no regressions anywhere else in the `syntax` package.

- [ ] **Step 6: Run the full repo**

Run: `go test ./... -count=1`
Expected: PASS across all packages (`filter`, `internal/tui`, `service`, e2e, …). Any failure here means you accidentally changed lexer output — revert and redo step 4.

- [ ] **Step 7: Commit**

```bash
git add syntax/token.go syntax/token_test.go
git commit -m "feat(syntax): add additive Modifier byte field to Token"
```

---

## Task 3: Add `Modifier byte` to `FieldFilter` (additive only)

**Files:**
- Modify: `syntax/ast.go`
- Modify: `syntax/ast_test.go`

- [ ] **Step 1: Read the current struct**

Open `syntax/ast.go` and locate `FieldFilter` (near the bottom of the file):

```go
type FieldFilter struct {
    Key   string
    Value string
    Pos   int
}
```

- [ ] **Step 2: Write a compile-only test**

Append to `syntax/ast_test.go`:

```go
func TestFieldFilterModifierFieldExistsAndDefaultsToZero(t *testing.T) {
	f := FieldFilter{Key: "priority", Value: "3"}
	if f.Modifier != 0 {
		t.Errorf("zero-value FieldFilter.Modifier = %q, want 0", f.Modifier)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./syntax -run TestFieldFilterModifierField -v`
Expected: FAIL — `f.Modifier undefined`.

- [ ] **Step 4: Add the field**

Edit `syntax/ast.go`. Replace the `FieldFilter` struct with:

```go
// FieldFilter represents a key=value term.
//
// Modifier carries any registered prefix rune recognised by the lexer
// (e.g. '+', '-'). 0 means "no modifier". The syntax package attaches no
// semantics — consumers interpret it however they like (include/exclude,
// add/remove, numeric delta, ...).
type FieldFilter struct {
	Key      string // field name (e.g. "status", "project", "uda.env")
	Value    string // raw value string, unparsed
	Modifier byte   // registered prefix marker ('+' / '-' / ...); 0 if none
	Pos      int    // byte offset in input
}
```

Do **not** change `HasField`, `GetField`, `IncludeTags`, `ExcludeTags`, `Title`, `TagFilter`, or `FilterSet`.

- [ ] **Step 5: Run the tests**

Run: `go test ./... -count=1`
Expected: PASS everywhere. `filter.FieldFilter` is a type alias of `syntax.FieldFilter`, so it picks up the new field automatically. The filter package writes struct literals with named fields (`FieldFilter{Key: …, Value: …, Pos: …}`) so the additive field is backward compatible.

- [ ] **Step 6: Commit**

```bash
git add syntax/ast.go syntax/ast_test.go
git commit -m "feat(syntax): add additive Modifier byte field to FieldFilter"
```

---

## Task 4: Refactor `Lex` into `LexWithModifiers(input, ModifierSet)` — signature only

**Files:**
- Modify: `syntax/token.go`
- Modify: `syntax/token_test.go`

**Important:** This task changes only the *shape* of the lexer entry point. The body is byte-for-byte unchanged, and the registry parameter is **not yet consulted** inside the body. Phase 2 is responsible for making the body actually read the `modifiers` parameter.

- [ ] **Step 1: Write the failing test**

Append to `syntax/token_test.go`:

```go
func TestLexWithModifiersDelegatesWithDefaultRegistry(t *testing.T) {
	input := "status=active +urgent -blocked title=\"hello\""
	defaultTokens, _ := Lex(input)
	withTokens, _ := LexWithModifiers(input, DefaultModifiers())

	if len(defaultTokens) != len(withTokens) {
		t.Fatalf("token count differs: default=%d, with=%d", len(defaultTokens), len(withTokens))
	}
	for i := range defaultTokens {
		if defaultTokens[i] != withTokens[i] {
			t.Errorf("tokens[%d] differ: default=%+v, with=%+v", i, defaultTokens[i], withTokens[i])
		}
	}
}

func TestLexWithModifiersIgnoredParameterInPhase1(t *testing.T) {
	// Phase 1 does not actually consult the registry in the body yet — passing
	// an extended set must produce the same tokens as the default set.
	custom := DefaultModifiers().With('?')
	a, _ := LexWithModifiers("?priority=3", DefaultModifiers())
	b, _ := LexWithModifiers("?priority=3", custom)
	if len(a) != len(b) {
		t.Fatalf("token count mismatch: default=%d custom=%d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("tokens[%d] differ: default=%+v custom=%+v", i, a[i], b[i])
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./syntax -run TestLexWithModifiers -v`
Expected: FAIL — `undefined: LexWithModifiers`.

- [ ] **Step 3: Refactor `Lex`**

Edit `syntax/token.go`. Find the existing `Lex` function:

```go
func Lex(input string) ([]Token, []ParseError) {
    var tokens []Token
    var errs []ParseError
    ...
    return tokens, errs
}
```

Rename the function to `LexWithModifiers` and add an unused parameter — the body must remain byte-for-byte identical. Do **not** add any new branches; do **not** call `modifiers.Has(...)` inside the body. Phase 2 will wire it in.

Replace with:

```go
// Lex splits input into tokens using the default modifier registry.
func Lex(input string) ([]Token, []ParseError) {
	return LexWithModifiers(input, DefaultModifiers())
}

// LexWithModifiers is Lex with an explicit modifier registry, for consumers
// that opt into additional prefix markers beyond the default '+' and '-'.
//
// Phase 1: the modifiers parameter is accepted but not yet consulted inside
// the body — behavior is identical to the legacy Lex. Phase 2 of the modifier
// AST initiative will make the body read the registry when scanning each
// token's first character.
func LexWithModifiers(input string, modifiers ModifierSet) ([]Token, []ParseError) {
	_ = modifiers // intentionally unused in Phase 1; Phase 2 wires it in
	var tokens []Token
	var errs []ParseError

	// ... original Lex body verbatim ...

	return tokens, errs
}
```

Paste the original `Lex` body verbatim between the variable declarations and the `return`. Do not modify any existing comment, branch, or classification rule inside it.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./syntax -v`
Expected: PASS for the two new delegation tests plus no regressions.

- [ ] **Step 5: Run the full repo**

Run: `go test ./... -count=1`
Expected: PASS everywhere. Behavior is unchanged.

- [ ] **Step 6: Commit**

```bash
git add syntax/token.go syntax/token_test.go
git commit -m "refactor(syntax): extract LexWithModifiers entry point without behavior change"
```

---

## Task 5: Phase 1 verification

- [ ] **Step 1: Run the race suite and linter**

Run: `make test-race`
Expected: PASS.

Run: `make vet && make lint`
Expected: PASS.

- [ ] **Step 2: Confirm user-visible behavior parity**

Build and spot-check the CLI:

```bash
make build
./bin/tusk list +urgent 2>&1 | head -n 5
./bin/tusk workflow list 2>&1 | head -n 20
```

Expected: identical output to `main`. If you see any new error or difference, stop and investigate — the only legitimate source of difference is an unrelated pre-existing change, which there shouldn't be.

- [ ] **Step 3: No final commit**

Phase 1 ends with the four commits from Tasks 1–4. Do not tag, do not push, do not edit `ROADMAP.md`. Phase 2 owns the roadmap check-off.

---

## Changes Introduced

**New files:**
- `syntax/modifier.go` — `ModifierSet`, `DefaultModifiers()`, `(m ModifierSet) Has(byte) bool`, `(m ModifierSet) With(byte) ModifierSet`.
- `syntax/modifier_test.go` — registry unit tests.

**Modified interfaces:**
- `syntax.Token` — new public field `Modifier byte`. Additive; zero-valued for every token produced by `Lex` in this phase.
- `syntax.FieldFilter` — new public field `Modifier byte`. Additive; zero-valued for every field produced by `filter.Parse` / `filter.ParseExpr` in this phase, because they do not yet set it.
- New exported function `syntax.LexWithModifiers(input string, modifiers ModifierSet) ([]Token, []ParseError)`. `syntax.Lex(input)` is retained as a wrapper that calls `LexWithModifiers(input, DefaultModifiers())`. Both return the same tokens `main`'s `Lex` does today.

**Bridge code:**
- `LexWithModifiers` accepts a `ModifierSet` parameter that is **intentionally unused** inside the body this phase. The `_ = modifiers` discard plus the doc comment explicitly flag this as bridge code. **Removal target: Phase 2, Task 1**, which rewrites the body to consult the registry when scanning each token's first character.

**Environment variables, schema migrations, dependencies:** none.

**Roadmap:** not touched. Phase 2 ticks the `ROADMAP.md` boxes.
