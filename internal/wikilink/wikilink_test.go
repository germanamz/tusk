package wikilink_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/germanamz/tusk/internal/wikilink"
)

func TestExtract_FindsBracketedTargets(test *testing.T) {
	body := []byte(`# Body

See [[notes/auth-rfc]] for context.

Also relates to [[tickets/refactor-storage]].

A second mention of [[notes/auth-rfc]] is deduped.
`)

	links := wikilink.Extract(body)

	sort.Strings(links)

	want := []string{"notes/auth-rfc", "tickets/refactor-storage"}

	if !reflect.DeepEqual(links, want) {
		test.Errorf("got %v, want %v", links, want)
	}
}

func TestExtract_IgnoresEscapedAndCodeFence(test *testing.T) {
	body := []byte("Real link [[real]].\n\n```\nfenced [[notreal]]\n```\n")

	links := wikilink.Extract(body)

	if len(links) != 1 || links[0] != "real" {
		test.Errorf("got %v, want [real]", links)
	}
}

func TestExtract_ReturnsEmptyWhenNoLinks(test *testing.T) {
	links := wikilink.Extract([]byte("plain body, no brackets at all\n"))

	if len(links) != 0 {
		test.Errorf("got %v, want empty", links)
	}
}

// #690: an Obsidian aliased wikilink `[[id|display]]` links to id — the
// display text after the pipe is presentation only. Extraction must return the
// target (before the pipe), trimmed, exactly as a bare `[[id]]` would.
func TestExtract_AliasedLinkExtractsTarget(test *testing.T) {
	body := []byte("see [[notes/target|the target]] and [[design/x | Design X]] and bare [[notes/target]]\n")

	links := wikilink.Extract(body)

	// The aliased and bare links to notes/target dedupe to one target.
	want := []string{"notes/target", "design/x"}

	if !reflect.DeepEqual(links, want) {
		test.Errorf("got %v, want %v", links, want)
	}
}

// #690: an aliased deep link `[[id#S1|display]]` keeps its sub-unit fragment in
// the target and drops only the display suffix.
func TestExtract_AliasedDeepLinkKeepsFragment(test *testing.T) {
	body := []byte("jump to [[notes/target#S1|that section]]\n")

	links := wikilink.Extract(body)

	if len(links) != 1 || links[0] != "notes/target#S1" {
		test.Errorf("got %v, want [notes/target#S1]", links)
	}
}

// #788: code is never a link. A page that documents the syntax in inline code,
// a fenced or tilde-fenced block, or an indented block must not link to the
// example target; only the real link outside code counts. The cases mirror the
// web reader, whose markdown-it wikilink rule never sees text inside code.
func TestExtract_SkipsCode(test *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "inline code span", body: "writes `[[id]]` but means [[real]]\n"},
		{name: "aliased link in inline code", body: "the form `[[id|Label]]` links to id; see [[real]]\n"},
		{name: "double-backtick span holding a backtick", body: "``a ` [[id]]`` then [[real]]\n"},
		{name: "span wrapping a line", body: "a `code\n[[id]] span` ends, [[real]] does not\n"},
		{name: "code span in a heading", body: "# The `[[id]]` form\n\n[[real]]\n"},
		{name: "code span in a list item", body: "- `[[id]]` example\n- [[real]]\n"},
		{name: "code span in a table cell", body: "| form | link |\n| --- | --- |\n| `[[id]]` | [[real]] |\n"},
		{name: "fenced block", body: "```\n[[id]]\n```\n\n[[real]]\n"},
		{name: "tilde-fenced block", body: "~~~\n[[id]]\n~~~\n\n[[real]]\n"},
		{name: "fenced block in a list item", body: "- item\n\n  ```\n  [[id]]\n  ```\n- [[real]]\n"},
		{name: "indented block", body: "para\n\n    [[id]]\n\n[[real]]\n"},
		{name: "CRLF line endings", body: "`[[id]]`\r\n\r\n```\r\n[[id]]\r\n```\r\n\r\n[[real]]\r\n"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			links := wikilink.Extract([]byte(testCase.body))

			if !reflect.DeepEqual(links, []string{"real"}) {
				subtest.Errorf("got %v, want [real]", links)
			}
		})
	}
}

// #788: a literal triple backtick inside inline code used to read as an
// unterminated fence and silently drop every link after it. A run of N
// backticks only closes at the next run of exactly N, so the run sits inside
// the span and the rest of the body still links.
func TestExtract_TripleBacktickInsideInlineCodeKeepsLaterLinks(test *testing.T) {
	body := []byte("The fence detector looks for `` ``` ``.\n\nLater: [[real]] and [[also-real]].\n")

	links := wikilink.Extract(body)
	want := []string{"real", "also-real"}

	if !reflect.DeepEqual(links, want) {
		test.Errorf("got %v, want %v", links, want)
	}
}

// An unmatched backtick run is literal text, not a span opener, so a link
// after it still counts.
func TestExtract_UnmatchedBacktickRunIsLiteral(test *testing.T) {
	links := wikilink.Extract([]byte("a `` run with no closer and [[real]]\n"))

	if !reflect.DeepEqual(links, []string{"real"}) {
		test.Errorf("got %v, want [real]", links)
	}
}

// MaskCode blanks code in place: same length, line breaks kept, everything
// outside code byte-identical, so offsets found in the mask index the original.
func TestMaskCode_KeepsOffsetsAndLineBreaks(test *testing.T) {
	body := "keep [[a]] `[[b]]`\r\n\r\n```go\n[[c]]\n```\n"

	masked := string(wikilink.MaskCode([]byte(body)))

	if len(masked) != len(body) {
		test.Fatalf("length changed: got %d, want %d", len(masked), len(body))
	}

	if want := "keep [[a]] `     `\r\n\r\n```go\n     \n```\n"; masked != want {
		test.Errorf("masked:\n got: %q\nwant: %q", masked, want)
	}
}

// ExtractText reads text with no markdown structure (already masked, or plain
// text from an HTML page), so it scans everything, backticks included.
func TestExtractText_ScansEverything(test *testing.T) {
	links := wikilink.ExtractText([]byte("`[[a]]` and [[b|B]]\n"))
	want := []string{"a", "b"}

	if !reflect.DeepEqual(links, want) {
		test.Errorf("got %v, want %v", links, want)
	}
}
