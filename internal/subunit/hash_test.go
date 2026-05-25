package subunit

import (
	"strings"
	"testing"
)

func TestComputeHash_LengthIs12Hex(test *testing.T) {
	cases := []Unit{
		{Kind: KindParagraph, Text: "anything"},
		{Kind: KindSection, Text: "head", Properties: map[string]any{"heading-level": 1}},
		{Kind: KindCodeBlock, Text: "x", Properties: map[string]any{"lang": "go"}},
		{Kind: KindListItem, Text: "y", Properties: map[string]any{"checkbox": true}},
		{Kind: KindBlockquote, Text: "z"},
		{Kind: KindTableCell, Text: "c", Properties: map[string]any{"row": 1, "column": 0, "column-header": "Name"}},
	}

	for idx, unit := range cases {
		got := ComputeHash(unit)
		if len(got) != HashLength {
			test.Errorf("[%d] hash length: want %d, got %d (%q)", idx, HashLength, len(got), got)
		}
		for _, char := range got {
			if !strings.ContainsRune("0123456789abcdef", char) {
				test.Errorf("[%d] hash %q has non-hex rune %q", idx, got, char)
			}
		}
	}
}

func TestComputeHash_KindAffectsHash(test *testing.T) {
	a := ComputeHash(Unit{Kind: KindParagraph, Text: "same"})
	b := ComputeHash(Unit{Kind: KindBlockquote, Text: "same"})
	if a == b {
		test.Errorf("paragraph and blockquote with same text should hash differently, both got %q", a)
	}
}

func TestComputeHash_SectionHeadingLevelAffectsHash(test *testing.T) {
	a := ComputeHash(Unit{Kind: KindSection, Text: "head", Properties: map[string]any{"heading-level": 1}})
	b := ComputeHash(Unit{Kind: KindSection, Text: "head", Properties: map[string]any{"heading-level": 2}})
	if a == b {
		test.Errorf("H1 and H2 with same text should hash differently, both got %q", a)
	}
}

func TestComputeHash_ListItemCheckboxAffectsHash(test *testing.T) {
	bare := ComputeHash(Unit{Kind: KindListItem, Text: "todo"})
	checked := ComputeHash(Unit{Kind: KindListItem, Text: "todo", Properties: map[string]any{"checkbox": true}})
	unchecked := ComputeHash(Unit{Kind: KindListItem, Text: "todo", Properties: map[string]any{"checkbox": false}})

	if bare == checked || bare == unchecked || checked == unchecked {
		test.Errorf("none/checked/unchecked must differ: bare=%q checked=%q unchecked=%q", bare, checked, unchecked)
	}
}

func TestComputeHash_CodeBlockLangAffectsHash(test *testing.T) {
	a := ComputeHash(Unit{Kind: KindCodeBlock, Text: "x = 1", Properties: map[string]any{"lang": "go"}})
	b := ComputeHash(Unit{Kind: KindCodeBlock, Text: "x = 1", Properties: map[string]any{"lang": "py"}})
	if a == b {
		test.Errorf("different lang must hash differently, both got %q", a)
	}
}

func TestComputeHash_TableCellPropertiesAffectHash(test *testing.T) {
	base := Unit{Kind: KindTableCell, Text: "cell", Properties: map[string]any{"row": 1, "column": 0, "column-header": "Name"}}
	baseHash := ComputeHash(base)

	rowDiff := base
	rowDiff.Properties = map[string]any{"row": 2, "column": 0, "column-header": "Name"}
	if ComputeHash(rowDiff) == baseHash {
		test.Errorf("row index must affect hash")
	}

	colDiff := base
	colDiff.Properties = map[string]any{"row": 1, "column": 1, "column-header": "Name"}
	if ComputeHash(colDiff) == baseHash {
		test.Errorf("column index must affect hash")
	}

	headerDiff := base
	headerDiff.Properties = map[string]any{"row": 1, "column": 0, "column-header": "Status"}
	if ComputeHash(headerDiff) == baseHash {
		test.Errorf("column-header must affect hash")
	}
}

