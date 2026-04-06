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
			input: "status:active   +api",
			want: []Token{
				{Type: TokenField, Value: "status:active", Pos: 0},
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
				{Type: TokenField, Value: "status:active", Pos: 15},
			},
		},
		{
			name:  "field with quoted value containing colon",
			input: `title:"step 1: do things"`,
			want: []Token{
				{Type: TokenField, Value: `title:step 1: do things`, Pos: 0},
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
