package query

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/linenum"
)

// numberedSource builds a source whose line N (1-based) is lines[N-1].
func numberedSource(lines ...string) []byte {
	return []byte(strings.Join(lines, "\n") + "\n")
}

// TestPassageWindow_WholeOwnTextFits: when every leaf fits under the limit the
// passage is all of them, blank lines between included, from the first
// leaf's first line to the last leaf's last line.
func TestPassageWindow_WholeOwnTextFits(test *testing.T) {
	file := newPassageFile(numberedSource("## Values", "", "- a", "- b", "", "Closing paragraph."), linenum.SchemeLF)
	leaves := []lineRange{{3, 3}, {4, 4}, {6, 6}}

	text, first, last := passageWindow(file, leaves, 1, 2048)

	if text != "- a\n- b\n\nClosing paragraph." || first != 3 || last != 6 {
		test.Errorf("passage = %q L%d-%d, want the three leaves at L3-6", text, first, last)
	}
}

// TestPassageWindow_GrowsAroundBestLeaf: over the limit, the window starts at
// the leaf that scored and takes whole neighbors, the following one first,
// until the next leaf on either side no longer fits.
func TestPassageWindow_GrowsAroundBestLeaf(test *testing.T) {
	lines := make([]string, 0, 9)
	leaves := make([]lineRange, 0, 9)

	for index := range 9 {
		lines = append(lines, string(rune('a'+index))+strings.Repeat(".", 9))
		leaves = append(leaves, lineRange{index + 1, index + 1})
	}

	file := newPassageFile(numberedSource(lines...), linenum.SchemeLF)

	// Each leaf is 10 bytes plus a newline between leaves: three leaves are
	// 32 bytes, four are 43.
	text, first, last := passageWindow(file, leaves, 4, 40)

	if first != 4 || last != 6 {
		test.Errorf("window = L%d-%d, want L4-6 (best leaf L5 plus one neighbor each side)", first, last)
	}

	if !strings.Contains(text, "e.........") || len(text) > 40 {
		test.Errorf("text = %q (%d bytes), want the best leaf within 40 bytes", text, len(text))
	}

	_, first, last = passageWindow(file, leaves, 4, 25)

	if first != 5 || last != 6 {
		test.Errorf("window = L%d-%d, want L5-6 (the following neighbor comes first)", first, last)
	}
}

// TestPassageWindow_OversizedLeafKeepsWholeLines: a leaf larger than the limit
// on its own (a long code block) keeps the whole lines that fit from its start.
func TestPassageWindow_OversizedLeafKeepsWholeLines(test *testing.T) {
	file := newPassageFile(numberedSource("```go", "first := 1", "second := 2", "third := 3", "```"), linenum.SchemeLF)

	text, first, last := passageWindow(file, []lineRange{{1, 5}}, 0, 30)

	if text != "```go\nfirst := 1\nsecond := 2" || first != 1 || last != 3 {
		test.Errorf("passage = %q L%d-%d, want the first three lines", text, first, last)
	}
}

// TestPassageWindow_OversizedLineIsCut: a single line over the limit is cut at
// a word boundary and marked with an ellipsis.
func TestPassageWindow_OversizedLineIsCut(test *testing.T) {
	file := newPassageFile(numberedSource("alpha beta gamma delta epsilon"), linenum.SchemeLF)

	text, first, last := passageWindow(file, []lineRange{{1, 1}}, 0, 20)

	if text != "alpha beta gamma…" || first != 1 || last != 1 {
		test.Errorf("passage = %q L%d-%d, want a word-boundary cut with an ellipsis", text, first, last)
	}

	if len(text) > 20 {
		test.Errorf("cut passage is %d bytes, want at most 20", len(text))
	}
}

// TestPassageFile_LinesFollowScheme: lines are sliced under the workspace's
// line-numbering scheme, the scheme sub-unit line ranges were numbered with,
// and CRLF folds to LF.
func TestPassageFile_LinesFollowScheme(test *testing.T) {
	source := []byte("one\r\ntwo\rthree\r\nfour\r\n")

	if got := newPassageFile(source, linenum.SchemeLF).lines(2, 2); got != "two\rthree" {
		test.Errorf("lf line 2 = %q, want %q", got, "two\rthree")
	}

	if got := newPassageFile(source, linenum.SchemeUniversal).lines(2, 3); got != "two\rthree" {
		test.Errorf("universal lines 2-3 = %q, want %q", got, "two\rthree")
	}

	if got := newPassageFile(source, linenum.SchemeUniversal).lines(3, 4); got != "three\nfour" {
		test.Errorf("universal lines 3-4 = %q, want %q", got, "three\nfour")
	}
}