func TestResolveCollisions_TwoCollisions(test *testing.T) {
	units := []Unit{
		{Hash: "aaaaaaaaaaaa", Ordinal: 0},
		{Hash: "bbbbbbbbbbbb", Ordinal: 1},
		{Hash: "aaaaaaaaaaaa", Ordinal: 2},
	}
	ResolveCollisions(units)

	if units[0].Hash != "aaaaaaaaaaaa" {
		test.Errorf("[0] should keep bare hash, got %q", units[0].Hash)
	}
	if units[1].Hash != "bbbbbbbbbbbb" {
		test.Errorf("[1] unique hash should not change, got %q", units[1].Hash)
	}
	if units[2].Hash != "aaaaaaaaaaaa-1" {
		test.Errorf("[2] want %q, got %q", "aaaaaaaaaaaa-1", units[2].Hash)
	}
}

func TestResolveCollisions_ThreeCollisions(test *testing.T) {
	units := []Unit{
		{Hash: "ffffffffffff", Ordinal: 0},
		{Hash: "ffffffffffff", Ordinal: 1},
		{Hash: "ffffffffffff", Ordinal: 2},
	}
	ResolveCollisions(units)

	want := []string{"ffffffffffff", "ffffffffffff-1", "ffffffffffff-2"}
	for i, w := range want {
		if units[i].Hash != w {
			test.Errorf("[%d] want %q, got %q", i, w, units[i].Hash)
		}
	}
}

func TestResolveCollisions_MixedUniqueAndColliding(test *testing.T) {
	units := []Unit{
		{Hash: "111111111111", Ordinal: 0},
		{Hash: "222222222222", Ordinal: 1},
		{Hash: "111111111111", Ordinal: 2},
		{Hash: "333333333333", Ordinal: 3},
		{Hash: "222222222222", Ordinal: 4},
		{Hash: "111111111111", Ordinal: 5},
	}
	ResolveCollisions(units)

	want := []string{
		"111111111111",
		"222222222222",
		"111111111111-1",
		"333333333333",
		"222222222222-1",
		"111111111111-2",
	}
	for i, w := range want {
		if units[i].Hash != w {
			test.Errorf("[%d] want %q, got %q", i, w, units[i].Hash)
		}
	}
}

func TestResolveCollisions_OrderingByOrdinalNotSliceIndex(test *testing.T) {
	// Three colliding entries inserted in reverse ordinal order.
	units := []Unit{
		{Hash: "xxxxxxxxxxxx", Ordinal: 5},
		{Hash: "xxxxxxxxxxxx", Ordinal: 3},
		{Hash: "xxxxxxxxxxxx", Ordinal: 1},
	}
	ResolveCollisions(units)

	// The lowest-ordinal entry (Ordinal=1) gets the bare hash.
	// Find by ordinal and verify.
	byOrdinal := map[int]string{}
	for _, u := range units {
		byOrdinal[u.Ordinal] = u.Hash
	}

	if byOrdinal[1] != "xxxxxxxxxxxx" {
		test.Errorf("Ordinal=1 should keep bare hash, got %q", byOrdinal[1])
	}
	if byOrdinal[3] != "xxxxxxxxxxxx-1" {
		test.Errorf("Ordinal=3 should be -1, got %q", byOrdinal[3])
	}
	if byOrdinal[5] != "xxxxxxxxxxxx-2" {
		test.Errorf("Ordinal=5 should be -2, got %q", byOrdinal[5])
	}
}

func TestResolveCollisions_NoOpForSingleUnit(test *testing.T) {
	units := []Unit{{Hash: "abcabcabcabc", Ordinal: 0}}
	ResolveCollisions(units)
	if units[0].Hash != "abcabcabcabc" {
		test.Errorf("single unit should not get suffix, got %q", units[0].Hash)
	}
}
