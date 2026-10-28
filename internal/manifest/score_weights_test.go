package manifest_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

const weightsWorkspace = `
[workspace]
name = "x"

[node-types.note]
description = "n"

[node-types.service]
description = "s"
`

func TestTitleWeight_AbsentTakesTheDefault(test *testing.T) {
	loaded := loadInlineManifest(test, weightsWorkspace)

	if loaded.TitleWeight != manifest.DefaultTitleWeight {
		test.Errorf("TitleWeight = %v, want the default %v", loaded.TitleWeight, manifest.DefaultTitleWeight)
	}
}

func TestTitleWeight_ExplicitZeroTurnsItOff(test *testing.T) {
	loaded := loadInlineManifest(test, weightsWorkspace+`
[query]
title-weight = 0
`)

	if loaded.TitleWeight != 0 {
		test.Errorf("TitleWeight = %v, want 0", loaded.TitleWeight)
	}
}

func TestTitleWeight_RejectsOutOfRange(test *testing.T) {
	for _, badValue := range []string{"-0.1", "1.5", "nan", `"high"`} {
		_, loadErr := loadInlineManifestAllowError(test, weightsWorkspace+`
[query]
title-weight = `+badValue+`
`)

		if loadErr == nil {
			test.Errorf("title-weight = %s: expected an error", badValue)

			continue
		}

		if !strings.Contains(loadErr.Error(), "title-weight") {
			test.Errorf("title-weight = %s: error %q does not name the key", badValue, loadErr.Error())
		}
	}
}

func TestScoreWeights_AbsentIsEmpty(test *testing.T) {
	loaded := loadInlineManifest(test, weightsWorkspace)

	if !loaded.ScoreWeights.Empty() {
		test.Errorf("ScoreWeights = %+v, want empty", loaded.ScoreWeights)
	}

	if weight := loaded.ScoreWeights.Weight("note", "notes/a.md"); weight != 1 {
		test.Errorf("Weight = %v, want 1", weight)
	}
}

func TestScoreWeights_Weight(test *testing.T) {
	loaded := loadInlineManifest(test, weightsWorkspace+`
[query]
lexical-weight = 0.5

[query.type-weights]
note = 0.8

[query.path-weights]
"incidents/**" = 0.5
"incidents/old/**" = 0.25
"*.md" = 0.9
`)

	if loaded.LexicalWeight != 0.5 {
		test.Errorf("LexicalWeight = %v; the weight tables must not disturb [query] keys", loaded.LexicalWeight)
	}

	cases := []struct {
		name     string
		nodeType string
		path     string
		want     float64
	}{
		{name: "nothing applies", nodeType: "service", path: "services/ledger-sync.md", want: 1},
		{name: "type alone", nodeType: "note", path: "notes/a.md", want: 0.8},
		{name: "path alone", nodeType: "service", path: "incidents/a.md", want: 0.5},
		{name: "type times path", nodeType: "note", path: "incidents/a.md", want: 0.4},
		{name: "overlapping paths take the smallest", nodeType: "service", path: "incidents/old/a.md", want: 0.25},
		{name: "a glob is anchored to the whole path", nodeType: "service", path: "root.md", want: 0.9},
		{name: "a star stays in its folder", nodeType: "service", path: "deep/root.md", want: 1},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			if got := loaded.ScoreWeights.Weight(testCase.nodeType, testCase.path); got != testCase.want {
				test.Errorf("Weight(%q, %q) = %v, want %v", testCase.nodeType, testCase.path, got, testCase.want)
			}
		})
	}
}

func TestScoreWeights_Rejects(test *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "type weight above 1", body: "[query.type-weights]\nnote = 1.5\n", wantErr: `query.type-weights: "note" = 1.5 must be in (0, 1]`},
		{name: "type weight of 0", body: "[query.type-weights]\nnote = 0\n", wantErr: `query.type-weights: "note" = 0 must be in (0, 1]`},
		{name: "undeclared type", body: "[query.type-weights]\nnotes = 0.5\n", wantErr: `query.type-weights: "notes" is not a declared node type`},
		{name: "path weight below 0", body: "[query.path-weights]\n\"incidents/**\" = -1\n", wantErr: `query.path-weights: "incidents/**" = -1 must be in (0, 1]`},
		{name: "empty path pattern", body: "[query.path-weights]\n\"\" = 0.5\n", wantErr: "query.path-weights: a path pattern must not be empty"},
		{name: "non-number weight", body: "[query.path-weights]\n\"incidents/**\" = \"low\"\n", wantErr: "path-weights"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			_, loadErr := loadInlineManifestAllowError(test, weightsWorkspace+testCase.body)

			if loadErr == nil || !strings.Contains(loadErr.Error(), testCase.wantErr) {
				test.Errorf("error = %v, want one containing %q", loadErr, testCase.wantErr)
			}
		})
	}
}
