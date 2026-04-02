package filter

import "fmt"

// TokenType classifies a lexed token.
type TokenType int

const (
	TokenField      TokenType = iota // key:value
	TokenTagInclude                  // +word
	TokenTagExclude                  // -word
	TokenText                        // anything else
)

func (t TokenType) String() string {
	switch t {
	case TokenField:
		return "Field"
	case TokenTagInclude:
		return "TagInclude"
	case TokenTagExclude:
		return "TagExclude"
	case TokenText:
		return "Text"
	default:
		return "Unknown"
	}
}

// Token is a single lexed element from a filter input string.
type Token struct {
	Type  TokenType
	Value string // raw text of the token
	Pos   int    // byte offset in the original input
}

// Lex splits the input string into tokens. It returns all tokens it could
// produce plus any errors encountered (e.g., bare +/- signs). Processing
// continues past errors so all issues are reported in one pass.
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

		// Find the end of this token (next whitespace or end of input)
		start := i
		for i < len(input) && input[i] != ' ' && input[i] != '\t' {
			i++
		}
		raw := input[start:i]

		// Classify the token
		switch {
		case len(raw) == 1 && (raw[0] == '+' || raw[0] == '-'):
			errs = append(errs, ParseError{
				Pos:     start,
				Message: fmt.Sprintf("bare %q is not a valid token; use %s<name> for tags", raw, raw),
			})

		case raw[0] == '+':
			tokens = append(tokens, Token{Type: TokenTagInclude, Value: raw, Pos: start})

		case raw[0] == '-' && !isFieldToken(raw):
			tokens = append(tokens, Token{Type: TokenTagExclude, Value: raw, Pos: start})

		case isFieldToken(raw):
			tokens = append(tokens, Token{Type: TokenField, Value: raw, Pos: start})

		default:
			tokens = append(tokens, Token{Type: TokenText, Value: raw, Pos: start})
		}
	}

	return tokens, errs
}

// isFieldToken returns true if the raw token contains a colon and has a
// non-empty key (i.e., it's not just ":value").
func isFieldToken(raw string) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] == ':' {
			return i > 0
		}
	}
	return false
}
