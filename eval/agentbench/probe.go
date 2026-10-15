package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// The probe replays queries against a staged vault with the tusk binary
// under test and records where each question's answering section ranks. It
// costs no agent runs, so a ranking change can be measured offline before
// the full benchmark spends money on it.

// rankCutoff is the depth MRR and hit rates look at.
const rankCutoff = 10

// ProbeQuery is one query to replay: the question it belongs to, where it
// came from (a run's stream, or "question" for the question text), and the
// semantic text itself.
type ProbeQuery struct {
	TaskID string
	Source string
	Text   string
}

// ProbeRecord is where one query ranked its question's answer. Ranks are
// 1-based and 0 when the answer didn't rank at all. SectionRank counts the
// matched sections of every file in score order; Visible is whether the
// answer was among the units of the result an agent sees (take files of
// maxUnits units each).
type ProbeRecord struct {
	TaskID      string `json:"task_id"`
	Source      string `json:"source"`
	Query       string `json:"query"`
	SectionRank int    `json:"section_rank"`
	FileRank    int    `json:"file_rank"`
	Visible     bool   `json:"visible"`
}

// ProbeResult is what --out writes and --baseline reads back.
type ProbeResult struct {
	Tusk     string        `json:"tusk"`
	Manifest string        `json:"manifest"`
	Queries  string        `json:"queries"`
	Take     int           `json:"take"`
	MaxUnits int           `json:"max_units"`
	Records  []ProbeRecord `json:"records"`
}

// probeSection is a section row from `tusk query type=section`.
type probeSection struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Title string `json:"title"`
}

// probeRow is a semantic result row; only what ranking needs.
type probeRow struct {
	Path  string `json:"path"`
	Units []struct {
		ID    string  `json:"id"`
		Score float64 `json:"score"`
	} `json:"matched_units"`
}

// FirstQueries returns the first tusk_query semantic text of every run in a
// results directory whose arm uses tusk, in runs.jsonl order. A run whose
// first query has no semantic text, or that never queried, is skipped.
func FirstQueries(resultsDir string) ([]ProbeQuery, error) {
	records, recordsErr := LoadRecords(filepath.Join(resultsDir, "runs.jsonl"))

	if recordsErr != nil {
		return nil, recordsErr
	}

	var queries []ProbeQuery

	for _, record := range records {
		arms, armsErr := LookupArms(record.Arm)

		if armsErr != nil || !arms[0].UsesTusk() {
			continue
		}

		name := strings.NewReplacer("|", "__").Replace(record.Key()) + ".jsonl"
		streamPath := filepath.Join(resultsDir, "streams", name)
		text, readErr := readFirstQuery(streamPath)

		if readErr != nil {
			return nil, readErr
		}

		if text != "" {
			queries = append(queries, ProbeQuery{TaskID: record.TaskID, Source: filepath.Base(resultsDir) + "/" + name, Text: text})
		}
	}

	return queries, nil
}

func readFirstQuery(path string) (string, error) {
	file, openErr := os.Open(path)

	if openErr != nil {
		return "", openErr
	}

	defer file.Close()

	return firstSemanticQuery(file)
}

// firstSemanticQuery returns the semantic text of the first tusk_query call
// in a stream-json transcript, or "" when there is none.
func firstSemanticQuery(reader io.Reader) (string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 1<<20), maxStreamLine)

	for scanner.Scan() {
		var event streamEvent

		if decodeErr := json.Unmarshal(scanner.Bytes(), &event); decodeErr != nil || event.Type != "assistant" {
			continue
		}

		for _, block := range decodeBlocks(event.Message.Content) {
			if block.Type != "tool_use" || block.Name != mcpToolName("tusk_query") {
				continue
			}

			var input struct {
				Semantic string `json:"semantic"`
			}

			_ = json.Unmarshal(block.Input, &input)

			return input.Semantic, nil
		}
	}

	return "", scanner.Err()
}

// QuestionQueries uses each question's own text as its query.
func QuestionQueries(tasks []Task) []ProbeQuery {
	queries := make([]ProbeQuery, 0, len(tasks))

	for _, task := range tasks {
		queries = append(queries, ProbeQuery{TaskID: task.ID, Source: "question", Text: task.Question})
	}

	return queries
}

