package subunit_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/linenum"
	"github.com/germanamz/tusk/internal/subunit"
)

// unitLines parses body as a whole file (no frontmatter) and returns each
// unit's [StartLine, EndLine] keyed by its structural address.
func unitLines(test *testing.T, body string, scheme linenum.Scheme) map[string][2]int {
	test.Helper()

	units, parseErr := subunit.Parse([]byte(body))

	if parseErr != nil {
		test.Fatalf("Parse: %v", parseErr)
	}

	subunit.AssignLines(units, []byte(body), 0, scheme)

	out := make(map[string][2]int, len(units))

	for _, unit := range units {
		out[unit.Address] = [2]int{unit.StartLine, unit.EndLine}
	}

	return out
}

func assertLines(test *testing.T, got map[string][2]int, want map[string][2]int) {
	test.Helper()

	for address, wantLines := range want {
		gotLines, present := got[address]

		if !present {
			test.Errorf("no unit at %s; got %v", address, got)

			continue
		}

		if gotLines != wantLines {
			test.Errorf("%s lines = %v, want %v", address, gotLines, wantLines)
		}
	}

	if len(got) != len(want) {
		test.Errorf("got %d units %v, want %d", len(got), got, len(want))
	}
}

func TestAssignLines_SectionsCoverHeadingThroughBody(test *testing.T) {
	body := strings.Join([]string{
		"# Ledger", // 1
		"",
		"## Entries", // 3
		"",
		"The ledger stores one row per entry.", // 5
		"",
		"## Balances", // 7
		"",
		"Balances are derived from entries, never stored.", // 9
		"",
		"",
	}, "\n")

	assertLines(test, unitLines(test, body, linenum.SchemeLF), map[string][2]int{
		"S1":     {1, 9},
		"S1.1":   {3, 5},
		"S1.1P1": {5, 5},
		"S1.2":   {7, 9},
		"S1.2P1": {9, 9},
	})
}

func TestAssignLines_EmptySectionAndSetextHeading(test *testing.T) {
	body := strings.Join([]string{
		"Intro line one", // 1
		"intro line two", // 2
		"",
		"Setext Title", // 4
		"============", // 5
		"",
		"## Empty", // 7
		"",
		"## Last", // 9
		"tail",    // 10
	}, "\n")

	assertLines(test, unitLines(test, body, linenum.SchemeLF), map[string][2]int{
		"P1":     {1, 2},
		"S1":     {4, 10},
		"S1.1":   {7, 7},
		"S1.2":   {9, 10},
		"S1.2P1": {10, 10},
	})
}

func TestAssignLines_EmptySetextSectionIncludesUnderline(test *testing.T) {
	body := "Title\n-----\n"

	assertLines(test, unitLines(test, body, linenum.SchemeLF), map[string][2]int{
		"S1": {1, 2},
	})
}

func TestAssignLines_FencedCodeIncludesFences(test *testing.T) {
	body := strings.Join([]string{
		"```go", // 1
		"a := 1",
		"```", // 3
		"",
		"~~~~", // 5
		"~~~~", // 6 (empty block)
		"",
		"```", // 8 (never closed)
		"tail",
		"", // 10, blank: not part of the range
	}, "\n")

	assertLines(test, unitLines(test, body, linenum.SchemeLF), map[string][2]int{
		"B1": {1, 3},
		"B2": {5, 6},
		"B3": {8, 9},
	})
}

// TestAssignLines_FenceLikeLineOpeningANewBlockIsNotAClosingFence: a fenced
// block inside a list item ends when the item does; a column-0 fence after it
// opens a new block and must not be read as the first block's closing fence.
func TestAssignLines_FenceLikeLineOpeningANewBlockIsNotAClosingFence(test *testing.T) {
	body := strings.Join([]string{
		"- item", // 1
		"  ```",  // 2
		"  code", // 3
		"```",    // 4 opens a new top-level block
		"more",   // 5
		"```",    // 6
	}, "\n")

	got := unitLines(test, body, linenum.SchemeLF)

	if got["B1"] != [2]int{2, 3} {
		test.Errorf("B1 lines = %v, want [2 3]", got["B1"])
	}

	if got["B2"] != [2]int{4, 6} {
		test.Errorf("B2 lines = %v, want [4 6]", got["B2"])
	}
}

// TestAssignLines_SectionEndsAtNestedClosingFence: a section whose last block
// is a list item wrapping a fenced code block ends at that block's closing
// fence, not at its last content line.
func TestAssignLines_SectionEndsAtNestedClosingFence(test *testing.T) {
	body := strings.Join([]string{
		"# Setup", // 1
		"",
		"- run:", // 3
		"  ```sh",
		"  make",
		"  ```", // 6
		"",
	}, "\n")

	got := unitLines(test, body, linenum.SchemeLF)

	if got["S1"] != [2]int{1, 6} {
		test.Errorf("S1 lines = %v, want [1 6]", got["S1"])
	}

	if got["S1B1"] != [2]int{4, 6} {
		test.Errorf("S1B1 lines = %v, want [4 6]", got["S1B1"])
	}
}

