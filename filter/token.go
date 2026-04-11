package filter

import "github.com/germanamz/tusk/syntax"

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

// Lex delegates to the shared syntax lexer.
func Lex(input string) ([]Token, []ParseError) {
	return syntax.Lex(input)
}
