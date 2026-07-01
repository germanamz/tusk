package query

import (
	"database/sql"
	"fmt"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/manifest"
)

// listRowScanColumns is the column set the structural list path reads from
// the filter compiler. It must stay in sync with filter.Compile's SELECT.
// We keep parent_id last to match the compiler's ordering.

// ListRequest configures ListRun. Filter is the filter-expression string
// (empty string matches all rows). Sort, Take, and Skip mirror the CLI flags.
//
// Include selects per-row expansions: any of "body", "edges", "properties".
// Fields, when set, names the columns the renderer should project; expandable
// field names (body, edges, properties) also imply their include flag so a
// caller can pass `fields=[id,title,body]` without separately requesting
// `include=body`. WorkspaceRoot is the absolute path used to resolve a row's
// Path when Include contains "body"; the caller is responsible for providing
// it (the service has no workspace abstraction).
type ListRequest struct {
	Filter        string
	Sort          string
	Take          int
	Skip          int
	Include       []string
	Fields        []string
	WorkspaceRoot string

	// StructuralDefaultTake caps the result when Take is 0. The CLI leaves it
	// at 0 (no cap, matching `tusk node list`'s historical "return all rows");
	// the MCP handler sets it to bound tool responses. Ignored when Take > 0.
	StructuralDefaultTake int
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
	ID            string `json:"id"`
	Type          string `json:"type"`
	Path          string `json:"path"`
	Title         string `json:"title"`
	PropertiesRaw string `json:"-"`
	LastMtime     int64  `json:"-"`
	LastSize      int64  `json:"-"`
	LastChecksum  string `json:"-"`

	// Populated only when Include / Fields requested the matching expansion.
	Body       string         `json:"body,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Edges      []EdgeRef      `json:"edges,omitempty"`
}

// ListRun is the canonical entry point for the `node list` / `tusk_node_list`
// verb. It parses req.Filter as a filter expression, compiles it to SQL, and
// returns the matching rows. Both Cobra and MCP build a ListRequest and call
// this function; the caller is responsible for rendering the result.
//
// Lives in internal/query rather than internal/node so that `node` does not
// pull in the filter package (which imports embed → node, creating a cycle).
func ListRun(database *sql.DB, loadedManifest *manifest.Manifest, req ListRequest) (*ListResult, error) {
	effectiveTake := req.Take

	if effectiveTake <= 0 {
		effectiveTake = req.StructuralDefaultTake
	}

	rows, compileErr := compileAndQuery(database, loadedManifest, req.Filter, req.Sort, effectiveTake, req.Skip)

	if compileErr != nil {
		return nil, compileErr
	}

	defer rows.Close()

	result := &ListResult{}

	for rows.Next() {
		var (
			row      ListRow
			parentID sql.NullString
		)

		if scanErr := rows.Scan(&row.ID, &row.Type, &row.Path, &row.Title, &row.PropertiesRaw, &row.LastMtime, &row.LastSize, &row.LastChecksum, &parentID); scanErr != nil {
			return nil, scanErr
		}

		_ = parentID // ListRow has no ParentID field today; reserved for symmetry with query.Run.

		result.Rows = append(result.Rows, row)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}

	includeSet, parseIncludeErr := ParseInclude(req.Include)

	if parseIncludeErr != nil {
		return nil, parseIncludeErr
	}

	includeSet = MergeInclude(includeSet, IncludeFromFields(req.Fields))

	if expandErr := ExpandListRows(result.Rows, includeSet, req.WorkspaceRoot, database); expandErr != nil {
		return nil, expandErr
	}

	return result, nil
}

// compileAndQuery parses and validates the structural filter, compiles it to
// SQL with the given sort/take/skip, and runs it against the index. It returns
// the OPEN *sql.Rows — the caller owns the result set and must defer
// rows.Close(). The error chain (parse, validate, sort, compile, query) is the
// exact order both Run and ListRun relied on before this was hoisted, with
// identical error text.
func compileAndQuery(database *sql.DB, loadedManifest *manifest.Manifest, filterStr, sortStr string, take, skip int) (*sql.Rows, error) {
	expr, parseErrs := filter.NewParser(filterStr).Parse()

	if len(parseErrs) > 0 {
		return nil, fmt.Errorf("filter parse: %v", parseErrs[0])
	}

	validateErrs := filter.Validate(expr, *loadedManifest)

	if len(validateErrs) > 0 {
		return nil, fmt.Errorf("filter validate: %v", validateErrs[0])
	}

	sortKeys, sortErr := filter.ParseSort(sortStr)

	if sortErr != nil {
		return nil, sortErr
	}

	// Resolve each sort key's declared type so ORDER BY compares an enum
	// property by declared order rather than lexically by its stored name.
	filter.ResolveSortKeys(expr, *loadedManifest, sortKeys)

	sqlQuery, params, compileErr := filter.Compile(expr, filter.CompileOptions{
		SortKeys: sortKeys,
		Take:     take,
		Skip:     skip,
	})

	if compileErr != nil {
		return nil, compileErr
	}

	rows, queryErr := database.Query(sqlQuery, params...)

	if queryErr != nil {
		return nil, queryErr
	}

	return rows, nil
}
