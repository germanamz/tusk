package syntax

import "testing"

func TestParseError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  ParseError
		want string
	}{
		{
			name: "with position and field",
			err:  ParseError{Pos: 5, Field: "status", Message: "unknown field"},
			want: `filter error at position 5: field "status": unknown field`,
		},
		{
			name: "with position no field",
			err:  ParseError{Pos: 0, Message: "bare \"+\" is not a valid token"},
			want: `filter error at position 0: bare "+" is not a valid token`,
		},
		{
			name: "negative position",
			err:  ParseError{Pos: -1, Message: "something went wrong"},
			want: "filter error: something went wrong",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.err.Error()
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatErrors(t *testing.T) {
	errs := []ParseError{
		{Pos: 0, Message: "first error"},
		{Pos: 10, Field: "due", Message: "invalid date"},
	}
	got := FormatErrors(errs)
	want := "filter error at position 0: first error\nfilter error at position 10: field \"due\": invalid date"
	if got != want {
		t.Errorf("FormatErrors:\ngot:  %q\nwant: %q", got, want)
	}
}
