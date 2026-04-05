# Phase 1: Quoted-Aware Lexer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the whitespace-split lexer with a character-by-character scanner that supports quoted strings, enabling multi-word values like `title:"fix the bug"` and `"multi word text"`.

**Architecture:** The `Lex()` function in `internal/filter/token.go` is rewritten from a split-on-whitespace approach to a character scanner that tracks quoted state. All existing token types and classification logic are preserved. The scanner recognizes `"` as a quote delimiter, consuming all bytes (including whitespace) until the closing `"`. Backslash-escaped quotes (`\"`) are supported. Unclosed quotes produce a `ParseError`.

**Tech Stack:** Go standard library only (no new dependencies).

**Prerequisites:** None — this phase operates on the base codebase.

---

### Task 1: Write Failing Tests for Quoted Lexer Behavior

**Files:**
- Modify: `internal/filter/token_test.go`

- [ ] **Step 1: Add quoted string test cases to TestLex**

Add these test cases to the `tests` slice inside `TestLex` (after the existing "multiple spaces between tokens" case at line 113):

```go
{
    name:  "quoted text standalone",
    input: `"fix the bug"`,
    want: []Token{
        {Type: TokenText, Value: "fix the bug", Pos: 0},
    },
},
{
    name:  "quoted field value",
    input: `title:"fix the bug"`,
    want: []Token{
        {Type: TokenField, Value: `title:fix the bug`, Pos: 0},
    },
},
{
    name:  "mixed quoted and unquoted",
    input: `status:active title:"fix the bug" +api`,
    want: []Token{
        {Type: TokenField, Value: "status:active", Pos: 0},
        {Type: TokenField, Value: `title:fix the bug`, Pos: 14},
        {Type: TokenTagInclude, Value: "+api", Pos: 34},
    },
},
{
    name:  "escaped quote inside quoted string",
    input: `title:"say \"hello\""`,
    want: []Token{
        {Type: TokenField, Value: `title:say "hello"`, Pos: 0},
    },
},
{
    name:  "quoted text with existing tokens",
    input: `"My cool task" project:backend +api`,
    want: []Token{
        {Type: TokenText, Value: "My cool task", Pos: 0},
        {Type: TokenField, Value: "project:backend", Pos: 15},
        {Type: TokenTagInclude, Value: "+api", Pos: 31},
    },
},
```

- [ ] **Step 2: Add quoted string edge cases to TestLex_EdgeCases**

Add these test cases to the `tests` slice inside `TestLex_EdgeCases` (after the existing "due date range is a field" case at line 203):

```go
{
    name:   "unclosed quote",
    input:  `title:"fix the bug`,
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
    name:  "adjacent quoted and unquoted",
    input: `+api "my task" status:active`,
    want: []Token{
        {Type: TokenTagInclude, Value: "+api", Pos: 0},
        {Type: TokenText, Value: "my task", Pos: 5},
        {Type: TokenField, Value: "status:active", Pos: 14},
    },
},
{
    name:  "field with quoted value containing colon",
    input: `title:"step 1: do things"`,
    want: []Token{
        {Type: TokenField, Value: `title:step 1: do things`, Pos: 0},
    },
},
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/filter/ -run "TestLex$|TestLex_EdgeCases"`
Expected: FAIL — the current whitespace-splitting lexer does not handle quotes.

- [ ] **Step 4: Commit the failing tests**

```bash
git add internal/filter/token_test.go
git commit -m "$(cat <<'EOF'
test(filter): add failing tests for quoted string lexer

Add test cases for quoted standalone text, quoted field values,
escaped quotes, unclosed quotes, and edge cases. These fail against
the current whitespace-split lexer.
EOF
)"
```

---

### Task 2: Rewrite Lex() as a Character Scanner with Quote Support

**Files:**
- Modify: `internal/filter/token.go`

- [ ] **Step 1: Replace the Lex function body**

Replace the entire `Lex` function (lines 40-82 of `internal/filter/token.go`) with a character-by-character scanner:

```go
// Lex splits the input string into tokens. It returns all tokens it could
// produce plus any errors encountered (e.g., bare +/- signs, unclosed quotes).
// Processing continues past errors so all issues are reported in one pass.
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
			// Skip trailing whitespace to detect next token boundary
			raw = content
			tokens = append(tokens, Token{Type: TokenText, Value: raw, Pos: start})
			continue
		}

		// Scan unquoted portion until whitespace
		// But if we encounter a quote mid-token (e.g. key:"value"), handle it
		var buf []byte
		for i < len(input) && input[i] != ' ' && input[i] != '\t' {
			if input[i] == '"' {
				// Quote inside a token: key:"value with spaces"
				content, end, err := scanQuoted(input, i)
				if err != nil {
					errs = append(errs, ParseError{Pos: i, Message: err.Error()})
					// Attach what we have so far as the token
					raw = string(buf)
					break
				}
				buf = append(buf, content...)
				i = end
				// After closing quote, continue scanning non-whitespace
				continue
			}
			buf = append(buf, input[i])
			i++
		}

		if raw == "" {
			raw = string(buf)
		}

		if raw == "" {
			continue
		}

		// Classify the token (same logic as before)
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
	}

	return tokens, errs
}

