package subunit

import (
	"fmt"
	"strings"
	"testing"
)

func TestSectionPathNesting(t *testing.T) {
	src := []byte("# A\npara\n## B\npara\n### C\npara\n## D\npara\n# E\npara\n")
	units, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{ // address -> first-line title
		"S1":       "A",
		"S1P1":     "para",
		"S1.1":     "B",
		"S1.1P1":   "para",
		"S1.1.1":   "C",
		"S1.1.1P1": "para",
		"S1.2":     "D",
		"S1.2P1":   "para",
		"S2":       "E",
		"S2P1":     "para",
	}

	got := map[string]string{}
	for _, u := range units {
		got[u.Address] = u.Title
	}

	for addr, title := range want {
		if got[addr] != title {
			t.Errorf("address %s: got title %q, want %q (all: %v)", addr, got[addr], title, got)
		}
	}
}

func TestRootLevelLeavesHaveNoSection(t *testing.T) {
	units, err := Parse([]byte("first para\n\nsecond para\n"))
	if err != nil {
		t.Fatal(err)
	}

	if units[0].Address != "P1" || units[1].Address != "P2" {
		t.Fatalf("root paragraphs: got %q,%q want P1,P2", units[0].Address, units[1].Address)
	}
}

func TestLeafKindAddresses(t *testing.T) {
	src := []byte("## Sec\n\npara one\n\n```go\ncode\n```\n\n> quote\n\n- item a\n- item b\n\n| h1 | h2 |\n| -- | -- |\n| a  | b  |\n")
	units, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}

	got := map[Kind][]string{}
	for _, u := range units {
		got[u.Kind] = append(got[u.Kind], u.Address)
	}

	assertEq := func(k Kind, want ...string) {
		if fmt.Sprint(got[k]) != fmt.Sprint(want) {
			t.Errorf("%s addresses: got %v want %v", k, got[k], want)
		}
	}

	assertEq(KindParagraph, "S1P1")
	assertEq(KindCodeBlock, "S1B1")
	assertEq(KindBlockquote, "S1Q1")
	assertEq(KindListItem, "S1L1", "S1L2")
	assertEq(KindTableCell, "S1T1R0C0", "S1T1R0C1", "S1T1R1C0", "S1T1R1C1")
}

func TestDuplicateContentDistinctAddressSameContentHash(t *testing.T) {
	src := []byte("- dup\n- dup\n")
	units, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}

	if units[0].Address == units[1].Address {
		t.Fatalf("duplicate list items must get distinct addresses: both %q", units[0].Address)
	}

	if units[0].ContentHash != units[1].ContentHash {
		t.Fatalf("duplicate content must share a content_hash: %q vs %q", units[0].ContentHash, units[1].ContentHash)
	}
}

func TestLeafContentHashIsEmbedPayloadHash(t *testing.T) {
	units, err := Parse([]byte("## H\n\nhello\n"))
	if err != nil {
		t.Fatal(err)
	}

	for _, u := range units {
		if u.Kind == KindParagraph {
			if u.ContentHash == "" || strings.ContainsRune(u.ContentHash, '#') {
				t.Fatalf("paragraph content_hash should be a bare hex hash, got %q", u.ContentHash)
			}
		}
	}
}
