package query

import (
	"fmt"

	"github.com/germanamz/tusk/internal/index"
)

// EdgeListRequest configures EdgeListRun: the index listing's filters, and
// whether to list sub-unit edges as they are stored.
type EdgeListRequest struct {
	index.EdgeListRequest
	// SubUnits lists the stored rows as they are: one from every sub-unit
	// that holds a link, and each file's `contains` edges to its own
	// sub-units. False folds them (foldSubUnitEdges), keeping the `contains`
	// edges only when the request filters on an edge type. A From that names
	// a sub-unit is listed as stored either way.
	SubUnits bool
}

// EdgeListResult is the typed payload returned by EdgeListRun.
type EdgeListResult struct {
	Rows []FoldedEdge
}

// EdgeListRun is the canonical entry point for the `edge list` /
// `tusk_edge_list` verb and the alias dispatcher. It lists edges with
// index.EdgeListRun and, unless the request asks for sub-unit edges, folds
// each sub-unit's copy of a link onto its file's row, with the passages that
// hold the link as Units in document order (nodes supplies their ordinals).
func EdgeListRun(edges *index.EdgeRepo, nodes *index.NodeRepo, req EdgeListRequest) (*EdgeListResult, error) {
	listRequest := req.EdgeListRequest
	fold := !req.SubUnits && !isSubUnitID(listRequest.From)

	// A file's folded rows need its sub-units' copies to find their Units.
	listRequest.FromSubUnits = fold

	listed, listErr := index.EdgeListRun(edges, listRequest)

	if listErr != nil {
		return nil, listErr
	}

	if !fold {
		rows := make([]FoldedEdge, 0, len(listed.Rows))

		for _, row := range listed.Rows {
			rows = append(rows, FoldedEdge{EdgeRow: row})
		}

		return &EdgeListResult{Rows: rows}, nil
	}

	folded := foldSubUnitEdges(listed.Rows, listRequest.Type != "" || listRequest.TypeRef != nil)

	if ids := unitIDs(folded); len(ids) > 0 {
		unitRows, unitsErr := nodes.ListByIDs(ids)

		if unitsErr != nil {
			return nil, fmt.Errorf("edge list: read sub-unit order: %w", unitsErr)
		}

		ordinals := make(map[string]int, len(unitRows))

		for _, row := range unitRows {
			if row.Ordinal.Valid {
				ordinals[row.ID] = int(row.Ordinal.Int64)
			}
		}

		sortUnits(folded, ordinals)
	}

	return &EdgeListResult{Rows: folded}, nil
}
