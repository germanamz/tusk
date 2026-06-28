package subunit

// FinalizeUnit fills the derived fields every parser must set on a Unit before
// appending it: the ordinal, the parent-section address, the embed payload
// (defaulting to Text), the title, the structural hash and content hash, and —
// for leaves that did not pre-set one — the per-kind leaf address from ctx.
//
// It is shared by the markdown walker (internal/subunit) and the HTML walker
// (internal/htmlunit) so their finalization stays byte-identical: ContentHashFor
// feeds the content-addressed dedup, so any drift between the two parsers would
// split or collide vectors. ordinal is the unit's position in the output slice
// (its slice index before appending); ctx supplies the section stack and the
// per-kind leaf-address counters (LeafAddress mutates those counters, so this
// must be called exactly once per emitted unit, in document order).
func FinalizeUnit(unit Unit, ctx *WalkCtx, ordinal int) Unit {
	unit.Ordinal = ordinal
	unit.ParentAddress = ctx.CurrentAddress()

	if unit.EmbedPayload == "" {
		unit.EmbedPayload = unit.Text
	}

	unit.Title = makeTitle(unit.Text)
	unit.Hash = ComputeHash(unit)
	unit.ContentHash = ContentHashFor(unit)

	if unit.Address == "" {
		unit.Address = ctx.LeafAddress(unit.Kind)
	}

	return unit
}

// TableCellPayload builds the embed payload for a table cell: a body cell whose
// column has a header embeds as "<column-header>: <cell-text>" so the header
// gives the value context; a cell with no column header embeds as its bare text.
// Shared by both parsers because the payload feeds ContentHashFor — it must be
// identical across the markdown and HTML table walkers or the same cell content
// would hash differently.
func TableCellPayload(columnHeader, cellText string) string {
	if columnHeader == "" {
		return cellText
	}

	return columnHeader + ": " + cellText
}
