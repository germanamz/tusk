package query

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/germanamz/tusk/internal/index"
)

// EdgeRef is a single edge attached to an expanded row. Direction is "out"
// when the row owns the source side of the edge and "in" when the row is the
// target. TargetTitle is the title of the other side of the edge (looked up
// via the nodes table); it is empty when the other side is unknown to the
// index (a dangling target).
//
// Units, in the default edge view, names the deepest sub-units that hold the
// edge, in document order: the file row's own when Direction is "out", the
// other file's when it is "in" (foldSubUnitEdges). position is the order the
// file writes an out edge's targets in, which the view sorts by.
type EdgeRef struct {
	Type        string   `json:"type"`
	Direction   string   `json:"direction"`
	TargetID    string   `json:"target_id"`
	TargetTitle string   `json:"target_title,omitempty"`
	Units       []string `json:"units,omitempty"`

	position int
}

// IncludeSet is the parsed, deduplicated set of include flags requested by
// the caller. ParseInclude builds it from a raw []string.
type IncludeSet struct {
	Body       bool
	Edges      bool
	Properties bool
	// Units, when true, requests that each returned file row carry its
	// full sub-unit list (depth-first, ordered by ordinal) in
	// MatchedUnits. No scoring is performed in the structural path —
	// each MatchedUnit's HasScore is false. Workspaces with sub-units
	// disabled silently drop the flag (MatchedUnits stays empty).
	Units bool
	// Paths, when true, attaches to each file row the workspace paths its
	// page names (see LoadPaths). Sub-unit rows get none.
	Paths bool
}

// Any reports whether the set requests at least one expansion.
func (set IncludeSet) Any() bool {
	return set.Body || set.Edges || set.Properties || set.Units || set.Paths
}

// ParseInclude parses a list of include tokens into an IncludeSet. Unknown
// tokens are rejected with an error that names the allowed values. The input
// list is treated as a set: duplicates are tolerated. Empty input yields an
// empty set with no error.
func ParseInclude(raw []string) (IncludeSet, error) {
	var set IncludeSet

	for _, token := range raw {
		switch token {
		case "":
			continue
		case "body":
			set.Body = true
		case "edges":
			set.Edges = true
		case "properties":
			set.Properties = true
		case "units":
			set.Units = true
		case "paths":
			set.Paths = true
		default:
			return IncludeSet{}, fmt.Errorf("unknown include %q (valid: body, edges, properties, units, paths)", token)
		}
	}

	return set, nil
}

// IncludeFromFields derives an IncludeSet from a list of field names. A field
// whose name matches an expandable column triggers the corresponding include
// flag; other field names are ignored (they project but do not expand).
func IncludeFromFields(fields []string) IncludeSet {
	var set IncludeSet

	for _, name := range fields {
		switch name {
		case "body":
			set.Body = true
		case "edges":
			set.Edges = true
		case "properties":
			set.Properties = true
		case "units", "matched_units":
			set.Units = true
		case "paths":
			set.Paths = true
		}
	}

	return set
}

// MergeInclude returns the union of two IncludeSets.
func MergeInclude(left, right IncludeSet) IncludeSet {
	return IncludeSet{
		Body:       left.Body || right.Body,
		Edges:      left.Edges || right.Edges,
		Properties: left.Properties || right.Properties,
		Units:      left.Units || right.Units,
		Paths:      left.Paths || right.Paths,
	}
}

// rowLike captures the minimum surface ExpandListRows needs to decorate a
// row regardless of which concrete row type (ListRow, Row) is used.
type rowLike interface {
	rowID() string
	rowPath() string
	rowPropertiesRaw() string
	setBody(string)
	setProperties(map[string]any)
	setEdges([]EdgeRef)
}

func (row *ListRow) rowID() string            { return row.ID }
func (row *ListRow) rowPath() string          { return row.Path }
func (row *ListRow) rowPropertiesRaw() string { return row.PropertiesRaw }
func (row *ListRow) setBody(value string)     { row.Body = value }
func (row *ListRow) setProperties(value map[string]any) {
	row.Properties = value
}
func (row *ListRow) setEdges(value []EdgeRef) { row.Edges = value }

func (row *Row) rowID() string            { return row.ID }
func (row *Row) rowPath() string          { return row.Path }
func (row *Row) rowPropertiesRaw() string { return row.PropertiesRaw }
func (row *Row) setBody(value string)     { row.Body = value }
func (row *Row) setProperties(value map[string]any) {
	row.Properties = value
}
func (row *Row) setEdges(value []EdgeRef) { row.Edges = value }

