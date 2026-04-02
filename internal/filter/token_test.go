package filter

import (
	"testing"
)

func TestTokenType_String(t *testing.T) {
	tests := []struct {
		tt   TokenType
		want string
	}{
		{TokenField, "Field"},
		{TokenTagInclude, "TagInclude"},
		{TokenTagExclude, "TagExclude"},
		{TokenText, "Text"},
	}
	for _, tc := range tests {
		got := tc.tt.String()
		if got != tc.want {
			t.Fatalf("TokenType(%d).String() = %q, want %q", tc.tt, got, tc.want)
		}
	}
}
