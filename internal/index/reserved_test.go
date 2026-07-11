package index_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

// TestReservedIDReason pins #683: paths whose derived node id would collide
// with tusk's reserved id syntax must be reported so the indexing boundary can
// reject them, while ordinary paths (including bracket names) stay indexable.
func TestReservedIDReason(test *testing.T) {
	cases := []struct {
		name    string
		relPath string
		want    bool // true = must be reported as reserved
		mention string
	}{
		{"plain", "notes/a.md", false, ""},
		{"bracket", "notes/[wip] rocket.md", false, ""},
		{"colon", "notes/reindex-plan.md", false, ""},
		{"space and underscore", "notes/foo_a b.md", false, ""},
		{"hash in name", "notes/y#S1.md", true, "#"},
		{"hash mid path", "a/b#c/d.md", true, "#"},
		{"reindex prefix", "reindex:notes.md", true, "reindex:"},
		{"reindex prefix nested target", "reindex:sub/x.md", true, "reindex:"},
		// A file literally named "reindex" (no colon) is fine — only the
		// reserved "reindex:" prefix collides with the queue key namespace.
		{"reindex without colon", "reindex.md", false, ""},
	}

	for _, tc := range cases {
		test.Run(tc.name, func(test *testing.T) {
			reason := index.ReservedIDReason(tc.relPath)

			if tc.want && reason == "" {
				test.Fatalf("ReservedIDReason(%q) = \"\", want a non-empty reason", tc.relPath)
			}

			if !tc.want && reason != "" {
				test.Fatalf("ReservedIDReason(%q) = %q, want \"\" (path is indexable)", tc.relPath, reason)
			}

			if tc.want && tc.mention != "" && !strings.Contains(reason, tc.mention) {
				test.Errorf("ReservedIDReason(%q) = %q, want it to mention %q", tc.relPath, reason, tc.mention)
			}
		})
	}
}
