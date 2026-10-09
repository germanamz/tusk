package query

import (
	"encoding/json"

	"strings"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
)

// MatchedUnit is a single sub-unit attached to a parent file row, either as
// a structural projection (include = units) or as a semantic-rank hit. When
// HasScore is false the Score and Snippet fields are absent from JSON output;
// see the matched_units MarshalJSON method.
//
// A semantic hit is a pointer an agent follows into the file, so every unit
// says where it is (StartLine/EndLine, emitted as `lines`) and a section says
// what it is (Heading).
type MatchedUnit struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	ParentID string `json:"parent_id,omitempty"`

	// Heading is a section row's heading text (its title column: markup
	// stripped, capped at 120 runes). Empty on leaves.
	Heading string `json:"heading,omitempty"`

	// HeadingLevel is populated only for section rows (1-6). 0 elsewhere;
	// the JSON marshaler omits the field for non-section rows.
	HeadingLevel int `json:"heading_level,omitempty"`

	// StartLine and EndLine are the 1-based, inclusive file lines the unit
	// covers; JSON emits them as `lines: [start, end]`. 0 when unknown (HTML
	// sub-units, or rows not yet renumbered by reindex), and `lines` is
	// omitted.
	StartLine int `json:"-"`
	EndLine   int `json:"-"`

	// Ordinal is the row's depth-first position within its parent file.
	Ordinal int `json:"ordinal"`

	// Score / Snippet are populated by the semantic path. HasScore
	// disambiguates "score is 0 by coincidence" from "score wasn't set".
	Score    float64 `json:"score,omitempty"`
	Snippet  string  `json:"snippet,omitempty"`
	HasScore bool    `json:"-"`

	// Explain-only score-trace fields, mirroring ScoredRow. Populated by
	// the sub-unit semantic path only when Request.Explain is true and
	// graph expansion ran. `omitempty` keeps the JSON shape byte-stable
	// for callers that don't opt into explain mode.
	CosineScore float64 `json:"cosine_score,omitempty"`
	GraphScore  float64 `json:"graph_score,omitempty"`
	FinalScore  float64 `json:"final_score,omitempty"`
	Distance    int     `json:"distance,omitempty"`
}

// MarshalJSON emits the score/snippet fields only when HasScore is true so
// structural-with-include=units rows have no score field per spec §5.7. The
// explain-trace fields use the same omitempty convention as ScoredRow.
func (unit MatchedUnit) MarshalJSON() ([]byte, error) {
	type alias struct {
		ID           string   `json:"id"`
		Type         string   `json:"type"`
		ParentID     string   `json:"parent_id,omitempty"`
		Heading      string   `json:"heading,omitempty"`
		HeadingLevel int      `json:"heading_level,omitempty"`
		Lines        []int    `json:"lines,omitempty"`
		Ordinal      int      `json:"ordinal"`
		Score        *float64 `json:"score,omitempty"`
		Snippet      string   `json:"snippet,omitempty"`
		CosineScore  float64  `json:"cosine_score,omitempty"`
		GraphScore   float64  `json:"graph_score,omitempty"`
		FinalScore   float64  `json:"final_score,omitempty"`
		Distance     int      `json:"distance,omitempty"`
	}

	out := alias{
		ID:           unit.ID,
		Type:         unit.Type,
		ParentID:     unit.ParentID,
		Heading:      unit.Heading,
		HeadingLevel: unit.HeadingLevel,
		Ordinal:      unit.Ordinal,
		Snippet:      unit.Snippet,
		CosineScore:  unit.CosineScore,
		GraphScore:   unit.GraphScore,
		FinalScore:   unit.FinalScore,
		Distance:     unit.Distance,
	}

	if unit.HasScore {
		score := unit.Score
		out.Score = &score
	}

	if unit.StartLine > 0 && unit.EndLine > 0 {
		out.Lines = []int{unit.StartLine, unit.EndLine}
	}

	return json.Marshal(out)
}

// LoadFileSubUnits returns the file's full sub-unit tree as MatchedUnits in
// depth-first order (sorted by ordinal). Score is unset; HasScore is false.
// Returns nil when the file has no sub-units. Used by the include = units
// structural path.
func LoadFileSubUnits(nodes *index.NodeRepo, fileID string) ([]MatchedUnit, error) {
	rows, listErr := nodes.ListSubUnitsForFile(fileID)

	if listErr != nil {
		return nil, listErr
	}

	if len(rows) == 0 {
		return nil, nil
	}

	out := make([]MatchedUnit, 0, len(rows))

	for _, row := range rows {
		unit := newMatchedUnit(row)
		unit.Snippet = filter.RenderSnippet(row.EmbedPayload.String, 200)

		out = append(out, unit)
	}

	return out, nil
}

// newMatchedUnit projects a sub-unit row's identity and position onto a
// MatchedUnit: id, type, parent, ordinal, line range, and for sections the
// heading text and level. Score and snippet are left to the caller.
func newMatchedUnit(row index.NodeRow) MatchedUnit {
	unit := MatchedUnit{
		ID:      row.ID,
		Type:    row.Type,
		Ordinal: int(row.Ordinal.Int64),
	}

	if row.ParentID.Valid {
		unit.ParentID = row.ParentID.String
	}

	if row.Type == "section" {
		unit.Heading = row.Title
		unit.HeadingLevel = readHeadingLevel(row.PropertiesJSON)
	}

	if row.StartLine.Valid && row.EndLine.Valid {
		unit.StartLine = int(row.StartLine.Int64)
		unit.EndLine = int(row.EndLine.Int64)
	}

	return unit
}

// capUnits keeps the first limit units (0 means no cap) and returns them with
// the count before the cap, which callers report as units_total so a reader
// can tell how many were cut.
func capUnits(units []MatchedUnit, limit int) ([]MatchedUnit, int) {
	total := len(units)

	if limit > 0 && total > limit {
		units = units[:limit]
	}

	return units, total
}

// unitsLimit resolves the per-file unit cap: an explicit max-units wins,
// otherwise fallback applies (0 = no cap).
func unitsLimit(maxUnits, fallback int) int {
	if maxUnits > 0 {
		return maxUnits
	}

	return fallback
}

// readHeadingLevel parses a section's properties JSON and returns its
// heading-level. Returns 0 when the property is missing or malformed.
func readHeadingLevel(propertiesJSON string) int {
	if propertiesJSON == "" {
		return 0
	}

	var props map[string]any

	if unmarshalErr := json.Unmarshal([]byte(propertiesJSON), &props); unmarshalErr != nil {
		return 0
	}

	value, present := props["heading-level"]

	if !present {
		return 0
	}

	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	case json.Number:
		level, convErr := typed.Int64()

		if convErr != nil {
			return 0
		}

		return int(level)
	}

	return 0
}

// fileIDFromSubUnit returns the file id portion of a sub-unit composite id
// of the form "<fileID>#<hash>". Sub-units always carry a '#'; file ids
// never do, so a missing '#' returns the id unchanged.
func fileIDFromSubUnit(id string) string {
	if idx := strings.IndexByte(id, '#'); idx > 0 {
		return id[:idx]
	}

	return id
}
