package filter

import "strings"

// Lexer tokenizes a filter expression.
type Lexer struct {
	input string
	pos   int
}

// NewLexer constructs a Lexer over input.
func NewLexer(input string) *Lexer {
	return &Lexer{input: input, pos: 0}
}

// Next returns the next operator/keyword/identifier token.
func (lex *Lexer) Next() Token {
	lex.skipWhitespace()

	if lex.pos >= len(lex.input) {
		return Token{Kind: TokenEOF, Pos: lex.pos}
	}

	startPos := lex.pos

	if op := lex.tryOperator(); op != nil {
		return Token{Kind: op.kind, Pos: startPos}
	}

	if isIdentStart(lex.input[lex.pos]) {
		end := lex.pos + 1

		for end < len(lex.input) && isIdentContinue(lex.input[end]) {
			// Stop before a '-' that is part of an arrow operator (-> or <-).
			if lex.input[end] == '-' && end+1 < len(lex.input) && lex.input[end+1] == '>' {
				break
			}

			end++
		}

		text := lex.input[lex.pos:end]
		lex.pos = end

		return Token{Kind: keywordOrIdent(text), Value: text, Pos: startPos}
	}

	if lex.input[lex.pos] == '"' || lex.input[lex.pos] == '\'' {
		return lex.lexString()
	}

	return Token{Kind: TokenEOF, Pos: startPos}
}

// NextValue returns a value-position token: STRING or BARE_VALUE.
func (lex *Lexer) NextValue() Token {
	lex.skipWhitespace()

	if lex.pos >= len(lex.input) {
		return Token{Kind: TokenEOF, Pos: lex.pos}
	}

	startPos := lex.pos

	if lex.input[lex.pos] == '"' || lex.input[lex.pos] == '\'' {
		return lex.lexString()
	}

	end := lex.pos

	for end < len(lex.input) {
		if end+1 < len(lex.input) && lex.input[end] == '.' && lex.input[end+1] == '.' {
			break
		}

		if !isBareValueChar(lex.input[end]) {
			break
		}

		end++
	}

	if end == lex.pos {
		return Token{Kind: TokenEOF, Pos: startPos}
	}

	text := lex.input[lex.pos:end]
	lex.pos = end

	return Token{Kind: TokenBareValue, Value: text, Pos: startPos}
}

// Pos reports the current byte offset.
func (lex *Lexer) Pos() int {
	return lex.pos
}

func (lex *Lexer) skipWhitespace() {
	for lex.pos < len(lex.input) {
		switch lex.input[lex.pos] {
		case ' ', '\t', '\n', '\r':
			lex.pos++
		default:
			return
		}
	}
}

type operatorMatch struct {
	literal string
	kind    TokenKind
}

var operators = []operatorMatch{
	{"!=", TokenNE},
	{"<=", TokenLE},
	{">=", TokenGE},
	{"->", TokenArrowOut},
	{"<-", TokenArrowIn},
	{"..", TokenDotDot},
	{"=", TokenEQ},
	{"<", TokenLT},
	{">", TokenGT},
	{"(", TokenLParen},
	{")", TokenRParen},
}

func (lex *Lexer) tryOperator() *operatorMatch {
	for _, op := range operators {
		if strings.HasPrefix(lex.input[lex.pos:], op.literal) {
			lex.pos += len(op.literal)

			return &op
		}
	}

	return nil
}

func (lex *Lexer) lexString() Token {
	startPos := lex.pos
	quote := lex.input[lex.pos]
	lex.pos++

	var builder strings.Builder

	for lex.pos < len(lex.input) {
		current := lex.input[lex.pos]

		if current == '\\' && lex.pos+1 < len(lex.input) {
			next := lex.input[lex.pos+1]

			switch next {
			case '\\', '"', '\'':
				builder.WriteByte(next)
				lex.pos += 2

				continue
			}

			builder.WriteByte(current)
			lex.pos++

			continue
		}

		if current == quote {
			lex.pos++

			return Token{Kind: TokenString, Value: builder.String(), Pos: startPos}
		}

		builder.WriteByte(current)
		lex.pos++
	}

	return Token{Kind: TokenEOF, Pos: startPos}
}

func isIdentStart(character byte) bool {
	return (character >= 'a' && character <= 'z') ||
		(character >= 'A' && character <= 'Z') ||
		character == '_'
}

func isIdentContinue(character byte) bool {
	return isIdentStart(character) ||
		(character >= '0' && character <= '9') ||
		character == '-'
}

func isBareValueChar(character byte) bool {
	return isIdentContinue(character) ||
		character == '/' ||
		character == '.' ||
		character == ':'
}

func keywordOrIdent(text string) TokenKind {
	switch strings.ToUpper(text) {
	case "AND":
		return TokenAnd
	case "OR":
		return TokenOr
	case "NOT":
		return TokenNot
	}

	return TokenIdent
}
