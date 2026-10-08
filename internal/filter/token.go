// Package filter implements the structural filter grammar for tusk query
// and tusk node list. Pipeline: input string → Lexer → Parser → AST →
// Validator (against manifest) → Compiler → SQL.
package filter

// TokenKind classifies a lexer Token.
type TokenKind int

const (
	TokenEOF TokenKind = iota
	TokenIdent
	TokenString
	TokenBareValue
	TokenEQ
	TokenColon
	TokenNE
	TokenLT
	TokenLE
	TokenGT
	TokenGE
	TokenArrowOut
	TokenArrowIn
	TokenDotDot
	TokenLParen
	TokenRParen
	TokenAnd
	TokenOr
	TokenNot
	// TokenIllegal is input the lexer can't tokenize: a character outside the
	// grammar or a string with no closing quote. Its Value describes the
	// problem, ready to report as the parse error.
	TokenIllegal
)

// Token is one unit produced by the lexer.
type Token struct {
	Kind  TokenKind
	Value string
	Pos   int
}
