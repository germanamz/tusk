package reindex

import (
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestIndexableExts_CoversMarkdownMDXAndHTML(test *testing.T) {
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

func TestNodeIDForPath_StripsOnlyMarkdown(test *testing.T) {
	cases := map[string]string{
		"notes/auth.md":   "notes/auth",
		"notes/page.html": "notes/page.html",
		"notes/page.htm":  "notes/page.htm",
		"notes/guide.mdx": "notes/guide.mdx",
		"README.md":       "README",
	}

	for in, want := range cases {
		if got := nodeIDForPath(in); got != want {
			test.Errorf("nodeIDForPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNodeIDForPath_TombstoneParityWithMarkdown(test *testing.T) {
	// Parity check: the tombstone path and the parse path must agree on the
	// id for the same file, for both markdown and html.
	if nodeIDForPath("notes/auth.md") != "notes/auth" {
		test.Errorf("markdown tombstone id mismatch")
	}

	if nodeIDForPath("notes/page.html") != "notes/page.html" {
		test.Errorf("html tombstone id mismatch")
	}
}
