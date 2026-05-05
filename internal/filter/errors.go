package filter

import "fmt"

// ParseError reports a single syntactic problem with a position and a message.
type ParseError struct {
	Pos     int
	Message string
}

func (parseErr *ParseError) Error() string {
	return fmt.Sprintf("filter: %s at column %d", parseErr.Message, parseErr.Pos+1)
}