// ExpandListRows decorates rows in place with body / edges / properties
// per set. workspaceRoot is the absolute path used to resolve a row's
// workspace-relative Path. db is required for edge expansion; pass nil when
// set.Edges is false.
func ExpandListRows(rows []ListRow, set IncludeSet, workspaceRoot string, db *sql.DB) error {
	if len(rows) == 0 || !set.Any() {
		return nil
	}

	likes := make([]rowLike, len(rows))

	for index := range rows {
		likes[index] = &rows[index]
	}

	return expandRowLikes(likes, set, workspaceRoot, db)
}

// ExpandRows is the Row-shaped (query.Run structural path) twin of
// ExpandListRows.
func ExpandRows(rows []Row, set IncludeSet, workspaceRoot string, db *sql.DB) error {
	if len(rows) == 0 || !set.Any() {
		return nil
	}

	likes := make([]rowLike, len(rows))

	for index := range rows {
		likes[index] = &rows[index]
	}

	return expandRowLikes(likes, set, workspaceRoot, db)
}

func expandRowLikes(rows []rowLike, set IncludeSet, workspaceRoot string, db *sql.DB) error {
	if set.Body {
		for _, row := range rows {
			content, readErr := os.ReadFile(filepath.Join(workspaceRoot, row.rowPath()))

			if readErr != nil {
				if os.IsNotExist(readErr) {
					continue
				}

				return fmt.Errorf("expand: read %s: %w", row.rowPath(), readErr)
			}

			row.setBody(string(content))
		}
	}

	if set.Properties {
		for _, row := range rows {
			raw := row.rowPropertiesRaw()

			if raw == "" {
				continue
			}

			properties, propErr := unmarshalProperties(raw, row.rowID())

			if propErr != nil {
				return propErr
			}

			row.setProperties(properties)
		}
	}

	if set.Edges {
		if db == nil {
			return fmt.Errorf("expand: edges requested but db is nil")
		}

		if loadErr := loadEdgesForRows(rows, db, EdgeOptions{}); loadErr != nil {
			return loadErr
		}
	}

	return nil
}

// unmarshalProperties decodes a row's stored properties JSON into a map. An
// empty string yields (nil, nil); a decode failure is wrapped with the row id.
// Shared by the structural expansion path and both semantic ranking paths.
func unmarshalProperties(raw, id string) (map[string]any, error) {
	if raw == "" {
		return nil, nil
	}

	var properties map[string]any

	if unmarshalErr := json.Unmarshal([]byte(raw), &properties); unmarshalErr != nil {
		return nil, fmt.Errorf("expand: parse properties for %s: %w", id, unmarshalErr)
	}

	return properties, nil
}

// EdgeOptions shapes the edges a node view carries. The zero value is the
// default view, which `query --include edges` always uses; `node get` sets the
// fields from its edge flags.
type EdgeOptions struct {
	// Type keeps only edges of this type; "" keeps every type.
	Type string
	// Direction keeps only "in" or "out" edges; "" or "both" keeps both.
	// node.GetRun rejects any other value.
	Direction string
	// SubUnits lists the stored rows as they are, like
	// EdgeListRequest.SubUnits: an edge from every sub-unit that holds a
	// link, and a file's `contains` edges to its own sub-units. False folds
	// them (foldSubUnitEdges), keeping the `contains` edges only when Type
	// is set.
	SubUnits bool
}

// keeps reports whether edge passes the Type and Direction filters.
func (options EdgeOptions) keeps(edge EdgeRef) bool {
	if options.Type != "" && edge.Type != options.Type {
		return false
	}

	switch options.Direction {
	case "in", "out":
		return edge.Direction == options.Direction
	}

	return true
}

// singleEdgeRow adapts a bare node id to rowLike so LoadEdgesForNode can reuse
// loadEdgesForRows' both-direction projection. Only rowID and setEdges are
// exercised; the body/properties setters are inert.
type singleEdgeRow struct {
	id    string
	edges []EdgeRef
}

