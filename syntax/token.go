package syntax

import "fmt"

// TokenType classifies a lexed token.
type TokenType int

const (
	TokenField      TokenType = iota // key=value
	TokenTagInclude                  // +word
	TokenTagExclude                  // -word
	TokenText                        // anything else
	TokenAnd                         // AND
	TokenOr                          // OR
	TokenNot                         // NOT
	TokenLParen                      // (
	TokenRParen                      // )
)

func (tokenType TokenType) String() string {
	switch tokenType {
	case TokenField:
		return "Field"
	case TokenTagInclude:
		return "TagInclude"
	case TokenTagExclude:
		return "TagExclude"
	case TokenText:
		return "Text"
	case TokenAnd:
		return "And"
	case TokenOr:
		return "Or"
	case TokenNot:
		return "Not"
	case TokenLParen:
		return "LParen"
	case TokenRParen:
		return "RParen"
	default:
		return "Unknown"
	}
}

// Token is a single lexed element from an input string.
//
// Modifier is populated by LexWithModifiers when a registered prefix rune
// (e.g. '+', '-') is stripped off the token body; 0 means no modifier.
type Token struct {
	Type     TokenType
	Value    string // raw text of the token, with any registered prefix stripped
	Modifier byte   // registered prefix marker ('+' / '-' / ...); 0 if none
	Pos      int    // byte offset in the original input
}

// Lex splits the input string into tokens using the default modifier registry.
// See LexWithModifiers for the full lexing behavior.
func Lex(input string) ([]Token, []ParseError) {
	return LexWithModifiers(input, DefaultModifiers())
}

// LexWithModifiers splits the input string into tokens using = as the field
// separator and the given modifier registry for recognised prefix markers.
// Modifiers (,  :  ..  ()) inside field values are preserved as part of the
// raw value — the lexer does not decompose them into sub-tokens.
// Quoted strings are opaque: no modifier tokenization inside quotes.
// Parentheses immediately after a value (no whitespace) are part of the value
// (group modifier); parentheses preceded by whitespace are boolean grouping.
//
// Returns all tokens produced plus any errors encountered. Processing
// continues past errors so all issues are reported in one pass.
//
// Registered prefix bytes present in modifiers are stripped from the start of
// a token and surfaced in Token.Modifier. The stripped body is what drives
// field/tag classification: `+priority=3` becomes a field token with
// Value="priority=3" and Modifier='+', while `+urgent` becomes a tag include
// with Value="urgent" and Modifier='+'.
func LexWithModifiers(input string, modifiers ModifierSet) ([]Token, []ParseError) {
	var tokens []Token
	var errs []ParseError

	pos := 0
	for pos < len(input) {
		// Skip whitespace
		if input[pos] == ' ' || input[pos] == '\t' {
			pos++
			continue
		}

		start := pos
		var raw string

		if input[pos] == '"' {
			// Standalone quoted string: "some text"
			content, end, err := scanQuoted(input, pos)

			if err != nil {
				errs = append(errs, ParseError{Pos: start, Message: err.Error()})
				break // unclosed quote — can't continue scanning
			}

			pos = end
			raw = content
			tokens = append(tokens, Token{Type: TokenText, Value: raw, Pos: start})
			continue
		}

		// Parentheses preceded by whitespace (or at start of input) are boolean grouping
		if input[pos] == '(' {
			tokens = append(tokens, Token{Type: TokenLParen, Value: "(", Pos: pos})
			pos++
			continue
		}
		if input[pos] == ')' {
			tokens = append(tokens, Token{Type: TokenRParen, Value: ")", Pos: pos})
			pos++
			continue
		}

		// Scan a token: characters until whitespace.
		// Quotes mid-token (key="value") are inlined.
		// ( immediately after prior chars is a group modifier (tracked via parenDepth).
		// ) at depth 0 is a boolean close — stop.
		var buf []byte
		unclosedQuote := false
		parenDepth := 0
		for pos < len(input) && input[pos] != ' ' && input[pos] != '\t' {
			if input[pos] == ')' && parenDepth == 0 {
				break
			}
			if input[pos] == '(' {
				parenDepth++
				buf = append(buf, input[pos])
				pos++
				continue
			}
			if input[pos] == ')' && parenDepth > 0 {
				parenDepth--
				buf = append(buf, input[pos])
				pos++
				continue
			}
			if input[pos] == '"' {
				content, end, err := scanQuoted(input, pos)

				if err != nil {
					errs = append(errs, ParseError{Pos: pos, Message: err.Error()})
					unclosedQuote = true
					break
				}

				buf = append(buf, content...)
				pos = end
				continue
			}
			buf = append(buf, input[pos])
			pos++
		}
		if unclosedQuote {
			break
		}

		raw = string(buf)

		if raw == "" {
			continue
		}

		// Classify the token
		var modifier byte
		body := raw

		if len(raw) >= 2 && modifiers.Has(raw[0]) {
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
	}

	return tokens, errs
}

// scanQuoted reads a quoted string starting at input[pos] (which must be '"').
// Returns the unescaped content (without surrounding quotes), the byte index
// immediately after the closing quote, and any error (unclosed quote).
// Supports \" as an escaped literal quote inside the string.
func scanQuoted(input string, pos int) (string, int, error) {
	cursor := pos + 1
	var buf []byte
	for cursor < len(input) {
		if input[cursor] == '\\' && cursor+1 < len(input) && input[cursor+1] == '"' {
			buf = append(buf, '"')
			cursor += 2
			continue
		}
		if input[cursor] == '"' {
			return string(buf), cursor + 1, nil
		}
		buf = append(buf, input[cursor])
		cursor++
	}
	return "", pos, fmt.Errorf("unclosed quoted string")
}

// isFieldToken returns true if raw contains = with a non-empty key.
// Tokens starting with + or - are handled separately by the caller.
func isFieldToken(raw string) bool {
	for index := 0; index < len(raw); index++ {
		if raw[index] == '=' {
			return index > 0
		}
	}
	return false
}
