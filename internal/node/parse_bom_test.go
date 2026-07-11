package node_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/node"
)

// utf8BOM is the three-byte UTF-8 byte-order mark that editors sometimes write
// at the head of a file.
const utf8BOM = "\xef\xbb\xbf"

// TestParseFile_StripsLeadingBOM guards #682 item 2: a UTF-8 BOM before the
// frontmatter must be stripped so the frontmatter is still recognized.
// Otherwise the delimiter check fails, ParseFile errors, and the whole typed
// file is silently dropped from the index for good.
func TestParseFile_StripsLeadingBOM(test *testing.T) {
	content := []byte(utf8BOM + "---\ntype: note\ntitle: Title\n---\n# Title\n\nbody text here\n")

	parsed, parseErr := node.ParseFile("bom.md", content)
	if parseErr != nil {
		test.Fatalf("ParseFile with leading BOM: %v", parseErr)
	}

	if parsed.Type != "note" {
		test.Errorf("Type = %q, want note", parsed.Type)
	}

	if parsed.Title != "Title" {
		test.Errorf("Title = %q, want Title", parsed.Title)
	}
}

// TestParseFile_StripsBodyLeadingBOM guards #682 item 2: a BOM at the start of
// the body (right after the closing frontmatter delimiter) must not survive
// into the parsed body, or the first heading demotes to a paragraph in the
// sub-unit outline.
func TestParseFile_StripsBodyLeadingBOM(test *testing.T) {
	content := []byte("---\ntype: note\n---\n" + utf8BOM + "# Title\n\nbody text here\n")

	parsed, parseErr := node.ParseFile("bodybom.md", content)
	if parseErr != nil {
		test.Fatalf("ParseFile with body BOM: %v", parseErr)
	}

	body := string(parsed.Body)

	if strings.HasPrefix(body, utf8BOM) {
		test.Errorf("body retained a leading BOM: %q", body)
	}

	if !strings.HasPrefix(body, "# Title") {
		test.Errorf("body should start with the heading, got %q", body)
	}
}
