package filter_test

import (
	"fmt"
	"testing"

	"github.com/germanamz/tusk/internal/filter"
)

// TestErrors_FormatByValue pins that both filter error types read as a
// sentence when formatted by value, which is how the query callers print the
// first error (#760).
func TestErrors_FormatByValue(test *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			"parse error",
			filter.ParseError{Pos: 13, Message: "expected inner predicate or end of edge predicate"},
			"expected inner predicate or end of edge predicate at column 14",
		},
		{
			"validation error",
			filter.ValidationError{Pos: 0, Message: `edge type "nope" not declared in manifest`},
			`edge type "nope" not declared in manifest at column 1`,
		},
		{
			"validation error with hint",
			filter.ValidationError{Pos: 4, Message: "unknown hierarchy alias", Hint: "declared: wbs"},
			"unknown hierarchy alias at column 5 (declared: wbs)",
		},
	}

	for _, testCase := range cases {
		if got := fmt.Sprintf("%v", testCase.err); got != testCase.want {
			test.Errorf("%s: got %q, want %q", testCase.name, got, testCase.want)
		}
	}
}
