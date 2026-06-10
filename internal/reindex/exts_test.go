package reindex

import "testing"

func TestIndexableExts_CoversMarkdownAndHTML(test *testing.T) {
	for _, ext := range []string{".md", ".html", ".htm"} {
		if !indexableExts[ext] {
			test.Errorf("indexableExts[%q] = false, want true", ext)
		}
	}

	for _, ext := range []string{".txt", ".markdown", ".xhtml", ""} {
		if indexableExts[ext] {
			test.Errorf("indexableExts[%q] = true, want false", ext)
		}
	}
}

func TestNodeIDForPath_StripsOnlyMarkdown(test *testing.T) {
	cases := map[string]string{
		"notes/auth.md":   "notes/auth",
		"notes/page.html": "notes/page.html",
		"notes/page.htm":  "notes/page.htm",
		"README.md":       "README",
	}

	for in, want := range cases {
		if got := nodeIDForPath(in); got != want {
			test.Errorf("nodeIDForPath(%q) = %q, want %q", in, got, want)
		}
	}
}
