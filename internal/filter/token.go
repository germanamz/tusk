package filter

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