func (row *singleEdgeRow) rowID() string                { return row.id }
func (row *singleEdgeRow) rowPath() string              { return "" }
func (row *singleEdgeRow) rowPropertiesRaw() string     { return "" }
func (row *singleEdgeRow) setBody(string)               {}
func (row *singleEdgeRow) setProperties(map[string]any) {}
func (row *singleEdgeRow) setEdges(value []EdgeRef)     { row.edges = value }

// LoadEdgesForNode returns the both-direction edges attached to a single node
// id, shaped by options, using the same edges-table projection as
// `tusk_query --include edges` (EdgeRef with in/out direction and target
// titles). It exists so the `node get` verb — whose service reads only the
// nodes table and cannot import this package (an embed→node→query cycle) —
// can hydrate edges the way the query path already does. Returns an empty
// slice when the node has no edges; db must be non-nil.
func LoadEdgesForNode(db *sql.DB, nodeID string, options EdgeOptions) ([]EdgeRef, error) {
	if db == nil {
		return nil, fmt.Errorf("query: LoadEdgesForNode requires a non-nil db")
	}

	row := &singleEdgeRow{id: nodeID}

	if loadErr := loadEdgesForRows([]rowLike{row}, db, options); loadErr != nil {
		return nil, loadErr
	}

	return row.edges, nil
}

// FileEdges returns edges without the ones whose other end is a sub-unit (an
// id carrying '#'): a file's `contains` edges to its own sections and
// paragraphs, and links to or from another file's sub-units, which repeat
// that file's own file-level link. It clears the Units of the edges it keeps
// too: the passages a link is written in are sub-unit detail. Returns nil when
// no edge is left.
func FileEdges(edges []EdgeRef) []EdgeRef {
	var kept []EdgeRef

	for _, edge := range edges {
		if !strings.Contains(edge.TargetID, "#") {
			edge.Units = nil
			kept = append(kept, edge)
		}
	}

	return kept
}

// edgeQueryChunk is how many rows one loadEdgesForRows query covers. A file
// row binds four variables (its id twice and its sub-unit id range), so a
// chunk stays far under SQLite's SQLITE_MAX_VARIABLE_NUMBER (32766).
const edgeQueryChunk = 2000

// loadEdgesForRows fetches outgoing and incoming edges for every row, one SQL
// query per edgeQueryChunk rows, then stitches them onto each row.
//
// In the default view (options.SubUnits false) a file row's out edges also
// take in the edges of its own sub-units, and both directions fold sub-unit
// sources onto their file (foldSubUnitEdges): an in edge from another file's
// paragraph reads as an edge from that file, with the paragraph in Units. A
// sub-unit row's out edges are its own and are not folded.
func loadEdgesForRows(rows []rowLike, db *sql.DB, options EdgeOptions) error {
	for start := 0; start < len(rows); start += edgeQueryChunk {
		if loadErr := loadEdgesForChunk(rows[start:min(start+edgeQueryChunk, len(rows))], db, options); loadErr != nil {
			return loadErr
		}
	}

	return nil
}

