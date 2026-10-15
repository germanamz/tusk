package query_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/query"
)

// jsonObject marshals value and decodes it back into a generic object, so a
// test can check exactly which keys the wire shape carries.
func jsonObject(test *testing.T, value any) map[string]any {
	test.Helper()

	encoded, marshalErr := json.Marshal(value)

	if marshalErr != nil {
		test.Fatalf("marshal: %v", marshalErr)
	}

	var object map[string]any

	if decodeErr := json.Unmarshal(encoded, &object); decodeErr != nil {
		test.Fatalf("decode %s: %v", encoded, decodeErr)
	}

	return object
}

func sortedKeys(object map[string]any) string {
	keys := make([]string, 0, len(object))

	for key := range object {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	return strings.Join(keys, ",")
}

// TestMatchedUnit_SemanticHitJSONIsLean pins #813 item 2: a scored unit
// carries what an agent follows (id, heading, lines, score, snippet) and
// nothing else, with the score rounded to three decimals.
func TestMatchedUnit_SemanticHitJSONIsLean(test *testing.T) {
	unit := jsonObject(test, query.MatchedUnit{
		ID: "docs/ledger#S1.2", Type: "section", ParentID: "docs/ledger#S1",
		Heading: "Balances", HeadingLevel: 2, StartLine: 11, EndLine: 13, Ordinal: 3,
		Score: 0.8576513793832418, HasScore: true, Snippet: "Balances are derived from entries.",
	})

	if got := sortedKeys(unit); got != "heading,id,lines,score,snippet" {
		test.Errorf("semantic unit keys = %s, want heading,id,lines,score,snippet", got)
	}

	if unit["score"] != 0.858 {
		test.Errorf("score = %v, want 0.858", unit["score"])
	}
}

// TestMatchedUnit_OutlineJSONKeepsStructure: the structural include=units
// outline still carries the fields a caller rebuilds the tree from.
func TestMatchedUnit_OutlineJSONKeepsStructure(test *testing.T) {
	unit := jsonObject(test, query.MatchedUnit{
		ID: "docs/ledger#S1.2", Type: "section", ParentID: "docs/ledger#S1",
		Heading: "Balances", HeadingLevel: 2, StartLine: 11, EndLine: 13, Ordinal: 3,
		Snippet: "Balances",
	})

	if got := sortedKeys(unit); got != "heading,heading_level,id,lines,ordinal,parent_id,snippet,type" {
		test.Errorf("outline unit keys = %s", got)
	}
}

// TestMatchedUnit_ExplainTraceIsRounded: the explain trace rounds like the
// score it explains.
func TestMatchedUnit_ExplainTraceIsRounded(test *testing.T) {
	unit := jsonObject(test, query.MatchedUnit{
		ID: "docs/ledger#S1.2", Score: 0.85355339, HasScore: true,
		CosineScore: 0.70710678, LexicalScore: 0.33333333, GraphScore: 0.12345678, FinalScore: 0.85355339, Distance: 1,
	})

	want := map[string]float64{"score": 0.854, "cosine_score": 0.707, "lexical_score": 0.333, "graph_score": 0.123, "final_score": 0.854, "distance": 1}

	for key, value := range want {
		if unit[key] != value {
			test.Errorf("%s = %v, want %v", key, unit[key], value)
		}
	}
}

// TestScoredRow_JSONOmitsSnippetTheUnitsRepeat pins #813 item 1: a row with
// matched units drops its own snippet, which was always the first unit's.
func TestScoredRow_JSONOmitsSnippetTheUnitsRepeat(test *testing.T) {
	result := runLedger(test, query.Request{Semantic: "balances"})
	row := jsonObject(test, result.Semantic.Ranked[0])

	if _, present := row["snippet"]; present {
		test.Errorf("row carries a snippet beside its matched units: %v", row)
	}

	units, _ := row["matched_units"].([]any)

	if len(units) != 2 {
		test.Fatalf("matched_units = %v, want 2", row["matched_units"])
	}

	if first, _ := units[0].(map[string]any); first["snippet"] == nil {
		test.Errorf("first unit lost its snippet: %v", first)
	}

	if result.Semantic.Ranked[0].Snippet == "" {
		test.Errorf("ScoredRow.Snippet emptied; the CLI table still prints it")
	}
}

// TestScoredRow_JSONKeepsSnippetWithoutUnits: a file that ranked by its
// file-level vector has no units, so its row snippet is the only one.
func TestScoredRow_JSONKeepsSnippetWithoutUnits(test *testing.T) {
	row := jsonObject(test, query.ScoredRow{ID: "notes/flat", Score: 0.9, Snippet: "the file-level passage"})

	if row["snippet"] != "the file-level passage" {
		test.Errorf("snippet = %v, want the file-level passage", row["snippet"])
	}
}

// TestScoredRow_JSONLeavesTopSnippetToBody: with include=body the body is
// the first unit's whole passage, so that unit's snippet is left out. The
// other units keep theirs, and the Go struct keeps it for the compact view.
func TestScoredRow_JSONLeavesTopSnippetToBody(test *testing.T) {
	result := runLedger(test, query.Request{Semantic: "balances", Include: []string{"body"}})
	row := jsonObject(test, result.Semantic.Ranked[0])

	if row["body"] != "Balances are derived from entries, never stored." {
		test.Fatalf("body = %v, want the best leaf's chunk", row["body"])
	}

	units, _ := row["matched_units"].([]any)

	if len(units) != 2 {
		test.Fatalf("matched_units = %v, want 2", row["matched_units"])
	}

	if first, _ := units[0].(map[string]any); first["snippet"] != nil {
		test.Errorf("first unit repeats the body as a snippet: %v", first)
	}

	if second, _ := units[1].(map[string]any); second["snippet"] == nil {
		test.Errorf("second unit lost its snippet: %v", second)
	}

	if result.Semantic.Ranked[0].MatchedUnits[0].Snippet == "" {
		test.Errorf("MatchedUnit.Snippet emptied; the compact view still prints it")
	}
}

// TestScoredRow_JSONRoundsScores pins #813 item 3 on the row and its trace.
func TestScoredRow_JSONRoundsScores(test *testing.T) {
	row := jsonObject(test, query.ScoredRow{
		ID: "notes/a", Score: 0.85355339,
		CosineScore: 0.70710678, LexicalScore: 1, GraphScore: 0.12345678, FinalScore: 0.85355339,
	})

	want := map[string]float64{"score": 0.854, "cosine_score": 0.707, "lexical_score": 1, "graph_score": 0.123, "final_score": 0.854}

	for key, value := range want {
		if row[key] != value {
			test.Errorf("%s = %v, want %v", key, row[key], value)
		}
	}
}
