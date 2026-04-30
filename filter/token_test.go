package filter

import (
	"testing"
)

func TestTokenType_String(test *testing.T) {
	cases := []struct {
		tokenType TokenType
		want      string
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
	for _, testCase := range cases {
		got := testCase.tokenType.String()
		if got != testCase.want {
			test.Fatalf("TokenType(%d).String() = %q, want %q", testCase.tokenType, got, testCase.want)
		}
	}
}

func TestLex(test *testing.T) {
	cases := []struct {
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
			name:  "field key=value",
			input: "status=active",
			want: []Token{
				{Type: TokenField, Value: "status=active", Pos: 0},
			},
		},
		{
			name:  "field with colon in value",
			input: "due=2026-04-10T15:30:00Z",
			want: []Token{
				{Type: TokenField, Value: "due=2026-04-10T15:30:00Z", Pos: 0},
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
			name:  "quoted text with existing tokens",
			input: `"My cool task" project=backend +api`,
			want: []Token{
				{Type: TokenText, Value: "My cool task", Pos: 0},
				{Type: TokenField, Value: "project=backend", Pos: 15},
				{Type: TokenTagInclude, Value: "api", Modifier: '+', Pos: 31},
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
			name:  "parentheses",
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
			input: "(status=active)",
			want: []Token{
				{Type: TokenLParen, Value: "(", Pos: 0},
				{Type: TokenField, Value: "status=active", Pos: 1},
				{Type: TokenRParen, Value: ")", Pos: 14},
			},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			tokens, errs := Lex(testCase.input)
			if len(errs) != testCase.errors {
				test.Fatalf("Lex(%q) returned %d errors, want %d: %v", testCase.input, len(errs), testCase.errors, errs)
			}
			if len(tokens) != len(testCase.want) {
				test.Fatalf("Lex(%q) returned %d tokens, want %d:\ngot:  %+v\nwant: %+v",
					testCase.input, len(tokens), len(testCase.want), tokens, testCase.want)
			}
			for index, tok := range tokens {
				if tok.Type != testCase.want[index].Type {
					test.Errorf("token[%d].Type = %v, want %v", index, tok.Type, testCase.want[index].Type)
				}
				if tok.Value != testCase.want[index].Value {
					test.Errorf("token[%d].Value = %q, want %q", index, tok.Value, testCase.want[index].Value)
				}
				if tok.Modifier != testCase.want[index].Modifier {
					test.Errorf("token[%d].Modifier = %q, want %q", index, tok.Modifier, testCase.want[index].Modifier)
				}
				if tok.Pos != testCase.want[index].Pos {
					test.Errorf("token[%d].Pos = %d, want %d", index, tok.Pos, testCase.want[index].Pos)
				}
			}
		})
	}
}

func TestLex_EdgeCases(test *testing.T) {
	cases := []struct {
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
			name:  "additive modifier field",
			input: "+api=test",
			want: []Token{
				{Type: TokenField, Value: "api=test", Modifier: '+', Pos: 0},
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
			name:  "priority range is a field",
			input: "priority=2..4",
			want: []Token{
				{Type: TokenField, Value: "priority=2..4", Pos: 0},
			},
		},
		{
			name:  "due date range is a field",
			input: "due=today..friday",
			want: []Token{
				{Type: TokenField, Value: "due=today..friday", Pos: 0},
			},
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
			name:  "adjacent quoted and unquoted",
			input: `+api "my task" status=active`,
			want: []Token{
				{Type: TokenTagInclude, Value: "api", Modifier: '+', Pos: 0},
				{Type: TokenText, Value: "my task", Pos: 5},
				{Type: TokenField, Value: "status=active", Pos: 15},
			},
		},
		{
			name:  "field with quoted value containing colon",
			input: `title="step 1: do things"`,
			want: []Token{
				{Type: TokenField, Value: `title=step 1: do things`, Pos: 0},
			},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			tokens, errs := Lex(testCase.input)
			if len(errs) != testCase.errors {
				test.Fatalf("Lex(%q) returned %d errors, want %d: %v", testCase.input, len(errs), testCase.errors, errs)
			}
			if len(tokens) != len(testCase.want) {
				test.Fatalf("Lex(%q) returned %d tokens, want %d:\ngot:  %+v\nwant: %+v",
					testCase.input, len(tokens), len(testCase.want), tokens, testCase.want)
			}
			for index, tok := range tokens {
				if tok.Type != testCase.want[index].Type {
					test.Errorf("token[%d].Type = %v, want %v", index, tok.Type, testCase.want[index].Type)
				}
				if tok.Value != testCase.want[index].Value {
					test.Errorf("token[%d].Value = %q, want %q", index, tok.Value, testCase.want[index].Value)
				}
				if tok.Modifier != testCase.want[index].Modifier {
					test.Errorf("token[%d].Modifier = %q, want %q", index, tok.Modifier, testCase.want[index].Modifier)
				}
				if tok.Pos != testCase.want[index].Pos {
					test.Errorf("token[%d].Pos = %d, want %d", index, tok.Pos, testCase.want[index].Pos)
				}
			}
		})
	}
}
