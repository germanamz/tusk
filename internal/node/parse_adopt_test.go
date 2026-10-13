package node_test

import (
	"errors"
	"testing"

	"github.com/germanamz/tusk/internal/node"
)

func TestParseFileAs_AdoptsFileWithoutFrontmatter(test *testing.T) {
	content := []byte("# Meeting notes\n\nWe talked about [[auth]].\n")

	parsed, parseErr := node.ParseFileAs("notes/meeting.md", content, "note")

	if parseErr != nil {
		test.Fatalf("ParseFileAs: %v", parseErr)
	}

	if parsed.ID != "notes/meeting" || parsed.Path != "notes/meeting.md" {
		test.Errorf("ID, Path = %q, %q", parsed.ID, parsed.Path)
	}

	if parsed.Type != "note" || !parsed.Adopted {
		test.Errorf("Type, Adopted = %q, %v; want note, true", parsed.Type, parsed.Adopted)
	}

	if parsed.Title != "Meeting notes" {
		test.Errorf("Title = %q, want the first heading", parsed.Title)
	}

	if len(parsed.Properties) != 0 {
		test.Errorf("Properties = %v, want none", parsed.Properties)
	}

	if string(parsed.Body) != string(content) || parsed.BodyOffset != 0 {
		test.Errorf("Body, BodyOffset = %q, %d; want the whole file at 0", parsed.Body, parsed.BodyOffset)
	}
}

func TestParseFileAs_AdoptsFrontmatterWithoutType(test *testing.T) {
	content := []byte("---\naliases: [standup]\ntags: [team]\n---\n\n# Standup\n\nBody.\n")

	parsed, parseErr := node.ParseFileAs("standup.md", content, "note")

	if parseErr != nil {
		test.Fatalf("ParseFileAs: %v", parseErr)
	}

	if parsed.Type != "note" || !parsed.Adopted {
		test.Errorf("Type, Adopted = %q, %v; want note, true", parsed.Type, parsed.Adopted)
	}

	if _, hasType := parsed.Properties["type"]; hasType {
		test.Errorf("Properties gained a type key: %v", parsed.Properties)
	}

	if _, hasTags := parsed.Properties["tags"]; !hasTags {
		test.Errorf("Properties = %v, want the file's own frontmatter kept", parsed.Properties)
	}

	if parsed.Title != "Standup" {
		test.Errorf("Title = %q, want the first heading", parsed.Title)
	}

	if string(parsed.Body) != "# Standup\n\nBody.\n" {
		test.Errorf("Body = %q", parsed.Body)
	}
}

func TestParseFileAs_FrontmatterTitleWins(test *testing.T) {
	content := []byte("---\ntitle: From frontmatter\n---\n# From heading\n")

	parsed, parseErr := node.ParseFileAs("doc.md", content, "note")

	if parseErr != nil {
		test.Fatalf("ParseFileAs: %v", parseErr)
	}

	if parsed.Title != "From frontmatter" {
		test.Errorf("Title = %q, want the frontmatter title", parsed.Title)
	}
}

func TestParseFileAs_TitleFallsBackToFileName(test *testing.T) {
	cases := []struct {
		name    string
		path    string
		content string
		want    string
	}{
		{"no heading", "notes/my-idea.md", "Just prose.\n", "my-idea"},
		{"only a level-two heading", "notes/my-idea.md", "## Section\n", "my-idea"},
		{"no space after the hash", "notes/my-idea.md", "#tag line\n", "my-idea"},
		{"heading inside a code fence", "notes/my-idea.md", "```md\n# Not a title\n```\n", "my-idea"},
		{"heading inside a tilde fence", "notes/my-idea.md", "~~~\n# Not a title\n~~~\n", "my-idea"},
		{"mdx keeps its stem", "pages/intro.mdx", "Hello.\n", "intro"},
	}

	for _, tc := range cases {
		test.Run(tc.name, func(test *testing.T) {
			parsed, parseErr := node.ParseFileAs(tc.path, []byte(tc.content), "note")

			if parseErr != nil {
				test.Fatalf("ParseFileAs: %v", parseErr)
			}

			if parsed.Title != tc.want {
				test.Errorf("Title = %q, want %q", parsed.Title, tc.want)
			}
		})
	}
}

