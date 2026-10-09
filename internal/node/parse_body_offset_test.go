package node_test

import (
	"bytes"
	"testing"

	"github.com/germanamz/tusk/internal/node"
)

// TestParseFile_BodyOffsetLocatesBodyInFile: BodyOffset is where Body starts
// in the file as read, so sub-unit spans over Body can be numbered as file
// lines (frontmatter included). The leading blank lines and a body BOM that
// ParseFile strips sit before the offset.
func TestParseFile_BodyOffsetLocatesBodyInFile(test *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{name: "plain", content: "---\ntype: note\n---\n# Title\n"},
		{name: "blank lines", content: "---\ntype: note\n---\n\n\n# Title\n"},
		{name: "leading bom", content: utf8BOM + "---\ntype: note\n---\n# Title\n"},
		{name: "body bom", content: "---\ntype: note\n---\n\n" + utf8BOM + "# Title\n"},
		{name: "crlf", content: "---\r\ntype: note\r\n---\r\n\r\n# Title\r\n"},
	}

	for _, testCase := range cases {
		content := []byte(testCase.content)

		parsed, parseErr := node.ParseFile("note.md", content)

		if parseErr != nil {
			test.Fatalf("%s: ParseFile: %v", testCase.name, parseErr)
		}

		if !bytes.HasPrefix(parsed.Body, []byte("# Title")) {
			test.Fatalf("%s: Body = %q, want it to start at the heading", testCase.name, parsed.Body)
		}

		if got := string(content[parsed.BodyOffset:]); got != string(parsed.Body) {
			test.Errorf("%s: content[BodyOffset:] = %q, want Body %q", testCase.name, got, parsed.Body)
		}
	}
}