// loadEdgesForChunk is loadEdgesForRows for one chunk of rows. An edge between
// rows of two chunks is fetched by both queries, but each query only files it
// under the rows of its own chunk, so no edge is listed twice.
func loadEdgesForChunk(rows []rowLike, db *sql.DB, options EdgeOptions) error {
	ids := make([]string, 0, len(rows))
	rowByID := make(map[string]rowLike, len(rows))

	for _, row := range rows {
		id := row.rowID()
		ids = append(ids, id)
		rowByID[id] = row
	}

	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	where := fmt.Sprintf(`e.source_id IN (%s) OR e.target_id IN (%s)`, placeholders, placeholders)
	args := make([]any, 0, 4*len(ids))

	for range 2 {
		for _, id := range ids {
			args = append(args, id)
		}
	}

	if !options.SubUnits {
		for _, id := range ids {
			if isSubUnitID(id) {
				continue
			}

			lo, hi := index.SubUnitIDRange(id)
			where += ` OR (e.source_id >= ? AND e.source_id < ?)`
			args = append(args, lo, hi)
		}
	}

	queryRows, queryErr := db.Query(`
		SELECT e.type, e.source_id, e.target_id, e.source_path, e.kind, e.position,
		       src.title AS source_title, tgt.title AS target_title, src.ordinal AS source_ordinal
		FROM edges e
		LEFT JOIN nodes src ON src.id = e.source_id
		LEFT JOIN nodes tgt ON tgt.id = e.target_id
		WHERE `+where+`
		ORDER BY e.source_id, e.type, e.position, e.target_id
	`, args...)

	if queryErr != nil {
		return fmt.Errorf("expand: query edges: %w", queryErr)
	}

	defer queryRows.Close()

	titles := map[string]string{}
	ordinals := map[string]int{}
	outBucket := make(map[string][]index.EdgeRow, len(ids))
	inBucket := make(map[string][]index.EdgeRow, len(ids))

	for queryRows.Next() {
		var (
			edge                     index.EdgeRow
			position, sourceOrdinal  sql.NullInt64
			sourceTitle, targetTitle sql.NullString
		)

		if scanErr := queryRows.Scan(
			&edge.Type, &edge.SourceID, &edge.TargetID, &edge.SourcePath, &edge.Kind, &position,
			&sourceTitle, &targetTitle, &sourceOrdinal,
		); scanErr != nil {
			return fmt.Errorf("expand: scan edge: %w", scanErr)
		}

		edge.Position = int(position.Int64)
		titles[edge.SourceID] = sourceTitle.String
		titles[edge.TargetID] = targetTitle.String

		if sourceOrdinal.Valid {
			ordinals[edge.SourceID] = int(sourceOrdinal.Int64)
		}

		if _, ownsSource := rowByID[edge.SourceID]; ownsSource {
			outBucket[edge.SourceID] = append(outBucket[edge.SourceID], edge)
		}

		if fileID, _ := splitSubUnitID(edge.SourceID); !options.SubUnits && fileID != edge.SourceID {
			if _, ownsFile := rowByID[fileID]; ownsFile {
				outBucket[fileID] = append(outBucket[fileID], edge)
			}
		}

		if _, ownsTarget := rowByID[edge.TargetID]; ownsTarget {
			inBucket[edge.TargetID] = append(inBucket[edge.TargetID], edge)
		}
	}

	if rowsErr := queryRows.Err(); rowsErr != nil {
		return fmt.Errorf("expand: edges iter: %w", rowsErr)
	}

	for id, row := range rowByID {
		// A sub-unit row's out edges are its own; only a file's take in copies.
		foldOut := !options.SubUnits && !isSubUnitID(id)
		foldIn := !options.SubUnits

		var combined []EdgeRef

		for _, folded := range edgeView(outBucket[id], foldOut, options.Type != "", ordinals) {
			combined = append(combined, EdgeRef{
				Type:        folded.Type,
				Direction:   "out",
				TargetID:    folded.TargetID,
				TargetTitle: titles[folded.TargetID],
				Units:       folded.Units,
				position:    folded.Position,
			})
		}

		for _, folded := range edgeView(inBucket[id], foldIn, options.Type != "", ordinals) {
			combined = append(combined, EdgeRef{
				Type:        folded.Type,
				Direction:   "in",
				TargetID:    folded.SourceID,
				TargetTitle: titles[folded.SourceID],
				Units:       folded.Units,
			})
		}

		combined = slices.DeleteFunc(combined, func(edge EdgeRef) bool { return !options.keeps(edge) })

		sort.SliceStable(combined, func(left, right int) bool {
			return edgeRefLess(combined[left], combined[right])
		})

		if len(combined) > 0 {
			row.setEdges(combined)
		}
	}

	return nil
}

// edgeView returns bucket folded (foldSubUnitEdges, Units in document order by
// ordinals) when fold is set, and as stored otherwise.
func edgeView(bucket []index.EdgeRow, fold, keepStructural bool, ordinals map[string]int) []FoldedEdge {
	if fold {
		folded := foldSubUnitEdges(bucket, keepStructural)
		sortUnits(folded, ordinals)

		return folded
	}

	view := make([]FoldedEdge, 0, len(bucket))

	for _, edge := range bucket {
		view = append(view, FoldedEdge{EdgeRow: edge})
	}

	return view
}

// edgeRefLess orders a row's edges: in before out, then by type, then an out
// edge by the order its file writes its targets in (a target with no recorded
// position after the rest) and every edge by the id at its other end.
func edgeRefLess(left, right EdgeRef) bool {
	if left.Direction != right.Direction {
		return left.Direction < right.Direction
	}

	if left.Type != right.Type {
		return left.Type < right.Type
	}

	if left.position != right.position {
		if left.position == 0 || right.position == 0 {
			return right.position == 0
		}

		return left.position < right.position
	}

	return left.TargetID < right.TargetID
}
