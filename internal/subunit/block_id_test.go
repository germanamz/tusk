package subunit_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/germanamz/tusk/internal/subunit"
)

// blockView lists units as "<address> <kind> <text> ^<block id>", the id
// left off when the unit has none.
func blockView(test *testing.T, body string) []string {
	test.Helper()

	units, parseErr := subunit.Parse([]byte(body))

	if parseErr != nil {
		test.Fatalf("Parse: %v", parseErr)
	}

	view := make([]string, 0, len(units))

	for _, unit := range units {
		line := fmt.Sprintf("%s %s %q", unit.Address, unit.Kind, unit.Text)

		if unit.BlockID != "" {
			line += " ^" + unit.BlockID
		}

		view = append(view, line)
	}

	return view
}

// A `^block-id` marker ending a paragraph, list item or quote names that unit
// and leaves its text, and a marker on a line of its own names the block before
// it without being a unit itself: a list's first item, a table's first cell
// (#819).
func TestParse_BlockIDs(test *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want []string
	}{
		{
			name: "trailing marker on a paragraph",
			body: "Some text. ^quote-of-the-day\n",
			want: []string{`P1 paragraph "Some text." ^quote-of-the-day`},
		},
		{
			name: "marker on the paragraph's last line",
			body: "First line\nsecond line\n^Mixed-Case-1\n\nNext.\n",
			want: []string{`P1 paragraph "First line\nsecond line" ^Mixed-Case-1`, `P2 paragraph "Next."`},
		},
		{
			name: "list items",
			body: "- one ^li-1\n- two\n  - nested ^li-2\n- [ ] task ^li-3\n",
			want: []string{
				`L1 list-item "one" ^li-1`,
				`L2 list-item "two"`,
				`L3 list-item "nested" ^li-2`,
				`L4 list-item "task" ^li-3`,
			},
		},
		{
			name: "quote and callout",
			body: "> quoted\n^q1\n\n> [!note] Title\n> body ^q2\n",
			want: []string{`Q1 blockquote "quoted" ^q1`, `Q2 blockquote "[!note] Title\nbody" ^q2`},
		},
		{
			name: "standalone marker after a table names its first cell",
			body: "# T\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n^table-1\n\nAfter.\n",
			want: []string{
				`S1 section "T\na\nb\n1\n2\n^table-1\nAfter."`,
				`S1T1R0C0 table-cell "a" ^table-1`,
				`S1T1R0C1 table-cell "b"`,
				`S1T1R1C0 table-cell "1"`,
				`S1T1R1C1 table-cell "2"`,
				`S1P1 paragraph "After."`,
			},
		},
		{
			name: "standalone marker after a list names its first item",
			body: "- one\n- two\n\n^list-1\n\nAfter.\n",
			want: []string{`L1 list-item "one" ^list-1`, `L2 list-item "two"`, `P1 paragraph "After."`},
		},
		{
			name: "standalone marker after a code block or quote",
			body: "```go\nx := 1\n```\n\n^code-1\n\n> quoted\n\n^quote-1\n",
			want: []string{"B1 code-block \"x := 1\\n\" ^code-1", `Q1 blockquote "quoted" ^quote-1`},
		},
		{
			name: "standalone marker after a paragraph",
			body: "Para.\n\n^para-1\n",
			want: []string{`P1 paragraph "Para." ^para-1`},
		},
		{
			name: "a unit keeps the id it ends with",
			body: "Para. ^own\n\n^next\n",
			want: []string{`P1 paragraph "Para." ^own`},
		},
		{
			name: "a list's first item keeps its own id over the list's",
			body: "- one ^item-1\n- two\n\n^list-1\n",
			want: []string{`L1 list-item "one" ^item-1`, `L2 list-item "two"`},
		},
		{
			name: "CRLF line endings",
			body: "Para. ^p1\r\n\r\n- one\r\n- two\r\n\r\n^list-1\r\n",
			want: []string{`P1 paragraph "Para." ^p1`, `L1 list-item "one" ^list-1`, `L2 list-item "two"`},
		},
		{
			name: "standalone marker with nothing before it names nothing",
			body: "^orphan\n\n# Heading\n\n^after-heading\n\n---\n\n^after-rule\n\nText.\n",
			want: []string{`S1 section "Heading\n^after-heading\n\n^after-rule\nText."`, `S1P1 paragraph "Text."`},
		},
		{
			name: "markers that are not block ids",
			body: "Use `x ^abc`\n\nfoo^abc\n\nunder_score ^a_b\n\n- ^only\n",
			want: []string{
				"P1 paragraph \"Use `x ^abc`\"",
				`P2 paragraph "foo^abc"`,
				`P3 paragraph "under_score ^a_b"`,
				`L1 list-item "^only"`,
			},
		},
	} {
		if got := blockView(test, tc.body); !slices.Equal(got, tc.want) {
			test.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
}

// A marker is not part of what a unit embeds: the unit hashes as its text
// without it, so naming a block costs no vector.
func TestParse_BlockIDLeavesTheContentHash(test *testing.T) {
	marked, markedErr := subunit.Parse([]byte("Some text. ^abc\n"))
	plain, plainErr := subunit.Parse([]byte("Some text.\n"))

	if markedErr != nil || plainErr != nil || len(marked) != 1 || len(plain) != 1 {
		test.Fatalf("Parse: %+v, %v; %+v, %v", marked, markedErr, plain, plainErr)
	}

	if marked[0].ContentHash != plain[0].ContentHash || marked[0].EmbedPayload != "Some text." || marked[0].Title != "Some text." {
		test.Errorf("marked unit = %+v, want the plain unit's hash, payload and title", marked[0])
	}
}
