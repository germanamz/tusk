package syntax

import "testing"

func TestTokenType_String(test *testing.T) {
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
	for _, testCase := range tests {
		got := testCase.tt.String()
		if got != testCase.want {
			test.Fatalf("TokenType(%d).String() = %q, want %q", testCase.tt, got, testCase.want)
		}
	}
}

func TestLex(test *testing.T) {
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
				{Type: TokenTagInclude, Value: "api", Modifier: '+', Pos: 0},
				{Type: TokenTagInclude, Value: "frontend", Modifier: '+', Pos: 5},
			},
		},
		{
			name:  "tag exclude",
			input: "-docs -wip",
			want: []Token{
				{Type: TokenTagExclude, Value: "docs", Modifier: '-', Pos: 0},
				{Type: TokenTagExclude, Value: "wip", Modifier: '-', Pos: 6},
			},
		},
		{
			name:  "mixed input",
			input: "My task project=backend +api -docs priority=3",
			want: []Token{
				{Type: TokenText, Value: "My", Pos: 0},
				{Type: TokenText, Value: "task", Pos: 3},
				{Type: TokenField, Value: "project=backend", Pos: 8},
				{Type: TokenTagInclude, Value: "api", Modifier: '+', Pos: 24},
				{Type: TokenTagExclude, Value: "docs", Modifier: '-', Pos: 29},
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
				{Type: TokenTagInclude, Value: "api", Modifier: '+', Pos: 16},
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
				{Type: TokenTagInclude, Value: "api", Modifier: '+', Pos: 34},
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
				{Type: TokenTagInclude, Value: "api", Modifier: '+', Pos: 18},
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
				{Type: TokenTagInclude, Value: "urgent", Modifier: '+', Pos: 18},
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
		test.Run(tt.name, func(test *testing.T) {
			tokens, errs := Lex(tt.input)
			if len(errs) != tt.errors {
				test.Fatalf("Lex(%q) returned %d errors, want %d: %v", tt.input, len(errs), tt.errors, errs)
			}
			if len(tokens) != len(tt.want) {
				test.Fatalf("Lex(%q) returned %d tokens, want %d:\ngot:  %+v\nwant: %+v",
					tt.input, len(tokens), len(tt.want), tokens, tt.want)
			}
			for index, tok := range tokens {
				if tok.Type != tt.want[index].Type {
					test.Errorf("token[%d].Type = %v, want %v", index, tok.Type, tt.want[index].Type)
				}
				if tok.Value != tt.want[index].Value {
					test.Errorf("token[%d].Value = %q, want %q", index, tok.Value, tt.want[index].Value)
				}
				if tok.Modifier != tt.want[index].Modifier {
					test.Errorf("token[%d].Modifier = %q, want %q", index, tok.Modifier, tt.want[index].Modifier)
				}
				if tok.Pos != tt.want[index].Pos {
					test.Errorf("token[%d].Pos = %d, want %d", index, tok.Pos, tt.want[index].Pos)
				}
			}
		})
	}
}

func TestLex_EdgeCases(test *testing.T) {
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
				{Type: TokenTagInclude, Value: "v2", Modifier: '+', Pos: 0},
				{Type: TokenTagExclude, Value: "v1", Modifier: '-', Pos: 4},
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
				{Type: TokenField, Value: "status=review", Modifier: '+', Pos: 0},
			},
		},
		{
			name:  "subtractive modifier with field",
			input: "-status=review",
			want: []Token{
				{Type: TokenField, Value: "status=review", Modifier: '-', Pos: 0},
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
				{Type: TokenField, Value: "status=active(start,highlight)", Pos: 24},
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
				{Type: TokenTagInclude, Value: "api:test", Modifier: '+', Pos: 0},
			},
		},
	}

	for _, tt := range tests {
		test.Run(tt.name, func(test *testing.T) {
			tokens, errs := Lex(tt.input)
			if len(errs) != tt.errors {
				test.Fatalf("Lex(%q) returned %d errors, want %d: %v", tt.input, len(errs), tt.errors, errs)
			}
			if len(tokens) != len(tt.want) {
				test.Fatalf("Lex(%q) returned %d tokens, want %d:\ngot:  %+v\nwant: %+v",
					tt.input, len(tokens), len(tt.want), tokens, tt.want)
			}
			for index, tok := range tokens {
				if tok.Type != tt.want[index].Type {
					test.Errorf("token[%d].Type = %v, want %v", index, tok.Type, tt.want[index].Type)
				}
				if tok.Value != tt.want[index].Value {
					test.Errorf("token[%d].Value = %q, want %q", index, tok.Value, tt.want[index].Value)
				}
				if tok.Modifier != tt.want[index].Modifier {
					test.Errorf("token[%d].Modifier = %q, want %q", index, tok.Modifier, tt.want[index].Modifier)
				}
				if tok.Pos != tt.want[index].Pos {
					test.Errorf("token[%d].Pos = %d, want %d", index, tok.Pos, tt.want[index].Pos)
				}
			}
		})
	}
}

