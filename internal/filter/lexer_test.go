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
