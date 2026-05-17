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
		{":", []filter.TokenKind{filter.TokenEQ, filter.TokenEOF}},
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
