package syntax

import (
	"fmt"
	"strings"
)

// ParseError represents a single issue found during parsing or validation.
type ParseError struct {
	Pos     int    // byte offset in input (-1 if not applicable)
	Field   string // field name, if relevant
	Message string // human-readable description
}

func (e ParseError) Error() string {
	var b strings.Builder
	if e.Pos >= 0 {
		fmt.Fprintf(&b, "filter error at position %d: ", e.Pos)
	} else {
		b.WriteString("filter error: ")
	}
	if e.Field != "" {
		fmt.Fprintf(&b, "field %q: ", e.Field)
	}
	b.WriteString(e.Message)
	return b.String()
}

// FormatErrors joins multiple ParseErrors into a newline-separated string.
func FormatErrors(errs []ParseError) string {
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "\n")
}
