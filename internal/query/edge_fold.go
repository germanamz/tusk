package query

import (
	"cmp"
	"slices"
	"strings"

	"github.com/germanamz/tusk/internal/index"
)

// A link written in a paragraph is stored once for its file and once for each
// sub-unit that holds it: the paragraph and every section around it. One link
// to a target was four rows in an edge listing, from the file, its two
// sections and the paragraph (#848). Edge views fold those copies onto the
// file's own row by default and keep the deepest sub-units that hold the link
// as its Units, so a caller can still jump to the passage. Stored edges stay
// as they are: graph expansion, filter arrows, the web graph and doctor read
// the sub-unit rows directly.

// FoldedEdge is one row of a folded edge view: the edge as its file stores it,
// plus the deepest of that file's sub-units that hold the same edge, in
// document order. Units is empty for an edge no sub-unit repeats, such as a
// frontmatter edge.
type FoldedEdge struct {
	index.EdgeRow
	Units []string `json:",omitempty"`
}

// foldSubUnitEdges folds every edge whose source is a sub-unit onto the edge
// of the same type and target from the sub-unit's file. It drops structural
// edges (a file's `contains` edges to its own sub-units), which only describe
// the sub-unit tree, unless keepStructural is set: a caller that filters on an
// edge type has asked for that type's rows by name, structural or not. A group
// keeps its file's row, or its first row moved onto
// the file when the file stores no copy. Groups come in the order of their
// file rows, then any file-less ones in the order first seen. Units are in
// input order; sortUnits puts them in document order.
func foldSubUnitEdges(rows []index.EdgeRow, keepStructural bool) []FoldedEdge {
	type groupKey struct {
		edgeType, fileID, targetID string
	}

	keyOf := func(row index.EdgeRow) groupKey {
		fileID, _ := splitSubUnitID(row.SourceID)

		return groupKey{row.Type, fileID, row.TargetID}
	}

	dropped := func(row index.EdgeRow) bool {
		return row.Kind == structuralEdgeKind && !keepStructural
	}

	var folded []FoldedEdge

	groupAt := map[groupKey]int{}

	for _, row := range rows {
		if dropped(row) || isSubUnitID(row.SourceID) {
			continue
		}

		groupAt[keyOf(row)] = len(folded)
		folded = append(folded, FoldedEdge{EdgeRow: row})
	}

	for _, row := range rows {
		if dropped(row) || !isSubUnitID(row.SourceID) {
			continue
		}

		key := keyOf(row)
		at, found := groupAt[key]

		if !found {
			moved := row
			moved.SourceID = key.fileID
			moved.Position = 0

			at = len(folded)
			groupAt[key] = at
			folded = append(folded, FoldedEdge{EdgeRow: moved})
		}

		folded[at].Units = append(folded[at].Units, row.SourceID)
	}

	for at := range folded {
		folded[at].Units = deepestUnits(folded[at].Units)
	}

	return folded
}

// structuralEdgeKind is the kind of the edges the sub-unit pipeline writes
// from a file to its own sub-units (index.EdgeRow.Kind).
const structuralEdgeKind = "structural"

// sortUnits puts each row's Units in document order by the sub-units'
// ordinals (their depth-first place in the file). A unit with no known
// ordinal keeps its place after the ones that have one.
func sortUnits(folded []FoldedEdge, ordinals map[string]int) {
	for _, row := range folded {
		slices.SortStableFunc(row.Units, func(left, right string) int {
			leftOrdinal, leftKnown := ordinals[left]
			rightOrdinal, rightKnown := ordinals[right]

			if leftKnown != rightKnown {
				if leftKnown {
					return -1
				}

				return 1
			}

			return cmp.Compare(leftOrdinal, rightOrdinal)
		})
	}
}

// unitIDs returns every sub-unit id the rows' Units name.
func unitIDs(folded []FoldedEdge) []string {
	var ids []string

	for _, row := range folded {
		ids = append(ids, row.Units...)
	}

	return ids
}

// deepestUnits drops each sub-unit that holds another one of ids, leaving the
// passages the link is written in: of a file's H1, H2 and the paragraph under
// them, only the paragraph. ids all belong to one file.
func deepestUnits(ids []string) []string {
	var kept []string

	for _, id := range ids {
		_, address := splitSubUnitID(id)

		holdsAnother := slices.ContainsFunc(ids, func(other string) bool {
			_, otherAddress := splitSubUnitID(other)

			return unitHolds(address, otherAddress)
		})

		if !holdsAnother {
			kept = append(kept, id)
		}
	}

	return kept
}

// unitHolds reports whether the sub-unit at address inner sits inside the one
// at outer. A section holds its subsections and leaves (S1.2 holds S1.2.1,
// S1.2P3 and S1.2T1R1C1), whose addresses extend its own. The byte after the
// shared prefix tells a child from a sibling that only shares leading digits
// (S1 and S12).
func unitHolds(outer, inner string) bool {
	if len(inner) <= len(outer) || !strings.HasPrefix(inner, outer) {
		return false
	}

	next := inner[len(outer)]

	return next < '0' || next > '9'
}

// splitSubUnitID returns the file id and the sub-unit address of id. address
// is "" for a file id: file ids never contain index.SubUnitIDSeparator.
func splitSubUnitID(id string) (string, string) {
	fileID, address, _ := strings.Cut(id, index.SubUnitIDSeparator)

	return fileID, address
}

// isSubUnitID reports whether id names a sub-unit rather than a file.
func isSubUnitID(id string) bool {
	return strings.Contains(id, index.SubUnitIDSeparator)
}