func TestParseFileAs_HeadingForms(test *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"closing hashes are dropped", "# Title ##\n", "Title"},
		{"up to three spaces of indent", "   # Indented\n", "Indented"},
		{"heading after a closed fence", "```\ncode\n```\n# Real\n", "Real"},
		{"first of two headings", "# One\n# Two\n", "One"},
		{"crlf line endings", "# Windows\r\nbody\r\n", "Windows"},
	}

	for _, tc := range cases {
		test.Run(tc.name, func(test *testing.T) {
			parsed, parseErr := node.ParseFileAs("doc.md", []byte(tc.content), "note")

			if parseErr != nil {
				test.Fatalf("ParseFileAs: %v", parseErr)
			}

			if parsed.Title != tc.want {
				test.Errorf("Title = %q, want %q", parsed.Title, tc.want)
			}
		})
	}
}

func TestParseFileAs_TypedFileKeepsItsOwnType(test *testing.T) {
	content := []byte("---\ntype: decision\n---\n# Use SSO\n")

	parsed, parseErr := node.ParseFileAs("decisions/sso.md", content, "note")

	if parseErr != nil {
		test.Fatalf("ParseFileAs: %v", parseErr)
	}

	if parsed.Type != "decision" || parsed.Adopted {
		test.Errorf("Type, Adopted = %q, %v; want decision, false", parsed.Type, parsed.Adopted)
	}

	if parsed.Title != "" {
		test.Errorf("Title = %q; a typed file's title comes only from frontmatter", parsed.Title)
	}
}

func TestParseFileAs_EmptyFallbackKeepsTheErrors(test *testing.T) {
	if _, parseErr := node.ParseFileAs("plain.md", []byte("# Plain\n"), ""); !errors.Is(parseErr, node.ErrMissingFrontmatter) {
		test.Errorf("no frontmatter: err = %v, want ErrMissingFrontmatter", parseErr)
	}

	if _, parseErr := node.ParseFileAs("plain.md", []byte("---\ntags: [a]\n---\n"), ""); !errors.Is(parseErr, node.ErrMissingType) {
		test.Errorf("no type: err = %v, want ErrMissingType", parseErr)
	}
}

func TestParseFileAs_BOMWithoutFrontmatter(test *testing.T) {
	content := []byte("\xef\xbb\xbf# Titled\n")

	parsed, parseErr := node.ParseFileAs("bom.md", content, "note")

	if parseErr != nil {
		test.Fatalf("ParseFileAs: %v", parseErr)
	}

	if parsed.Title != "Titled" {
		test.Errorf("Title = %q; a BOM must not hide the first heading", parsed.Title)
	}

	if string(parsed.Body) != "# Titled\n" || parsed.BodyOffset != 3 {
		test.Errorf("Body, BodyOffset = %q, %d; want the BOM-free body at offset 3", parsed.Body, parsed.BodyOffset)
	}
}

func TestParseContentFileAs_HTMLIsNeverAdopted(test *testing.T) {
	content := []byte("<html><head><title>Page</title></head><body>hi</body></html>")

	if _, parseErr := node.ParseContentFileAs("page.html", content, "note"); !errors.Is(parseErr, node.ErrMissingType) {
		test.Errorf("err = %v, want ErrMissingType for untyped HTML", parseErr)
	}
}

func TestParseContentFileAs_AdoptsMarkdown(test *testing.T) {
	parsed, parseErr := node.ParseContentFileAs("plain.md", []byte("hello\n"), "note")

	if parseErr != nil {
		test.Fatalf("ParseContentFileAs: %v", parseErr)
	}

	if parsed.Type != "note" || parsed.ID != "plain" {
		test.Errorf("Type, ID = %q, %q", parsed.Type, parsed.ID)
	}
}
