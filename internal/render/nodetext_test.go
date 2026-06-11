package render

import "testing"

func TestMarkdownPlainText_StripsMarkup(test *testing.T) {
	body := []byte("# Title\n\nFirst **bold** line.\n\n- one\n- two\n")
	got := markdownPlainText(body)

	if got == "" {
		test.Fatalf("markdownPlainText returned empty for non-empty markdown")
	}

	for _, marker := range []string{"#", "**", "- "} {
		if contains(got, marker) {
			test.Errorf("plain text still contains markdown marker %q: %q", marker, got)
		}
	}

	for _, word := range []string{"Title", "First", "bold", "line", "one", "two"} {
		if !contains(got, word) {
			test.Errorf("plain text dropped word %q: %q", word, got)
		}
	}
}

func TestNodeText_DispatchesByExtension(test *testing.T) {
	markdown := []byte("# Heading\n\nBody text.\n")
	html := []byte("<html><body><h1>Heading</h1><p>Body text.</p></body></html>")

	mdOut := NodeText("notes/foo.md", markdown)
	htmlOut := NodeText("notes/foo.html", html)
	htmOut := NodeText("notes/foo.htm", html)

	for name, out := range map[string]string{"md": mdOut, "html": htmlOut, "htm": htmOut} {
		if !contains(out, "Heading") || !contains(out, "Body text") {
			test.Errorf("%s: NodeText dropped prose: %q", name, out)
		}
	}

	if contains(htmlOut, "<") || contains(htmlOut, ">") {
		test.Errorf(".html dispatch left angle brackets: %q", htmlOut)
	}
}

func TestNodeText_UnknownExtensionUsesMarkdown(test *testing.T) {
	// An extensionless or unknown-extension path falls back to the markdown
	// strategy (matches the node-ID scheme where `.md` is the default kind).
	got := NodeText("notes/foo", []byte("# Hi\n\nWords.\n"))

	if !contains(got, "Hi") || !contains(got, "Words") {
		test.Errorf("unknown-extension fallback dropped prose: %q", got)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}

	return -1
}
