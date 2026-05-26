package index

import (
	"fmt"

	"github.com/germanamz/tusk/internal/typeref"
)

// EdgeListRequest configures EdgeListRun. All three filter fields are
// optional, but the CLI (cmd_edge_list) requires at least one to be set;
// the MCP handler falls back to ListAll when none are provided.
//
// TypeRef is the scope-aware sibling of Type. When set it takes precedence
// over Type and is matched against both `edges.type` and `edges.source` per
// the parsed Scope. Callers that already parse user input at the boundary
// (the MCP handler does) populate TypeRef; legacy callers (the CLI, the
// alias dispatcher) continue setting Type.
type EdgeListRequest struct {
	From    string
	To      string
	Type    string
	TypeRef *typeref.Ref
	// RequireFilter, when true, makes EdgeListRun return an error if none of
	// From/To/Type/TypeRef are set. The CLI sets this; the MCP handler does not.
	RequireFilter bool
}

// EdgeListResult is the typed payload returned by EdgeListRun.
type EdgeListResult struct {
	Rows []EdgeRow
}

// EdgeListRun is the canonical entry point for the `edge list` /
// `tusk_edge_list` verb. It collapses the three "list by X" lookups behind a
// single signature so both Cobra and MCP can share one code path.
func EdgeListRun(repo *EdgeRepo, req EdgeListRequest) (*EdgeListResult, error) {
	switch {
	case req.From != "":
		rows, listErr := repo.ListBySource(req.From)

		if listErr != nil {
			return nil, listErr
		}

		return &EdgeListResult{Rows: narrowEdgeRows(rows, req.To, req.Type, req.TypeRef)}, nil

	case req.To != "":
		rows, listErr := repo.ListByTarget(req.To)

		if listErr != nil {
			return nil, listErr
		}

		return &EdgeListResult{Rows: narrowEdgeRows(rows, "", req.Type, req.TypeRef)}, nil

	case req.TypeRef != nil:
		rows, listErr := repo.ListByEdgeRef(*req.TypeRef)

		if listErr != nil {
			return nil, listErr
		}

		return &EdgeListResult{Rows: rows}, nil

	case req.Type != "":
		rows, listErr := repo.ListByType(req.Type)

		if listErr != nil {
			return nil, listErr
		}

		return &EdgeListResult{Rows: rows}, nil
	}

	if req.RequireFilter {
		return nil, fmt.Errorf("specify at least one of --from, --to, --type")
	}

	rows, listErr := repo.ListAll()

	if listErr != nil {
		return nil, listErr
	}

	return &EdgeListResult{Rows: rows}, nil
}

// narrowEdgeRows filters rows by optional target and either a raw type
// string or a parsed type ref. ref takes precedence when non-nil.
func narrowEdgeRows(rows []EdgeRow, toID, edgeType string, ref *typeref.Ref) []EdgeRow {
	var out []EdgeRow

	for _, row := range rows {
		if toID != "" && row.TargetID != toID {
			continue
		}

		switch {
		case ref != nil:
			if !edgeRowMatchesRef(row, *ref) {
				continue
			}
		case edgeType != "":
			if row.Type != edgeType {
				continue
			}
		}

		out = append(out, row)
	}

	return out
}

// edgeRowMatchesRef returns true when row's (type, source) matches ref's
// scope semantics: ScopeAny ignores source, ScopeUser requires source IS
// NULL, ScopeSource requires source equality.
func edgeRowMatchesRef(row EdgeRow, ref typeref.Ref) bool {
	if row.Type != ref.Type {
		return false
	}

	switch ref.Scope {
	case typeref.ScopeAny:
		return true
	case typeref.ScopeUser:
		return !row.Source.Valid
	case typeref.ScopeSource:
		return row.Source.Valid && row.Source.String == ref.Source
	}

	return false
}