func TestAssignLines_ListItemsCoverOwnText(test *testing.T) {
	body := strings.Join([]string{
		"- first item", // 1
		"  continues",  // 2
		"  - nested",   // 3
		"- [ ] task",   // 4
		"",
		"  loose continuation", // 6
	}, "\n")

	assertLines(test, unitLines(test, body, linenum.SchemeLF), map[string][2]int{
		"L1": {1, 2},
		"L2": {3, 3},
		"L3": {4, 6},
	})
}

func TestAssignLines_IndentedCodeBlockquoteAndTable(test *testing.T) {
	body := strings.Join([]string{
		"    indented code", // 1
		"    more code",     // 2
		"",
		"> quoted", // 4
		"> > nested",
		"> end", // 6
		"",
		"| h1 | h2 |", // 8
		"| -- | -- |",
		"| a  | b  |", // 10
		"| c  |    |", // 11
	}, "\n")

	assertLines(test, unitLines(test, body, linenum.SchemeLF), map[string][2]int{
		"B1":     {1, 2},
		"Q1":     {4, 6},
		"T1R0C0": {8, 8},
		"T1R0C1": {8, 8},
		"T1R1C0": {10, 10},
		"T1R1C1": {10, 10},
		"T1R2C0": {11, 11},
		"T1R2C1": {11, 11},
	})
}

// TestAssignLines_CRLFAndLoneCR: CRLF numbers the same under every scheme;
// a lone CR is a line break to the parser and to universal, but not to lf.
func TestAssignLines_CRLFAndLoneCR(test *testing.T) {
	crlf := "# Title\r\n\r\nbody text\r\n"

	for _, scheme := range []linenum.Scheme{linenum.SchemeLF, linenum.SchemeUniversal} {
		assertLines(test, unitLines(test, crlf, scheme), map[string][2]int{
			"S1":   {1, 3},
			"S1P1": {3, 3},
		})
	}

	loneCR := "# Title\r\rbody\nmore\n"

	assertLines(test, unitLines(test, loneCR, linenum.SchemeUniversal), map[string][2]int{
		"S1":   {1, 4},
		"S1P1": {3, 4},
	})

	assertLines(test, unitLines(test, loneCR, linenum.SchemeLF), map[string][2]int{
		"S1":   {1, 2},
		"S1P1": {1, 2},
	})
}

func TestAssignLines_UnicodeSeparators(test *testing.T) {
	body := "# Title\n\nalpha beta\n"

	assertLines(test, unitLines(test, body, linenum.SchemeUnicode), map[string][2]int{
		"S1":   {1, 4},
		"S1P1": {3, 4},
	})

	assertLines(test, unitLines(test, body, linenum.SchemeLF), map[string][2]int{
		"S1":   {1, 3},
		"S1P1": {3, 3},
	})
}

// TestAssignLines_BodyOffsetCountsFrontmatter: lines are file-relative, so
// the frontmatter above the parsed body shifts every range.
func TestAssignLines_BodyOffsetCountsFrontmatter(test *testing.T) {
	frontmatter := "---\ntype: note\n---\n\n"
	body := "# Ledger\n\nentry\n"
	file := []byte(frontmatter + body)

	units, parseErr := subunit.Parse([]byte(body))

	if parseErr != nil {
		test.Fatalf("Parse: %v", parseErr)
	}

	subunit.AssignLines(units, file, len(frontmatter), linenum.SchemeLF)

	got := map[string][2]int{}

	for _, unit := range units {
		got[unit.Address] = [2]int{unit.StartLine, unit.EndLine}
	}

	assertLines(test, got, map[string][2]int{
		"S1":   {5, 7},
		"S1P1": {7, 7},
	})
}

// TestAssignLines_SkipsUnitsWithoutSpan: a unit with no byte span (the HTML
// parser has no source positions) keeps zero lines, which the index stores
// as NULL.
func TestAssignLines_SkipsUnitsWithoutSpan(test *testing.T) {
	units := []subunit.Unit{{Kind: subunit.KindParagraph, Text: "x"}}

	subunit.AssignLines(units, []byte("x"), 0, linenum.SchemeLF)

	if units[0].StartLine != 0 || units[0].EndLine != 0 {
		test.Errorf("lines = [%d %d], want [0 0]", units[0].StartLine, units[0].EndLine)
	}
}