func TestLexWithModifiersDelegatesWithDefaultRegistry(test *testing.T) {
	input := "status=active +urgent -blocked title=\"hello\""
	defaultTokens, _ := Lex(input)
	withTokens, _ := LexWithModifiers(input, DefaultModifiers())

	if len(defaultTokens) != len(withTokens) {
		test.Fatalf("token count differs: default=%d, with=%d", len(defaultTokens), len(withTokens))
	}
	for index := range defaultTokens {
		if defaultTokens[index] != withTokens[index] {
			test.Errorf("tokens[%d] differ: default=%+v, with=%+v", index, defaultTokens[index], withTokens[index])
		}
	}
}

func TestLexStripsPlusModifierFromFieldToken(test *testing.T) {
	tokens, errs := Lex("+priority=3")
	if len(errs) > 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if len(tokens) != 1 {
		test.Fatalf("expected 1 token, got %d", len(tokens))
	}
	tok := tokens[0]
	if tok.Type != TokenField {
		test.Errorf("type = %v, want TokenField", tok.Type)
	}
	if tok.Modifier != '+' {
		test.Errorf("modifier = %q, want '+'", tok.Modifier)
	}
	if tok.Value != "priority=3" {
		test.Errorf("value = %q, want %q", tok.Value, "priority=3")
	}
}

func TestLexStripsMinusModifierFromFieldToken(test *testing.T) {
	tokens, _ := Lex("-priority=3")
	if len(tokens) != 1 || tokens[0].Modifier != '-' || tokens[0].Value != "priority=3" {
		test.Fatalf("unexpected tokens: %+v", tokens)
	}
}

func TestLexBareFieldHasZeroModifier(test *testing.T) {
	tokens, _ := Lex("status=active")
	if len(tokens) != 1 || tokens[0].Modifier != 0 || tokens[0].Value != "status=active" {
		test.Fatalf("unexpected tokens: %+v", tokens)
	}
}

func TestLexStripsPlusModifierFromTagToken(test *testing.T) {
	tokens, errs := Lex("+urgent")
	if len(errs) > 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if len(tokens) != 1 {
		test.Fatalf("expected 1 token, got %d", len(tokens))
	}
	tok := tokens[0]
	if tok.Type != TokenTagInclude {
		test.Errorf("type = %v, want TokenTagInclude", tok.Type)
	}
	if tok.Modifier != '+' {
		test.Errorf("modifier = %q, want '+'", tok.Modifier)
	}
	if tok.Value != "urgent" {
		test.Errorf("value = %q, want %q", tok.Value, "urgent")
	}
}

func TestLexStripsMinusModifierFromTagToken(test *testing.T) {
	tokens, _ := Lex("-blocked")
	if len(tokens) != 1 {
		test.Fatalf("expected 1 token, got %d", len(tokens))
	}
	if tokens[0].Type != TokenTagExclude || tokens[0].Modifier != '-' || tokens[0].Value != "blocked" {
		test.Fatalf("unexpected token: %+v", tokens[0])
	}
}
