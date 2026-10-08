package filter

import "fmt"

// ParseError reports a single syntactic problem with a position and a message.
type ParseError struct {
	Pos     int
	Message string
}

// Error has a value receiver so the ParseError values Parse returns format as
// the message, not as the bare struct. Callers add their own "filter parse:"
// prefix.
func (parseErr ParseError) Error() string {
	return fmt.Sprintf("%s at column %d", parseErr.Message, parseErr.Pos+1)
}
