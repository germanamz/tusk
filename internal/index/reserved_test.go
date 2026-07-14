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

// TestIsIndexableExt_CoversMDX pins that .mdx joins .md/.html/.htm as a content
// kind while unrelated extensions stay excluded.
func TestIsIndexableExt_CoversMDX(test *testing.T) {
	for _, name := range []string{"a.md", "a.mdx", "a.html", "a.htm"} {
		if !index.IsIndexableExt(name) {
			test.Errorf("IsIndexableExt(%q) = false, want true", name)
		}
	}

	for _, name := range []string{"a.txt", "a.markdown", "a.xhtml", "a"} {
		if index.IsIndexableExt(name) {
			test.Errorf("IsIndexableExt(%q) = true, want false", name)
		}
	}
}

// TestNodeIDForPath pins the single id rule: only ".md" is a bare stem; every
// other indexable kind (.html/.htm/.mdx) retains its extension so same-stem
// files never collide on the nodes.id primary key (Decision #12).
func TestNodeIDForPath(test *testing.T) {
	cases := map[string]string{
		"notes/auth.md":   "notes/auth",
		"README.md":       "README",
		"notes/page.html": "notes/page.html",
		"notes/page.htm":  "notes/page.htm",
		"notes/guide.mdx": "notes/guide.mdx",
		"noext":           "noext",
	}

	for in, want := range cases {
		if got := index.NodeIDForPath(in); got != want {
			test.Errorf("NodeIDForPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestFilePathForID pins the inverse: a bare stem regains ".md", while an id
// already carrying an indexable extension (.html/.htm/.mdx) is its own path.
func TestFilePathForID(test *testing.T) {
	cases := map[string]string{
		"notes/auth":      "notes/auth.md",
		"README":          "README.md",
		"notes/page.html": "notes/page.html",
		"notes/page.htm":  "notes/page.htm",
		"notes/guide.mdx": "notes/guide.mdx",
		// A ".md"-suffixed id is never a retained path — it can only be the bare
		// stem NodeIDForPath produced from a "*.md.md" file, so it regains ".md".
		"notes/changelog.md": "notes/changelog.md.md",
	}

	for id, want := range cases {
		if got := index.FilePathForID(id); got != want {
			test.Errorf("FilePathForID(%q) = %q, want %q", id, got, want)
		}
	}
}

// TestNodeIDPathRoundTrip pins that FilePathForID reverses NodeIDForPath for
// every content kind, so a rename derives an id the reindex re-parse maps back
// to the same on-disk file.
func TestNodeIDPathRoundTrip(test *testing.T) {
	for _, path := range []string{"notes/auth.md", "notes/guide.mdx", "pages/x.html", "pages/y.htm", "notes/changelog.md.md"} {
		if got := index.FilePathForID(index.NodeIDForPath(path)); got != path {
			test.Errorf("round-trip %q = %q, want %q", path, got, path)
		}
	}
}