// scanQuoted reads a quoted string starting at input[pos] (which must be '"').
// It returns the unescaped content (without surrounding quotes), the byte index
// immediately after the closing quote, and any error (unclosed quote).
// Supports \" as an escaped literal quote inside the string.
func scanQuoted(input string, pos int) (string, int, error) {
	// pos points at the opening "
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
```

- [ ] **Step 2: Run the full lexer test suite**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/filter/ -run "TestLex|TestTokenType"`
Expected: ALL PASS — both the new quoted tests and all existing tests.

- [ ] **Step 3: Run the full filter package tests to check for regressions**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/filter/`
Expected: ALL PASS.

- [ ] **Step 4: Run the full project tests**

Run: `cd /Users/germanamz/projects/tusk && make test`
Expected: ALL PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/filter/token.go
git commit -m "$(cat <<'EOF'
feat(filter): rewrite lexer as character scanner with quote support

Replace whitespace-split approach with byte-by-byte scanner that
handles "quoted strings" including inside field values (key:"value").
Supports escaped quotes (\") and reports unclosed quotes as errors.
EOF
)"
```

---

### Task 3: Update TokenType.String() for New Token Types and Verify Existing Token Tests

**Files:**
- Modify: `internal/filter/token_test.go`

This is a verification task. The existing tests should all pass after the rewrite. If any positions shifted due to the scanner change, fix them.

- [ ] **Step 1: Run E2E tests to verify no regressions**

Run: `cd /Users/germanamz/projects/tusk && make test-e2e`
Expected: ALL PASS.

- [ ] **Step 2: Run all tests with race detector**

Run: `cd /Users/germanamz/projects/tusk && make test-race`
Expected: ALL PASS.

- [ ] **Step 3: Commit if any test fixes were needed**

Only commit if tests required adjustments:
```bash
git add internal/filter/token_test.go
git commit -m "$(cat <<'EOF'
test(filter): fix lexer tests after character scanner rewrite
EOF
)"
```

---

### Task 4: Add Integration Test for Quoted Strings Through Parse Pipeline

**Files:**
- Modify: `internal/filter/parser_test.go`

- [ ] **Step 1: Add parser tests for quoted strings**

Add these test functions at the end of `internal/filter/parser_test.go`:

```go
func TestParse_QuotedTextTitle(t *testing.T) {
	fs, errs := Parse(`"My complex task"`)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if fs.Title() != "My complex task" {
		t.Fatalf("expected title %q, got %q", "My complex task", fs.Title())
	}
}

func TestParse_QuotedTextWithFields(t *testing.T) {
	fs, errs := Parse(`"My task" project:backend +api`)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if fs.Title() != "My task" {
		t.Fatalf("expected title %q, got %q", "My task", fs.Title())
	}
	if len(fs.Fields) != 1 || fs.Fields[0].Key != "project" {
		t.Fatalf("expected 1 field (project), got %+v", fs.Fields)
	}
	if len(fs.IncludeTags()) != 1 || fs.IncludeTags()[0] != "api" {
		t.Fatalf("expected include tags [api], got %v", fs.IncludeTags())
	}
}
```

- [ ] **Step 2: Run parser tests**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/filter/ -run "TestParse_Quoted"`
Expected: ALL PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/filter/parser_test.go
git commit -m "$(cat <<'EOF'
test(filter): add parser tests for quoted string tokens
EOF
)"
```

---

## Changes Introduced

**Modified files:**
- `internal/filter/token.go` — `Lex()` rewritten as character scanner; new `scanQuoted()` helper function
- `internal/filter/token_test.go` — New test cases for quoted strings, escaped quotes, unclosed quotes
- `internal/filter/parser_test.go` — New test functions for quoted text through Parse pipeline

**New functions:**
- `scanQuoted(input string, pos int) (string, int, error)` in `token.go`

**No new files, dependencies, migrations, or environment variables.**

**No bridge code introduced.**

**User-visible behaviors preserved:**
- All existing filter expressions produce identical tokens
- `tusk add My task project:backend +api` works as before
- `tusk list status:active +api -docs priority:3` works as before
- All E2E tests pass unchanged

**New user-visible behaviors:**
- `"quoted text"` as standalone produces a single `TokenText` with the full content
- `key:"quoted value"` produces a single `TokenField` with the quoted content as the value
- `\"` inside quotes is an escaped literal quote
- Unclosed `"` produces a parse error
