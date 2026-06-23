package query

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
)

// MatchedUnit is a single sub-unit attached to a parent file row, either as
// a structural projection (include = units) or as a semantic-rank hit. When
// HasScore is false the Score and Snippet fields are absent from JSON output;
// see the matched_units MarshalJSON method.
type MatchedUnit struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	ParentID string `json:"parent_id,omitempty"`

	// HeadingLevel is populated only for section rows (1-6). 0 elsewhere;
	// the JSON marshaler omits the field for non-section rows.
	HeadingLevel int `json:"heading_level,omitempty"`

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
		HeadingLevel int      `json:"heading_level,omitempty"`
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

	return json.Marshal(out)
}

// headingWeights are the fixed §5.7 multipliers used to aggregate a
// section's score from its best descendant leaf score. Index 0 is unused;
// headings are 1-indexed.
var headingWeights = [7]float64{0, 1.00, 0.85, 0.70, 0.55, 0.40, 0.25}

// HeadingWeight returns the §5.7 multiplier for a heading level. Out-of-
// range levels return 0 so callers never multiply by an undefined weight.
func HeadingWeight(level int) float64 {
	if level < 1 || level > 6 {
		return 0
	}

	return headingWeights[level]
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
		unit := MatchedUnit{
			ID:      row.ID,
			Type:    row.Type,
			Ordinal: int(row.Ordinal.Int64),
		}

		if row.ParentID.Valid {
			unit.ParentID = row.ParentID.String
		}

		if row.Type == "section" {
			unit.HeadingLevel = readHeadingLevel(row.PropertiesJSON)
		}

		unit.Snippet = filter.RenderSnippet(row.EmbedPayload.String, 200)

		out = append(out, unit)
	}

	return out, nil
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

// groupSubUnitsByFile builds an index of every sub-unit row keyed by its
// file id, plus a slice of section rows per file in document order. Used by
// the semantic path to look up descendants when aggregating section scores.
type subUnitIndex struct {
	// rowsByID is every loaded sub-unit row keyed by its composite id.
	rowsByID map[string]index.NodeRow
	// childrenByParent maps a parent id (file or section) to its
	// immediate child sub-unit ids in ordinal order.
	childrenByParent map[string][]string
}

func newSubUnitIndex(rows []index.NodeRow) *subUnitIndex {
	idx := &subUnitIndex{
		rowsByID:         make(map[string]index.NodeRow, len(rows)),
		childrenByParent: make(map[string][]string, len(rows)),
	}

	for _, row := range rows {
		idx.rowsByID[row.ID] = row

		if row.ParentID.Valid {
			idx.childrenByParent[row.ParentID.String] = append(idx.childrenByParent[row.ParentID.String], row.ID)
		}
	}

	for parentID := range idx.childrenByParent {
		ids := idx.childrenByParent[parentID]
		sort.SliceStable(ids, func(left, right int) bool {
			return idx.rowsByID[ids[left]].Ordinal.Int64 < idx.rowsByID[ids[right]].Ordinal.Int64
		})
	}

	return idx
}

// bestLeafUnder returns, in a single descendant walk, both the maximum leaf
// score among the section's descendants and the id of the leaf achieving it.
// leafScores is the map of scored leaf ids → cosine score. Sections are
// skipped as scoring candidates (they're aggregated separately) but recursed
// into. Returns ("", 0, false) when no descendant leaf was scored. On a score
// tie the first leaf in depth-first (ordinal) order wins, matching the prior
// separate bestLeafScoreUnder / bestDescendantLeafID walks.
func (idx *subUnitIndex) bestLeafUnder(sectionID string, leafScores map[string]float64) (string, float64, bool) {
	var (
		bestID    string
		bestScore float64
		found     bool
	)

	var walk func(id string)
	walk = func(id string) {
		for _, childID := range idx.childrenByParent[id] {
			child, ok := idx.rowsByID[childID]

			if !ok {
				continue
			}

			if child.Type == "section" {
				walk(childID)

				continue
			}

			score, scored := leafScores[childID]

			if !scored {
				continue
			}

			if !found || score > bestScore {
				bestID = childID
				bestScore = score
				found = true
			}
		}
	}

	walk(sectionID)

	return bestID, bestScore, found
}

// firstLeafSnippet returns the embed_payload of the first descendant leaf
// (depth-first, ordinal-ordered). When preferredID is set and resolves to a
// scored leaf, its body is preferred over the document-order first leaf.
// Returns empty when the section has no descendant leaf.
func (idx *subUnitIndex) firstLeafSnippet(sectionID, preferredID string) string {
	if preferredID != "" {
		if row, ok := idx.rowsByID[preferredID]; ok && row.Type != "section" {
			return row.EmbedPayload.String
		}
	}

	for _, childID := range idx.childrenByParent[sectionID] {
		child, ok := idx.rowsByID[childID]

		if !ok {
			continue
		}

		if child.Type == "section" {
			nested := idx.firstLeafSnippet(childID, "")

			if nested != "" {
				return nested
			}

			continue
		}

		if child.EmbedPayload.String != "" {
			return child.EmbedPayload.String
		}
	}

	return ""
}
