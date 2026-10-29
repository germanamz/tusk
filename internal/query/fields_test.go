package query_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/query"
)

// TestParseFields_RejectsUnknownName pins #851: an unknown field name is an
// error naming the valid ones, so a typo can't silently drop a column.
func TestParseFields_RejectsUnknownName(test *testing.T) {
	_, parseErr := query.ParseFields([]string{"id", "titel"})

	if parseErr == nil {
		test.Fatal("ParseFields(titel) = nil error, want unknown field")
	}

	for _, want := range []string{`"titel"`, "title", "properties.<name>"} {
		if !strings.Contains(parseErr.Error(), want) {
			test.Errorf("error %q does not mention %s", parseErr, want)
		}
	}

	if _, emptyNameErr := query.ParseFields([]string{"properties."}); emptyNameErr == nil {
		test.Error("ParseFields(properties.) = nil error, want a missing property name")
	}
}

func TestParseFields_ImpliesIncludes(test *testing.T) {
	fields, parseErr := query.ParseFields([]string{"id", "properties.owner", "units"})

	if parseErr != nil {
		test.Fatalf("ParseFields: %v", parseErr)
	}

	include := fields.Include()

	if !include.Properties || !include.Units || include.Body || include.Edges {
		test.Errorf("Include() = %+v, want properties and units only", include)
	}

	if got := strings.Join(fields.Names(), ","); got != "id,properties.owner,matched_units" {
		test.Errorf("Names() = %q, want units normalized to matched_units", got)
	}
}

// TestFieldsProject_KeepsNamedKeysInOrder pins the JSON half of #851: a row
// keeps only the named keys, in the order the caller named them.
func TestFieldsProject_KeepsNamedKeysInOrder(test *testing.T) {
	fields, parseErr := query.ParseFields([]string{"title", "id"})

	if parseErr != nil {
		test.Fatalf("ParseFields: %v", parseErr)
	}

	projected, projectErr := fields.Project(query.Row{ID: "a", Type: "service", Path: "a.md", Title: "A"})

	if projectErr != nil {
		test.Fatalf("Project: %v", projectErr)
	}

	if got, want := string(projected), `{"title":"A","id":"a"}`; got != want {
		test.Errorf("Project = %s, want %s", got, want)
	}
}

func TestFieldsProject_NamedPropertiesOnly(test *testing.T) {
	fields, parseErr := query.ParseFields([]string{"id", "properties.owner", "properties.missing"})

	if parseErr != nil {
		test.Fatalf("ParseFields: %v", parseErr)
	}

	row := query.Row{ID: "a", Properties: map[string]any{"owner": "me", "domain": "billing"}}
	projected, projectErr := fields.Project(row)

	if projectErr != nil {
		test.Fatalf("Project: %v", projectErr)
	}

	if got, want := string(projected), `{"id":"a","properties":{"owner":"me"}}`; got != want {
		test.Errorf("Project = %s, want %s", got, want)
	}

	// A row with none of the named properties carries no properties key.
	bare, bareErr := fields.Project(query.Row{ID: "b", Properties: map[string]any{"domain": "billing"}})

	if bareErr != nil {
		test.Fatalf("Project: %v", bareErr)
	}

	if got, want := string(bare), `{"id":"b"}`; got != want {
		test.Errorf("Project = %s, want %s", got, want)
	}

	// properties beside properties.<name> keeps them all.
	every, everyErr := query.ParseFields([]string{"properties.owner", "properties"})

	if everyErr != nil {
		test.Fatalf("ParseFields: %v", everyErr)
	}

	whole, wholeErr := every.Project(row)

	if wholeErr != nil {
		test.Fatalf("Project: %v", wholeErr)
	}

	if got, want := string(whole), `{"properties":{"domain":"billing","owner":"me"}}`; got != want {
		test.Errorf("Project = %s, want %s", got, want)
	}
}

// TestFieldsProject_SemanticRow covers the semantic wire shape: matched_units
// brings units_total, and explain scores stay without being named.
func TestFieldsProject_SemanticRow(test *testing.T) {
	fields, parseErr := query.ParseFields([]string{"id", "matched_units"})

	if parseErr != nil {
		test.Fatalf("ParseFields: %v", parseErr)
	}

	row := query.ScoredRow{
		ID:           "notes/a",
		Type:         "note",
		Path:         "notes/a.md",
		Score:        0.81234,
		MatchedUnits: []query.MatchedUnit{{ID: "notes/a#S1", Heading: "Intro", Score: 0.8, HasScore: true}},
		UnitsTotal:   3,
		CosineScore:  0.7,
		FinalScore:   0.81234,
	}

	projected, projectErr := fields.Project(row)

	if projectErr != nil {
		test.Fatalf("Project: %v", projectErr)
	}

	got := string(projected)

	for _, want := range []string{`"id":"notes/a"`, `"matched_units":[`, `"units_total":3`, `"cosine_score":0.7`, `"final_score":0.812`} {
		if !strings.Contains(got, want) {
			test.Errorf("Project = %s, missing %s", got, want)
		}
	}

	for _, unwanted := range []string{`"type"`, `"path"`, `"score":0.812`} {
		if strings.Contains(got, unwanted) {
			test.Errorf("Project = %s, should not carry %s", got, unwanted)
		}
	}
}

func TestProjectRows(test *testing.T) {
	rows := []query.Row{{ID: "a", Type: "note"}, {ID: "b", Type: "note"}}

	if _, same := query.ProjectRows(query.Fields{}, rows).([]query.Row); !same {
		test.Error("ProjectRows with no fields changed the rows' type, want them unchanged")
	}

	fields, parseErr := query.ParseFields([]string{"id"})

	if parseErr != nil {
		test.Fatalf("ParseFields: %v", parseErr)
	}

	encoded, marshalErr := json.Marshal(map[string]any{
		"rows": query.ProjectRows(fields, rows),
		"one":  query.ProjectRow(fields, rows[0]),
	})

	if marshalErr != nil {
		test.Fatalf("marshal: %v", marshalErr)
	}

	if got, want := string(encoded), `{"one":{"id":"a"},"rows":[{"id":"a"},{"id":"b"}]}`; got != want {
		test.Errorf("JSON = %s, want %s", got, want)
	}
}
