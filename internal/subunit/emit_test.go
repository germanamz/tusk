package subunit

import "testing"

// TestTableCellPayload pins the shared C5 helper: a header prefixes the cell
// text; no header leaves it bare.
func TestTableCellPayload(test *testing.T) {
	if got := TableCellPayload("Owner", "Alice"); got != "Owner: Alice" {
		test.Errorf("TableCellPayload(header) = %q, want %q", got, "Owner: Alice")
	}

	if got := TableCellPayload("", "Alice"); got != "Alice" {
		test.Errorf("TableCellPayload(no header) = %q, want %q", got, "Alice")
	}
}

// TestFinalizeUnit_DefaultsAndHashes confirms FinalizeUnit fills the derived
// fields: ordinal, EmbedPayload defaulted from Text, title, content hash, and a
// leaf address from ctx.
func TestFinalizeUnit_DefaultsAndHashes(test *testing.T) {
	ctx := NewWalkCtx()

	unit := FinalizeUnit(Unit{Kind: KindParagraph, Text: "hello world"}, ctx, 3)

	if unit.Ordinal != 3 {
		test.Errorf("Ordinal = %d, want 3", unit.Ordinal)
	}

	if unit.EmbedPayload != "hello world" {
		test.Errorf("EmbedPayload = %q, want default to Text", unit.EmbedPayload)
	}

	if unit.Title != "hello world" {
		test.Errorf("Title = %q, want %q", unit.Title, "hello world")
	}

	if unit.ContentHash == "" {
		test.Error("ContentHash should be set")
	}

	if unit.Address == "" {
		test.Error("leaf Address should be assigned from ctx")
	}
}

// TestFinalizeUnit_PreservesPresetAddressAndPayload confirms a unit that already
// set Address (sections, table cells) and EmbedPayload (table cells) keeps them.
func TestFinalizeUnit_PreservesPresetAddressAndPayload(test *testing.T) {
	ctx := NewWalkCtx()

	unit := FinalizeUnit(Unit{
		Kind:         KindTableCell,
		Address:      "T1R0C0",
		Text:         "Alice",
		EmbedPayload: "Owner: Alice",
	}, ctx, 0)

	if unit.Address != "T1R0C0" {
		test.Errorf("Address = %q, want preset T1R0C0 preserved", unit.Address)
	}

	if unit.EmbedPayload != "Owner: Alice" {
		test.Errorf("EmbedPayload = %q, want preset preserved", unit.EmbedPayload)
	}
}
