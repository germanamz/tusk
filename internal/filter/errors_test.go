package filter

import (
	"testing"
)

func TestParseError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  ParseError
		want string
	}{
		{
			name: "with field and position",
			err:  ParseError{Pos: 10, Field: "priority", Message: "expected 0-4"},
			want: `filter error at position 10: field "priority": expected 0-4`,
		},
		{
			name: "with position, no field",
			err:  ParseError{Pos: 5, Field: "", Message: "unexpected token"},
			want: `filter error at position 5: unexpected token`,
		},
		{
			name: "no position",
			err:  ParseError{Pos: -1, Field: "status", Message: "empty value"},
			want: `filter error: field "status": empty value`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatErrors(t *testing.T) {
	errs := []ParseError{
		{Pos: 0, Field: "foo", Message: "unknown field"},
		{Pos: 10, Field: "priority", Message: "invalid value"},
	}
	got := FormatErrors(errs)
	want := "filter error at position 0: field \"foo\": unknown field\nfilter error at position 10: field \"priority\": invalid value"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
