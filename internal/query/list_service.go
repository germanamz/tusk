package query

import (
	"database/sql"
	"fmt"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/manifest"
)

// ListRequest configures ListRun. Filter is the filter-expression string
// (empty string matches all rows). Sort, Take, and Skip mirror the CLI flags.
//
// Phase 1 Task 3 will add Include, Fields, Format here; field set is stable
// so callers do not have to be rewired again.
type ListRequest struct {
	Filter string
	Sort   string
	Take   int
	Skip   int
}

// ListResult is the typed payload returned by ListRun.
type ListResult struct {
	Rows []ListRow
}

// ListRow is the per-row projection ListRun returns. Includes the index
// metadata fields the CLI table renderer reads off the scan today (the MCP
// handler ignores them) so the result struct is a strict superset of both
// callers' needs.
type ListRow struct {
	ID            string
	Type          string
	Path          string
	Title         string
	PropertiesRaw string
	LastMtime     int64
	LastSize      int64
	LastChecksum  string
}

// ListRun is the canonical entry point for the `node list` / `tusk_node_list`
// verb. It parses req.Filter as a filter expression, compiles it to SQL, and
// returns the matching rows. Both Cobra and MCP build a ListRequest and call
// this function; the caller is responsible for rendering the result.
//
// Lives in internal/query rather than internal/node so that `node` does not
// pull in the filter package (which imports embed → node, creating a cycle).
func ListRun(database *sql.DB, loadedManifest *manifest.Manifest, req ListRequest) (*ListResult, error) {
	expr, parseErrs := filter.NewParser(req.Filter).Parse()

	if len(parseErrs) > 0 {
		return nil, fmt.Errorf("filter parse: %v", parseErrs[0])
	}

	validateErrs := filter.Validate(expr, *loadedManifest)

	if len(validateErrs) > 0 {
		return nil, fmt.Errorf("filter validate: %v", validateErrs[0])
	}

	sortKeys, sortErr := filter.ParseSort(req.Sort)

	if sortErr != nil {
		return nil, sortErr
	}

	sqlQuery, params, compileErr := filter.Compile(expr, filter.CompileOptions{
		SortKeys: sortKeys,
		Take:     req.Take,
		Skip:     req.Skip,
	})

	if compileErr != nil {
		return nil, compileErr
	}

	rows, queryErr := database.Query(sqlQuery, params...)

	if queryErr != nil {
		return nil, queryErr
	}

	defer rows.Close()

	result := &ListResult{}

	for rows.Next() {
		var row ListRow

		if scanErr := rows.Scan(&row.ID, &row.Type, &row.Path, &row.Title, &row.PropertiesRaw, &row.LastMtime, &row.LastSize, &row.LastChecksum); scanErr != nil {
			return nil, scanErr
		}

		result.Rows = append(result.Rows, row)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}

	return result, nil
}
