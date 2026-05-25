package query

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// EdgeRef is a single edge attached to an expanded row. Direction is "out"
// when the row owns the source side of the edge and "in" when the row is the
// target. TargetTitle is the title of the other side of the edge (looked up
// via the nodes table); it is empty when the other side is unknown to the
// index (a dangling target).
type EdgeRef struct {
	Type        string `json:"type"`
	Direction   string `json:"direction"`
	TargetID    string `json:"target_id"`
	TargetTitle string `json:"target_title,omitempty"`
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
}

// Any reports whether the set requests at least one expansion.
func (set IncludeSet) Any() bool {
	return set.Body || set.Edges || set.Properties || set.Units
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
		default:
			return IncludeSet{}, fmt.Errorf("unknown include %q (valid: body, edges, properties, units)", token)
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

			var properties map[string]any

			if unmarshalErr := json.Unmarshal([]byte(raw), &properties); unmarshalErr != nil {
				return fmt.Errorf("expand: parse properties for %s: %w", row.rowID(), unmarshalErr)
			}

			row.setProperties(properties)
		}
	}

	if set.Edges {
		if db == nil {
			return fmt.Errorf("expand: edges requested but db is nil")
		}

		if loadErr := loadEdgesForRows(rows, db); loadErr != nil {
			return loadErr
		}
	}

	return nil
}

// loadEdgesForRows fetches outgoing and incoming edges for every row in a
// single SQL query, then stitches them onto each row.
func loadEdgesForRows(rows []rowLike, db *sql.DB) error {
	ids := make([]string, 0, len(rows))
	rowByID := make(map[string]rowLike, len(rows))

	for _, row := range rows {
		id := row.rowID()
		ids = append(ids, id)
		rowByID[id] = row
	}

	placeholders := strings.Repeat("?,", len(ids))
	placeholders = strings.TrimRight(placeholders, ",")

	queryText := fmt.Sprintf(`
		SELECT e.source_id, e.target_id, e.type,
		       src.title AS source_title, tgt.title AS target_title
		FROM edges e
		LEFT JOIN nodes src ON src.id = e.source_id
		LEFT JOIN nodes tgt ON tgt.id = e.target_id
		WHERE e.source_id IN (%s) OR e.target_id IN (%s)
		ORDER BY e.source_id, e.type, e.target_id
	`, placeholders, placeholders)

	args := make([]any, 0, 2*len(ids))

	for _, id := range ids {
		args = append(args, id)
	}

	for _, id := range ids {
		args = append(args, id)
	}

	queryRows, queryErr := db.Query(queryText, args...)

	if queryErr != nil {
		return fmt.Errorf("expand: query edges: %w", queryErr)
	}

	defer queryRows.Close()

	outBucket := make(map[string][]EdgeRef, len(ids))
	inBucket := make(map[string][]EdgeRef, len(ids))

	for queryRows.Next() {
		var (
			sourceID, targetID, edgeType string
			sourceTitle, targetTitle     sql.NullString
		)

		if scanErr := queryRows.Scan(&sourceID, &targetID, &edgeType, &sourceTitle, &targetTitle); scanErr != nil {
			return fmt.Errorf("expand: scan edge: %w", scanErr)
		}

		if _, ownsSource := rowByID[sourceID]; ownsSource {
			outBucket[sourceID] = append(outBucket[sourceID], EdgeRef{
				Type:        edgeType,
				Direction:   "out",
				TargetID:    targetID,
				TargetTitle: targetTitle.String,
			})
		}

		if _, ownsTarget := rowByID[targetID]; ownsTarget {
			inBucket[targetID] = append(inBucket[targetID], EdgeRef{
				Type:        edgeType,
				Direction:   "in",
				TargetID:    sourceID,
				TargetTitle: sourceTitle.String,
			})
		}
	}

	if rowsErr := queryRows.Err(); rowsErr != nil {
		return fmt.Errorf("expand: edges iter: %w", rowsErr)
	}

	for id, row := range rowByID {
		combined := append([]EdgeRef{}, outBucket[id]...)
		combined = append(combined, inBucket[id]...)

		sort.SliceStable(combined, func(left, right int) bool {
			if combined[left].Direction != combined[right].Direction {
				return combined[left].Direction < combined[right].Direction
			}

			if combined[left].Type != combined[right].Type {
				return combined[left].Type < combined[right].Type
			}

			return combined[left].TargetID < combined[right].TargetID
		})

		if len(combined) > 0 {
			row.setEdges(combined)
		}
	}

	return nil
}
