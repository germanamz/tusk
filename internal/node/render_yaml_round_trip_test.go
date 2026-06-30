package node

import (
	"strings"
	"testing"
)

// TestYAMLNeedsQuoting_AmbiguousScalars pins the round-trip guarantee that
// yamlNeedsQuoting documents: any string a YAML parser would resolve to a
// non-string scalar must be quoted. The date case is issue #662 — an unquoted
// YYYY-MM-DD resolves to a time.Time on the next parse, so the modify
// round-trip (parse -> render -> parse) would silently change its type.
func TestYAMLNeedsQuoting_AmbiguousScalars(test *testing.T) {
	mustQuote := []string{
		"2026-06-11",           // date -> time.Time
		"2026-06-11T00:00:00Z", // datetime -> time.Time
		"0x1F",                 // hex int
		"0o17",                 // octal int
		".inf",                 // float infinity
		".nan",                 // float NaN
	}

	for _, s := range mustQuote {
		if !yamlNeedsQuoting(s) {
			test.Errorf("yamlNeedsQuoting(%q) = false, want true (resolves to a non-string scalar)", s)
		}
	}

	plain := []string{"active", "high", "notes/x", "hello world"}

	for _, s := range plain {
		if yamlNeedsQuoting(s) {
			test.Errorf("yamlNeedsQuoting(%q) = true, want false (plain string)", s)
		}
	}
}

// TestRenderMarkdown_DateStringRoundTrips reproduces issue #662 at the
// serialization layer: a string-valued date property must survive
// renderMarkdown -> ParseFile as a string, not be normalized into a time.Time.
func TestRenderMarkdown_DateStringRoundTrips(test *testing.T) {
	props := map[string]any{"type": "ticket", "status": "pending", "due": "2026-06-11"}

	rendered, renderErr := renderMarkdown(props, []byte("body\n"))

	if renderErr != nil {
		test.Fatalf("renderMarkdown: %v", renderErr)
	}

	if !strings.Contains(string(rendered), `due: "2026-06-11"`) {
		test.Errorf("rendered frontmatter should quote the date; got:\n%s", rendered)
	}

	reparsed, parseErr := ParseFile("tickets/foo.md", rendered)

	if parseErr != nil {
		test.Fatalf("ParseFile: %v", parseErr)
	}

	got, ok := reparsed.Properties["due"].(string)

	if !ok {
		test.Fatalf("reparsed due is %T, want string", reparsed.Properties["due"])
	}

	if got != "2026-06-11" {
		test.Errorf("reparsed due = %q, want 2026-06-11", got)
	}
}
