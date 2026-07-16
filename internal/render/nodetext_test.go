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

// TestStripFrontmatter pins the exported wrapper's contract: the leading YAML
// block is removed and everything after it — markup included — survives
// verbatim. Callers that render the markdown themselves (the book view hands it
// to the browser) depend on the markup surviving, which is what separates this
// from NodeText.
func TestStripFrontmatter(test *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "strips frontmatter and keeps markup",
			body: "---\ntitle: A\ntype: note\n---\n# Heading\n\n*emphasis*\n",
			want: "# Heading\n\n*emphasis*\n",
		},
		{
			name: "body without frontmatter is unchanged",
			body: "# Heading\n\nNo frontmatter here.\n",
			want: "# Heading\n\nNo frontmatter here.\n",
		},
		{
			name: "crlf frontmatter",
			body: "---\r\ntitle: A\r\n---\r\n# Heading\r\n",
			want: "# Heading\r\n",
		},
		{
			name: "unterminated frontmatter is returned unchanged",
			body: "---\ntitle: A\n\n# Heading\n",
			want: "---\ntitle: A\n\n# Heading\n",
		},
		{
			name: "empty body",
			body: "",
			want: "",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			if got := string(StripFrontmatter([]byte(testCase.body))); got != testCase.want {
				test.Fatalf("StripFrontmatter(%q) = %q, want %q", testCase.body, got, testCase.want)
			}
		})
	}
}
