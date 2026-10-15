package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirstSemanticQuery(test *testing.T) {
	file, openErr := os.Open("testdata/stream.jsonl")

	if openErr != nil {
		test.Fatal(openErr)
	}

	defer file.Close()

	query, parseErr := firstSemanticQuery(file)

	if parseErr != nil {
		test.Fatal(parseErr)
	}

	if query != "embedding prefixes" {
		test.Errorf("query = %q, want %q", query, "embedding prefixes")
	}
}

func TestFirstQueriesSkipsGrepRunsAndRunsWithoutAQuery(test *testing.T) {
	dir := test.TempDir()
	streams := filepath.Join(dir, "streams")

	if mkdirErr := os.MkdirAll(streams, 0o755); mkdirErr != nil {
		test.Fatal(mkdirErr)
	}

	stream, readErr := os.ReadFile("testdata/stream.jsonl")

	if readErr != nil {
		test.Fatal(readErr)
	}

	noQuery := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"a.md"}}]}}` + "\n"

	files := map[string]string{
		"alpha__tusk__1.jsonl": string(stream),
		"alpha__grep__1.jsonl": string(stream),
		"beta__both__1.jsonl":  string(stream),
		"beta__tusk__1.jsonl":  noQuery,
	}

	for name, content := range files {
		if writeErr := os.WriteFile(filepath.Join(streams, name), []byte(content), 0o644); writeErr != nil {
			test.Fatal(writeErr)
		}
	}

	var runs strings.Builder

	for _, record := range []RunRecord{
		{TaskID: "alpha", Arm: "tusk", Rep: 1, OK: true},
		{TaskID: "alpha", Arm: "grep", Rep: 1, OK: true},
		{TaskID: "beta", Arm: "both", Rep: 1, OK: true},
		{TaskID: "beta", Arm: "tusk", Rep: 1, OK: true},
	} {
		encoded, _ := json.Marshal(record)
		runs.Write(append(encoded, '\n'))
	}

	if writeErr := os.WriteFile(filepath.Join(dir, "runs.jsonl"), []byte(runs.String()), 0o644); writeErr != nil {
		test.Fatal(writeErr)
	}

	queries, queriesErr := FirstQueries(dir)

	if queriesErr != nil {
		test.Fatal(queriesErr)
	}

	if len(queries) != 2 {
		test.Fatalf("queries = %+v, want alpha's tusk run and beta's both run", queries)
	}

	if queries[0].TaskID != "alpha" || queries[1].TaskID != "beta" || queries[1].Text != "embedding prefixes" {
		test.Errorf("queries = %+v", queries)
	}

	if !strings.HasSuffix(queries[0].Source, "/alpha__tusk__1.jsonl") {
		test.Errorf("source = %q", queries[0].Source)
	}
}

func probeFixtureSections() []probeSection {
	return []probeSection{
		{ID: "docs/a#S1", Path: "docs/a.md", Title: "internal/a"},
		{ID: "docs/a#S1.1", Path: "docs/a.md", Title: "Public surface"},
		{ID: "docs/a#S1.2", Path: "docs/a.md", Title: "Notes"},
		{ID: "docs/a#S1.2.1", Path: "docs/a.md", Title: "Lease TTL — [lease]"},
		{ID: "docs/a#S1.10", Path: "docs/a.md", Title: "Later"},
		{ID: "docs/b#S1.2", Path: "docs/b.md", Title: "Notes"},
	}
}

func TestTargetSectionsIncludesNestedSections(test *testing.T) {
	task := Task{ID: "q", Targets: []Target{{File: "docs/a.md", Heading: "Notes"}}}

	ids, files := targetSections(task, probeFixtureSections())

	if len(ids) != 2 || !ids["docs/a#S1.2"] || !ids["docs/a#S1.2.1"] {
		test.Errorf("ids = %v, want S1.2 and its child S1.2.1 only (not S1.10, not docs/b)", ids)
	}

	if len(files) != 1 || !files["docs/a.md"] {
		test.Errorf("files = %v", files)
	}
}

func TestTargetSectionsNormalizesHeadings(test *testing.T) {
	task := Task{ID: "q", Targets: []Target{{File: "docs/a.md", Heading: "Lease TTL — `[lease]`"}}}

	ids, _ := targetSections(task, probeFixtureSections())

	if !ids["docs/a#S1.2.1"] {
		test.Errorf("ids = %v, want the backticked heading matched", ids)
	}
}

func probeFixtureRows() []probeRow {
	var rows []probeRow

	decodeErr := json.Unmarshal([]byte(`[
		{"path": "docs/b.md", "matched_units": [{"id": "docs/b#S1.2", "score": 0.9}, {"id": "docs/b#S1.1", "score": 0.5}]},
		{"path": "docs/a.md", "matched_units": [{"id": "docs/a#S1.1", "score": 0.7}, {"id": "docs/a#S1.2.1", "score": 0.8}]}
	]`), &rows)

	if decodeErr != nil {
		panic(decodeErr)
	}

	return rows
}

func TestRankAnswer(test *testing.T) {
	ids := map[string]bool{"docs/a#S1.2": true, "docs/a#S1.2.1": true}
	files := map[string]bool{"docs/a.md": true}

	sectionRank, fileRank, visible := rankAnswer(probeFixtureRows(), ids, files, 5, 3)

	// Units by score: b#S1.2 0.9, a#S1.2.1 0.8, a#S1.1 0.7, b#S1.1 0.5.
	if sectionRank != 2 || fileRank != 2 || !visible {
		test.Errorf("ranks = %d, %d, visible %v; want 2, 2, true", sectionRank, fileRank, visible)
	}

	// One file and one unit per file: docs/a is cut, so the answer isn't shown.
	if _, _, shown := rankAnswer(probeFixtureRows(), ids, files, 1, 1); shown {
		test.Errorf("visible with take 1, want false")
	}

	// The answer is the best unit of docs/a, so one unit per file still shows it.
	if _, _, shown := rankAnswer(probeFixtureRows(), ids, files, 2, 1); !shown {
		test.Errorf("not visible with take 2, max units 1; want true")
	}

	if sectionRank, fileRank, _ := rankAnswer(probeFixtureRows(), map[string]bool{"docs/c#S1": true}, map[string]bool{"docs/c.md": true}, 5, 3); sectionRank != 0 || fileRank != 0 {
		test.Errorf("missing answer ranked %d, %d; want 0, 0", sectionRank, fileRank)
	}
}

func TestWriteProbeReport(test *testing.T) {
	baseline := ProbeResult{Queries: "first", Take: 5, MaxUnits: 3, Records: []ProbeRecord{
		{TaskID: "alpha", SectionRank: 1, FileRank: 1, Visible: true},
		{TaskID: "alpha", SectionRank: 2, FileRank: 1, Visible: true},
		{TaskID: "beta", SectionRank: 20, FileRank: 4, Visible: false},
	}}

	alone := WriteProbeReport(baseline, nil)

	// alpha: (1 + 0.5) / 2 = 0.75; beta: rank 20 is past the cutoff, 0.
	for _, want := range []string{"3 queries over 2 questions (first)", "| Section MRR@10 | 0.375 |", "| File MRR@10 | 0.625 |", "| Section in the agent's result | 0.500 |"} {
		if !strings.Contains(alone, want) {
			test.Errorf("report lacks %q:\n%s", want, alone)
		}
	}

	current := ProbeResult{Queries: "first", Take: 5, MaxUnits: 3, Records: []ProbeRecord{
		{TaskID: "alpha", SectionRank: 1, FileRank: 1, Visible: true},
		{TaskID: "beta", SectionRank: 1, FileRank: 1, Visible: true},
	}}

	compared := WriteProbeReport(current, &baseline)

	for _, want := range []string{"| Section MRR@10 | 0.375 | 1.000 | +0.625", "| beta | 0.00 | 1.00 |", "| alpha | 0.75 | 1.00 |"} {
		if !strings.Contains(compared, want) {
			test.Errorf("comparison lacks %q:\n%s", want, compared)
		}
	}
}
