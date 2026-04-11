package filter

import (
	"fmt"

	"github.com/germanamz/tusk/syntax"
)

// Re-export token types from syntax package.
type TokenType = syntax.TokenType
type Token = syntax.Token

const (
	TokenField      = syntax.TokenField
	TokenTagInclude = syntax.TokenTagInclude
	TokenTagExclude = syntax.TokenTagExclude
	TokenText       = syntax.TokenText
	TokenAnd        = syntax.TokenAnd
	TokenOr         = syntax.TokenOr
	TokenNot        = syntax.TokenNot
	TokenLParen     = syntax.TokenLParen
	TokenRParen     = syntax.TokenRParen
)

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
			raw = content
			tokens = append(tokens, Token{Type: TokenText, Value: raw, Pos: start})
			continue
		}

		// Parentheses are always single-character tokens
		if input[i] == '(' {
			tokens = append(tokens, Token{Type: TokenLParen, Value: "(", Pos: i})
			i++
			continue
		}
		if input[i] == ')' {
			tokens = append(tokens, Token{Type: TokenRParen, Value: ")", Pos: i})
			i++
			continue
		}

		// Scan unquoted portion until whitespace
		// But if we encounter a quote mid-token (e.g. key:"value"), handle it
		var buf []byte
		unclosedQuote := false
		for i < len(input) && input[i] != ' ' && input[i] != '\t' && input[i] != '(' && input[i] != ')' {
			if input[i] == '"' {
				// Quote inside a token: key:"value with spaces"
				content, end, err := scanQuoted(input, i)
				if err != nil {
					errs = append(errs, ParseError{Pos: i, Message: err.Error()})
					raw = string(buf)
					unclosedQuote = true
					break
				}
				buf = append(buf, content...)
				i = end
				continue
			}
			buf = append(buf, input[i])
			i++
		}
		if unclosedQuote {
			break
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

		case raw == "AND":
			tokens = append(tokens, Token{Type: TokenAnd, Value: raw, Pos: start})

		case raw == "OR":
			tokens = append(tokens, Token{Type: TokenOr, Value: raw, Pos: start})

		case raw == "NOT":
			tokens = append(tokens, Token{Type: TokenNot, Value: raw, Pos: start})

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

// isFieldToken returns true if the raw token contains a field separator
// with a non-empty key.
// BRIDGE: accepts both = (new) and : (legacy). Remove : check in Phase 3.
func isFieldToken(raw string) bool {
	// Check = first (new syntax)
	for i := 0; i < len(raw); i++ {
		if raw[i] == '=' {
			return i > 0
		}
	}
	// BRIDGE: also accept : (legacy syntax)
	for i := 0; i < len(raw); i++ {
		if raw[i] == ':' {
			return i > 0
		}
	}
	return false
}
