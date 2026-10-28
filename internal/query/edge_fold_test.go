package query

import (
	"database/sql"
	"slices"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestUnitHolds(test *testing.T) {
	cases := []struct {
		outer, inner string
		want         bool
	}{
		{"S1", "S1.1", true},
		{"S1", "S1P1", true},
		{"S1", "S1.2T1R1C1", true},
		{"S1.2", "S1.2.1P3", true},
		{"S1", "S12", false},
		{"S1", "S10P1", false},
		{"S1.1", "S1.10", false},
		{"P1", "P10", false},
		{"S1", "S1", false},
		{"S1.1", "S1", false},
	}

	for _, testCase := range cases {
		if got := unitHolds(testCase.outer, testCase.inner); got != testCase.want {
			test.Errorf("unitHolds(%q, %q) = %v, want %v", testCase.outer, testCase.inner, got, testCase.want)
		}
	}
}

func TestDeepestUnits(test *testing.T) {
	got := deepestUnits([]string{"src#S1", "src#S1.1", "src#S1.1P1", "src#S1P1", "src#S12P1"})
	want := []string{"src#S1.1P1", "src#S1P1", "src#S12P1"}

	if !slices.Equal(got, want) {
		test.Errorf("deepestUnits = %v, want %v", got, want)
	}
}

// TestFoldSubUnitEdges pins #848 finding 4: the file's row stands for its
// sub-units' copies of the same link and keeps the deepest of them as Units;
// a copy whose file stores none still comes out as one row from the file; the
// file's structural `contains` edges are dropped unless asked for by type.
func TestFoldSubUnitEdges(test *testing.T) {
	structural := sql.NullString{String: "markdown", Valid: true}
	rows := []index.EdgeRow{
		{Type: "contains", SourceID: "src", TargetID: "src#S1", SourcePath: "src.md", Kind: "structural", Source: structural},
		{Type: "describes", SourceID: "src", TargetID: "target", SourcePath: "src.md", Kind: "direct", Position: 1},
		{Type: "references", SourceID: "src", TargetID: "target", SourcePath: "src.md", Kind: "direct", Position: 1},
		{Type: "references", SourceID: "src#S1", TargetID: "target", SourcePath: "src.md", Kind: "direct"},
		{Type: "references", SourceID: "src#S1.1", TargetID: "target", SourcePath: "src.md", Kind: "direct"},
		{Type: "references", SourceID: "src#S1.1P1", TargetID: "target", SourcePath: "src.md", Kind: "direct"},
		{Type: "references", SourceID: "src#S1P1", TargetID: "target", SourcePath: "src.md", Kind: "direct"},
		{Type: "references", SourceID: "other#P2", TargetID: "target", SourcePath: "other.md", Kind: "direct"},
	}

	folded := foldSubUnitEdges(rows, false)

	type shape struct {
		edgeType, source, target string
		position                 int
		units                    []string
	}

	var got []shape

	for _, row := range folded {
		got = append(got, shape{row.Type, row.SourceID, row.TargetID, row.Position, row.Units})
	}

	want := []shape{
		{"describes", "src", "target", 1, nil},
		{"references", "src", "target", 1, []string{"src#S1.1P1", "src#S1P1"}},
		{"references", "other", "target", 0, []string{"other#P2"}},
	}

	if len(got) != len(want) {
		test.Fatalf("folded = %+v, want %+v", got, want)
	}

	for at := range want {
		if got[at].edgeType != want[at].edgeType || got[at].source != want[at].source ||
			got[at].target != want[at].target || got[at].position != want[at].position ||
			!slices.Equal(got[at].units, want[at].units) {
			test.Errorf("folded[%d] = %+v, want %+v", at, got[at], want[at])
		}
	}

	if kept := foldSubUnitEdges(rows, true); len(kept) != 4 || kept[0].Type != "contains" {
		test.Errorf("with keepStructural, folded = %+v, want the contains edge first and three more", kept)
	}
}

func TestSortUnits_DocumentOrder(test *testing.T) {
	folded := []FoldedEdge{{Units: []string{"src#S1.1P1", "src#S1.2L1", "src#S9", "src#S1P1"}}}

	sortUnits(folded, map[string]int{"src#S1P1": 2, "src#S1.1P1": 4, "src#S1.2L1": 6})

	want := []string{"src#S1P1", "src#S1.1P1", "src#S1.2L1", "src#S9"}

	if !slices.Equal(folded[0].Units, want) {
		test.Errorf("units = %v, want %v (by ordinal, unknown last)", folded[0].Units, want)
	}
}
