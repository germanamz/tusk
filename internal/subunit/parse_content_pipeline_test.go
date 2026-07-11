package subunit

import (
	"strings"
	"testing"
)

// TestParse_CRLFStableHashesAndNoCarriageReturn pins the documented
// CRLF-stability contract (see internal/subunit/hash.go): a file that differs
// from another only in line endings must produce byte-identical sub-unit
// content hashes, and no raw carriage return may leak into a stored payload.
// Regression guard for #682 item 3.
func TestParse_CRLFStableHashesAndNoCarriageReturn(test *testing.T) {
	lfSrc := []byte("# Head\n\nalpha\nbeta\n")
	crlfSrc := []byte("# Head\r\n\r\nalpha\r\nbeta\r\n")

	lfUnits, lfErr := Parse(lfSrc)
	if lfErr != nil {
		test.Fatalf("parse LF: %v", lfErr)
	}

	crlfUnits, crlfErr := Parse(crlfSrc)
	if crlfErr != nil {
		test.Fatalf("parse CRLF: %v", crlfErr)
	}

	if len(lfUnits) != len(crlfUnits) {
		test.Fatalf("unit count differs across EOL: LF=%d CRLF=%d\nLF:\n%sCRLF:\n%s",
			len(lfUnits), len(crlfUnits), dumpUnits(lfUnits), dumpUnits(crlfUnits))
	}

	for idx := range lfUnits {
		if lfUnits[idx].ContentHash != crlfUnits[idx].ContentHash {
			test.Errorf("[%d] content hash differs across EOL: LF=%q CRLF=%q (text LF=%q CRLF=%q)",
				idx, lfUnits[idx].ContentHash, crlfUnits[idx].ContentHash, lfUnits[idx].Text, crlfUnits[idx].Text)
		}

		if strings.ContainsRune(crlfUnits[idx].EmbedPayload, '\r') {
			test.Errorf("[%d] CRLF payload leaked a carriage return: %q", idx, crlfUnits[idx].EmbedPayload)
		}

		if strings.ContainsRune(crlfUnits[idx].Text, '\r') {
			test.Errorf("[%d] CRLF text leaked a carriage return: %q", idx, crlfUnits[idx].Text)
		}
	}
}

// TestParse_LooseListItemIncludesContinuationParagraph guards #682 item 4: a
// loose list item's continuation paragraph must be carried by the list-item
// leaf so it is embedded and semantically findable, instead of being stranded
// in the section-only text that is never embedded.
func TestParse_LooseListItemIncludesContinuationParagraph(test *testing.T) {
	src := []byte("# List\n\n- headline point\n\n  unique-phrase-xyz elaboration paragraph\n")

	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	var item *Unit

	for i := range units {
		if units[i].Kind == KindListItem {
			item = &units[i]

			break
		}
	}

	if item == nil {
		test.Fatalf("no list-item unit emitted\n%s", dumpUnits(units))
	}

	if !strings.Contains(item.Text, "headline point") {
		test.Errorf("list-item text missing headline: %q", item.Text)
	}

	if !strings.Contains(item.Text, "unique-phrase-xyz elaboration paragraph") {
		test.Errorf("list-item text missing continuation paragraph: %q", item.Text)
	}

	if !strings.Contains(item.EmbedPayload, "unique-phrase-xyz elaboration paragraph") {
		test.Errorf("list-item payload missing continuation paragraph: %q", item.EmbedPayload)
	}
}

// TestParse_EmptyBulletEmitsNoUnit guards #682 item 5: a bare "- " bullet
// carries no queryable or embeddable content, so it must not emit a list-item
// node (which would embed empty and trip doctor's embed-no-chunks forever).
func TestParse_EmptyBulletEmitsNoUnit(test *testing.T) {
	src := []byte("# Checklist\n\n- first real item\n- \n- \n- last real item\n")

	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	var items []Unit

	for _, unit := range units {
		if unit.Kind == KindListItem {
			items = append(items, unit)
		}
	}

	if len(items) != 2 {
		test.Fatalf("want 2 list-item units (empty bullets skipped), got %d\n%s",
			len(items), dumpUnits(units))
	}

	for _, item := range items {
		if item.EmbedPayload == "" {
			test.Errorf("emitted a list-item with empty payload: %+v", item)
		}
	}
}

// TestParse_EmptyBulletStillEmitsNestedBlock guards #682 item 5: skipping an
// empty scaffolding bullet must not drop a nested block it wraps — the nested
// block is still emitted as a peer unit under the enclosing section.
func TestParse_EmptyBulletStillEmitsNestedBlock(test *testing.T) {
	src := []byte("# H\n\n- \n  - nested item\n")

	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	var items []Unit

	for _, unit := range units {
		if unit.Kind == KindListItem {
			items = append(items, unit)
		}
	}

	if len(items) != 1 {
		test.Fatalf("want only the nested item emitted, got %d\n%s", len(items), dumpUnits(units))
	}

	if items[0].Text != "nested item" {
		test.Errorf("nested item text = %q, want %q", items[0].Text, "nested item")
	}
}

// TestParse_EmptyCheckboxBulletKeepsCheckboxButEmptyPayload guards #682 item 5:
// an empty "- [ ]" item still carries a queryable checkbox property, so it
// stays a node — but with an empty payload that the embed/doctor accounting
// must treat as non-embeddable rather than a permanent failure.
func TestParse_EmptyCheckboxBulletKeepsCheckboxButEmptyPayload(test *testing.T) {
	src := []byte("# Checklist\n\n- [ ] \n")

	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	var item *Unit

	for i := range units {
		if units[i].Kind == KindListItem {
			item = &units[i]

			break
		}
	}

	if item == nil {
		test.Fatalf("empty checkbox item should still emit a node\n%s", dumpUnits(units))
	}

	if _, ok := item.Properties["checkbox"]; !ok {
		test.Errorf("checkbox property missing: %+v", item.Properties)
	}

	if item.EmbedPayload != "" {
		test.Errorf("want empty payload for a text-less checkbox item, got %q", item.EmbedPayload)
	}
}