// targetSections returns the ids of the sections that answer task, each
// target heading plus every section nested inside it, and the target files.
// Section ids are hierarchical (file#S1, file#S1.2), so nesting is a prefix.
func targetSections(task Task, sections []probeSection) (map[string]bool, map[string]bool) {
	ids := make(map[string]bool)
	files := make(map[string]bool)

	for _, target := range task.Targets {
		files[target.File] = true

		for _, root := range sections {
			if root.Path != target.File || normalizeHeading(root.Title) != normalizeHeading(target.Heading) {
				continue
			}

			for _, section := range sections {
				if section.ID == root.ID || strings.HasPrefix(section.ID, root.ID+".") {
					ids[section.ID] = true
				}
			}
		}
	}

	return ids, files
}

// rankAnswer scores one query's rows against the answer. take and maxUnits
// are the result shape an agent gets by default (files, then units per file).
func rankAnswer(rows []probeRow, ids map[string]bool, files map[string]bool, take int, maxUnits int) (int, int, bool) {
	type unit struct {
		id    string
		score float64
	}

	var units []unit

	visible := false

	for position, row := range rows {
		ordered := slices.Clone(row.Units)
		sort.SliceStable(ordered, func(left, right int) bool { return ordered[left].Score > ordered[right].Score })

		for rank, matched := range ordered {
			units = append(units, unit{id: matched.ID, score: matched.Score})

			if position < take && rank < maxUnits && ids[matched.ID] {
				visible = true
			}
		}
	}

	sort.SliceStable(units, func(left, right int) bool { return units[left].score > units[right].score })

	sectionRank := slices.IndexFunc(units, func(candidate unit) bool { return ids[candidate.id] }) + 1
	fileRank := slices.IndexFunc(rows, func(row probeRow) bool { return files[row.Path] }) + 1

	return sectionRank, fileRank, visible
}

// Probe replays queries against the staged vault with tuskBin.
func Probe(ctx context.Context, tuskBin string, vault string, tasks []Task, queries []ProbeQuery, take int, maxUnits int) ([]ProbeRecord, error) {
	output, sectionsErr := runTusk(ctx, tuskBin, vault, "query", "type=section", "--json")

	if sectionsErr != nil {
		return nil, sectionsErr
	}

	var sections []probeSection

	if decodeErr := json.Unmarshal(output, &sections); decodeErr != nil {
		return nil, fmt.Errorf("decoding sections: %w", decodeErr)
	}

	byID := make(map[string]Task, len(tasks))

	for _, task := range tasks {
		byID[task.ID] = task
	}

	ranked := make(map[string][]probeRow)
	records := make([]ProbeRecord, 0, len(queries))

	for _, query := range queries {
		task, known := byID[query.TaskID]

		if !known {
			continue
		}

		rows, seen := ranked[query.Text]

		if !seen {
			// No --take and --max-units 0: the CLI returns every file and
			// every matched unit, so a rank is never cut off.
			result, queryErr := runTusk(ctx, tuskBin, vault, "query", "", "--semantic", query.Text, "--max-units", "0", "--json")

			if queryErr != nil {
				return nil, queryErr
			}

			if decodeErr := json.Unmarshal(result, &rows); decodeErr != nil {
				return nil, fmt.Errorf("decoding results for %q: %w", query.Text, decodeErr)
			}

			ranked[query.Text] = rows
		}

		ids, files := targetSections(task, sections)

		if len(ids) == 0 {
			return nil, fmt.Errorf("%s: no indexed section matches its target headings", task.ID)
		}

		sectionRank, fileRank, visible := rankAnswer(rows, ids, files, take, maxUnits)

		records = append(records, ProbeRecord{
			TaskID:      query.TaskID,
			Source:      query.Source,
			Query:       query.Text,
			SectionRank: sectionRank,
			FileRank:    fileRank,
			Visible:     visible,
		})
	}

	return records, nil
}

// probeMetric is one row of the probe report: a per-record value, averaged
// within each question and then across questions, so a question with many
// runs weighs no more than one with few.
type probeMetric struct {
	label string
	value func(ProbeRecord) float64
}

func reciprocalRank(rank int) float64 {
	if rank < 1 || rank > rankCutoff {
		return 0
	}

	return 1 / float64(rank)
}

func hitAt(rank int, cutoff int) float64 {
	if rank >= 1 && rank <= cutoff {
		return 1
	}

	return 0
}

