package index

import "fmt"

// EdgeListRequest configures EdgeListRun. All three filter fields are
// optional, but the CLI (cmd_edge_list) requires at least one to be set;
// the MCP handler falls back to ListAll when none are provided.
type EdgeListRequest struct {
	From string
	To   string
	Type string
	// RequireFilter, when true, makes EdgeListRun return an error if none of
	// From/To/Type are set. The CLI sets this; the MCP handler does not.
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

		return &EdgeListResult{Rows: narrowEdgeRows(rows, req.To, req.Type)}, nil

	case req.To != "":
		rows, listErr := repo.ListByTarget(req.To)

		if listErr != nil {
			return nil, listErr
		}

		return &EdgeListResult{Rows: narrowEdgeRows(rows, "", req.Type)}, nil

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

// narrowEdgeRows filters rows by optional target and type. Mirrors the
// previous cmd_edge_list.narrow helper.
func narrowEdgeRows(rows []EdgeRow, toID, edgeType string) []EdgeRow {
	var out []EdgeRow

	for _, row := range rows {
		if toID != "" && row.TargetID != toID {
			continue
		}

		if edgeType != "" && row.Type != edgeType {
			continue
		}

		out = append(out, row)
	}

	return out
}
