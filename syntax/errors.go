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

func (parseErr ParseError) Error() string {
	var builder strings.Builder
	if parseErr.Pos >= 0 {
		fmt.Fprintf(&builder, "filter error at position %d: ", parseErr.Pos)
	} else {
		builder.WriteString("filter error: ")
	}
	if parseErr.Field != "" {
		fmt.Fprintf(&builder, "field %q: ", parseErr.Field)
	}
	builder.WriteString(parseErr.Message)
	return builder.String()
}

// FormatErrors joins multiple ParseErrors into a newline-separated string.
func FormatErrors(errs []ParseError) string {
	msgs := make([]string, len(errs))
	for index, item := range errs {
		msgs[index] = item.Error()
	}
	return strings.Join(msgs, "\n")
}
