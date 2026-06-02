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

func TestDisambiguateFallbackIDs_NoSuffixForAddressedUnits(test *testing.T) {
	// Two list items with identical text get distinct positional addresses
	// (L1, L2) and must NOT receive a -N hash suffix.
	units, err := Parse([]byte("- dup\n- dup\n"))
	if err != nil {
		test.Fatal(err)
	}

	for _, unit := range units {
		if strings.ContainsRune(unit.Address, '-') {
			test.Errorf("addressed unit must not carry -N suffix: %q", unit.Address)
		}
	}
}

func TestDisambiguateFallbackIDs_SuffixesCollidingFallbackHashes(test *testing.T) {
	// Units with no structural address fall back to their content hash;
	// later duplicates get -1, -2, …
	units := []Unit{
		{Hash: "aaaaaaaaaaaa"},
		{Hash: "aaaaaaaaaaaa"},
		{Hash: "aaaaaaaaaaaa"},
	}

	disambiguateFallbackIDs(units)

	want := []string{"aaaaaaaaaaaa", "aaaaaaaaaaaa-1", "aaaaaaaaaaaa-2"}
	for idx, expected := range want {
		if units[idx].Hash != expected {
			test.Errorf("[%d] want %q, got %q", idx, expected, units[idx].Hash)
		}
	}
}
