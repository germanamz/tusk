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
// (e.g. '+', '-') is stripped off the token body. In Phase 1 of the modifier
// initiative Modifier is always 0 — the lexer does not strip prefixes yet —
// but the field is present so downstream consumers can compile against it.
type Token struct {
	Type     TokenType
	Value    string // raw text of the token
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
// Phase 1: the modifiers parameter is accepted but not yet consulted inside
// the body — behavior is identical to the legacy Lex. Phase 2 of the modifier
// AST initiative will make the body read the registry when scanning each
// token's first character.
func LexWithModifiers(input string, modifiers ModifierSet) ([]Token, []ParseError) {
	_ = modifiers // intentionally unused in Phase 1; Phase 2 wires it in
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

		// Parentheses preceded by whitespace (or at start of input) are boolean grouping
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

		// Scan a token: characters until whitespace.
		// Quotes mid-token (key="value") are inlined.
		// ( immediately after prior chars is a group modifier (tracked via parenDepth).
		// ) at depth 0 is a boolean close — stop.
		var buf []byte
		unclosedQuote := false
		parenDepth := 0
		for i < len(input) && input[i] != ' ' && input[i] != '\t' {
			if input[i] == ')' && parenDepth == 0 {
				break
			}
			if input[i] == '(' {
				parenDepth++
				buf = append(buf, input[i])
				i++
				continue
			}
			if input[i] == ')' && parenDepth > 0 {
				parenDepth--
				buf = append(buf, input[i])
				i++
				continue
			}
			if input[i] == '"' {
				content, end, err := scanQuoted(input, i)
				if err != nil {
					errs = append(errs, ParseError{Pos: i, Message: err.Error()})
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

		raw = string(buf)

		if raw == "" {
			continue
		}

		// Classify the token
		switch {
		case len(raw) == 1 && (raw[0] == '+' || raw[0] == '-'):
			errs = append(errs, ParseError{
				Pos:     start,
				Message: fmt.Sprintf("bare %q is not a valid token; use %s<name> for tags", raw, raw),
			})

		case raw[0] == '+':
			if hasEquals(raw[1:]) {
				tokens = append(tokens, Token{Type: TokenField, Value: raw, Pos: start})
			} else {
				tokens = append(tokens, Token{Type: TokenTagInclude, Value: raw, Pos: start})
			}

		case raw[0] == '-':
			if hasEquals(raw[1:]) {
				tokens = append(tokens, Token{Type: TokenField, Value: raw, Pos: start})
			} else {
				tokens = append(tokens, Token{Type: TokenTagExclude, Value: raw, Pos: start})
			}

		case isFieldToken(raw):
			tokens = append(tokens, Token{Type: TokenField, Value: raw, Pos: start})

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

// isFieldToken returns true if raw contains = with a non-empty key.
// Tokens starting with + or - are handled separately by the caller.
func isFieldToken(raw string) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] == '=' {
			return i > 0
		}
	}
	return false
}

// hasEquals returns true if s contains at least one '=' character.
func hasEquals(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return true
		}
	}
	return false
}
