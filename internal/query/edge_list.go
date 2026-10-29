package query

import (
	"fmt"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/typeref"
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
	// EdgeTypes is the manifest's edge types. When set, a type that is an
	// inverse label lists the edge type it names with From and To swapped
	// (resolveInverseType), and a type the manifest doesn't declare is an
	// error when nothing is listed, instead of an empty result. The index can
	// still hold edges of a type tusk.toml dropped, and those still list.
	EdgeTypes manifest.EdgeTypes
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
	undeclared := ""

	if req.EdgeTypes != nil {
		undeclared = resolveInverseType(&listRequest, req.EdgeTypes)
	}

	fold := !req.SubUnits && !isSubUnitID(listRequest.From)

	// A file's folded rows need its sub-units' copies to find their Units.
	listRequest.FromSubUnits = fold

	listed, listErr := index.EdgeListRun(edges, listRequest)

	if listErr != nil {
		return nil, listErr
	}

	if undeclared != "" && len(listed.Rows) == 0 {
		return nil, fmt.Errorf("edge type %q %w", undeclared, manifest.ErrEdgeTypeNotDeclared)
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

// resolveInverseType rewrites a listing's type when it is an inverse label: the
// listing asks for the edge type the label names, with From and To swapped, so
// `--type depended-on-by --from c` lists the depends-on edges into c. The rows
// keep the edge type they are stored under. It returns the type's name when it
// is neither a declared edge type nor an inverse label, and "" otherwise.
func resolveInverseType(request *index.EdgeListRequest, edgeTypes manifest.EdgeTypes) string {
	name := request.Type

	if request.TypeRef != nil {
		name = request.TypeRef.Type
	} else if ref, parseErr := typeref.Parse(name); parseErr == nil {
		name = ref.Type
	}

	if _, declared := edgeTypes[name]; declared || name == "" {
		return ""
	}

	forward, isInverse := manifest.InverseOf(edgeTypes, name)

	if !isInverse {
		return name
	}

	if request.TypeRef != nil {
		ref := *request.TypeRef
		ref.Type = forward
		request.TypeRef = &ref
	} else {
		request.Type = forward
	}

	request.From, request.To = request.To, request.From

	return ""
}
