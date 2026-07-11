package ignore_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/ignore"
)

// TestWithinBuiltinIgnore pins the write-surface guard (#686): paths inside the
// always-on built-in ignores (.tusk/, .git/) are reported ignored regardless of
// any .gitignore or workspace patterns, while ordinary node paths are not.
func TestWithinBuiltinIgnore(test *testing.T) {
	cases := []struct {
		path    string
		ignored bool
	}{
		{".tusk/evil.md", true},
		{".tusk/index.db", true},
		{".git/HEAD", true},
		{".git/objects/ab/cdef", true},
		{"notes/foo.md", false},
		{"foo.md", false},
		{"tusk.toml", false},
	}

	for _, tc := range cases {
		if got := ignore.WithinBuiltinIgnore(tc.path); got != tc.ignored {
			test.Errorf("WithinBuiltinIgnore(%q) = %v, want %v", tc.path, got, tc.ignored)
		}
	}
}
