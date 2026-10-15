package query

import (
	"encoding/json"
	"math"
)

// scoreScale rounds wire scores to three decimals. Ranking and ties use the
// full value; three decimals are all a reader compares, and a full float64
// costs an agent four times the bytes (#813).
const scoreScale = 1000

// RoundScore rounds a score to the three decimals JSON output carries.
func RoundScore(score float64) float64 {
	return math.Round(score*scoreScale) / scoreScale
}

// WireSnippet is the row-level snippet JSON output carries. A row with
// matched units leaves it out, since it is always the first unit's snippet;
// only a file that ranked by its file-level vector, with no units, shows one.
func (row ScoredRow) WireSnippet() string {
	if len(row.MatchedUnits) > 0 {
		return ""
	}

	return row.Snippet
}

// WireUnits returns MatchedUnits as JSON output carries them. When the row
// carries Body (include=body on a row whose top unit has no Text), that is
// the first unit's best leaf, so the first unit's snippet, a window of it, is
// left out. The row's own slice is not modified: the compact and table views
// still print every snippet.
func (row ScoredRow) WireUnits() []MatchedUnit {
	if row.Body == "" || len(row.MatchedUnits) == 0 {
		return row.MatchedUnits
	}

	units := make([]MatchedUnit, len(row.MatchedUnits))
	copy(units, row.MatchedUnits)
	units[0].Snippet = ""

	return units
}

// MarshalJSON writes the row with its wire snippet and units and its scores
// rounded, so `tusk query --json`, `tusk run` and the MCP tools agree.
func (row ScoredRow) MarshalJSON() ([]byte, error) {
	// plain has ScoredRow's fields and tags but not this method, so the
	// Marshal below doesn't recurse.
	type plain ScoredRow

	out := plain(row)
	out.Score = RoundScore(row.Score)
	out.Snippet = row.WireSnippet()
	out.MatchedUnits = row.WireUnits()
	out.CosineScore = RoundScore(row.CosineScore)
	out.LexicalScore = RoundScore(row.LexicalScore)
	out.GraphScore = RoundScore(row.GraphScore)
	out.FinalScore = RoundScore(row.FinalScore)

	return json.Marshal(out)
}