var probeMetrics = []probeMetric{
	{"Section MRR@10", func(record ProbeRecord) float64 { return reciprocalRank(record.SectionRank) }},
	{"Section in top 1", func(record ProbeRecord) float64 { return hitAt(record.SectionRank, 1) }},
	{"Section in top 3", func(record ProbeRecord) float64 { return hitAt(record.SectionRank, 3) }},
	{"Section in top 10", func(record ProbeRecord) float64 { return hitAt(record.SectionRank, rankCutoff) }},
	{"Section in the agent's result", func(record ProbeRecord) float64 {
		if record.Visible {
			return 1
		}

		return 0
	}},
	{"File MRR@10", func(record ProbeRecord) float64 { return reciprocalRank(record.FileRank) }},
}

// perQuestion averages metric over each question's records.
func perQuestion(records []ProbeRecord, metric probeMetric) map[string]float64 {
	values := make(map[string][]float64)

	for _, record := range records {
		values[record.TaskID] = append(values[record.TaskID], metric.value(record))
	}

	means := make(map[string]float64, len(values))

	for taskID, taskValues := range values {
		means[taskID] = mean(taskValues)
	}

	return means
}

func sortedValues(byTask map[string]float64) []float64 {
	values := make([]float64, 0, len(byTask))

	for _, key := range slices.Sorted(maps.Keys(byTask)) {
		values = append(values, byTask[key])
	}

	return values
}

// WriteProbeReport renders the metrics as a markdown table. With a baseline
// it adds the baseline's column and the per-question difference, with a 95%
// bootstrap interval over the questions both probes share, then lists the
// questions whose answering section moved.
func WriteProbeReport(result ProbeResult, baseline *ProbeResult) string {
	var out strings.Builder

	questions := make(map[string]bool)

	for _, record := range result.Records {
		questions[record.TaskID] = true
	}

	fmt.Fprintf(&out, "%d queries over %d questions (%s), take %d, max units %d\n\n",
		len(result.Records), len(questions), result.Queries, result.Take, result.MaxUnits)

	if baseline == nil {
		out.WriteString("| | Value |\n| --- | --- |\n")

		for _, metric := range probeMetrics {
			fmt.Fprintf(&out, "| %s | %.3f |\n", metric.label, mean(sortedValues(perQuestion(result.Records, metric))))
		}

		return out.String()
	}

	out.WriteString("| | Baseline | This probe | Difference per question |\n| --- | --- | --- | --- |\n")

	for _, metric := range probeMetrics {
		current := perQuestion(result.Records, metric)
		before := perQuestion(baseline.Records, metric)

		var shared, currentValues, beforeValues []float64

		for _, taskID := range slices.Sorted(maps.Keys(current)) {
			previous, inBoth := before[taskID]

			if !inBoth {
				continue
			}

			currentValues = append(currentValues, current[taskID])
			beforeValues = append(beforeValues, previous)
			shared = append(shared, current[taskID]-previous)
		}

		low, high := bootstrapCI(shared, mean)

		fmt.Fprintf(&out, "| %s | %.3f | %.3f | %+.3f (%+.3f to %+.3f) |\n",
			metric.label, mean(beforeValues), mean(currentValues), mean(shared), low, high)
	}

	writeMovers(&out, result, *baseline)

	return out.String()
}

// writeMovers lists the questions whose section MRR@10 moved by 0.1 or more.
func writeMovers(out *strings.Builder, result ProbeResult, baseline ProbeResult) {
	current := perQuestion(result.Records, probeMetrics[0])
	before := perQuestion(baseline.Records, probeMetrics[0])

	var lines []string

	for _, taskID := range slices.Sorted(maps.Keys(current)) {
		previous, inBoth := before[taskID]

		if inBoth && (current[taskID]-previous >= 0.1 || previous-current[taskID] >= 0.1) {
			lines = append(lines, fmt.Sprintf("| %s | %.2f | %.2f |", taskID, previous, current[taskID]))
		}
	}

	if len(lines) == 0 {
		return
	}

	out.WriteString("\nQuestions whose section MRR@10 moved by 0.1 or more:\n\n| Question | Baseline | This probe |\n| --- | --- | --- |\n")
	out.WriteString(strings.Join(lines, "\n") + "\n")
}

// LoadProbeResult reads a probe's --out file.
func LoadProbeResult(path string) (*ProbeResult, error) {
	content, readErr := os.ReadFile(path)

	if readErr != nil {
		return nil, readErr
	}

	var result ProbeResult

	if decodeErr := json.Unmarshal(content, &result); decodeErr != nil {
		return nil, fmt.Errorf("decoding %s: %w", path, decodeErr)
	}

	if len(result.Records) == 0 {
		return nil, errors.New(path + " has no probe records")
	}

	return &result, nil
}
