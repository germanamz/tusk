package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"time"
)

// Meta describes one benchmark run. run writes it to meta.json next to
// runs.jsonl, and report prints it as the report's header.
type Meta struct {
	Label          string    `json:"label"`
	TasksSHA256    string    `json:"tasks_sha256"`
	Questions      int       `json:"questions"`
	Paths          []string  `json:"paths"`
	CorpusFiles    int       `json:"corpus_files"`
	CorpusSections int       `json:"corpus_sections"`
	Arms           []string  `json:"arms"`
	Reps           int       `json:"reps"`
	Model          string    `json:"model"`
	JudgeModel     string    `json:"judge_model,omitempty"`
	Concurrency    int       `json:"concurrency"`
	TuskVersion    string    `json:"tusk_version"`
	ClaudeVersion  string    `json:"claude_version"`
	StartedAt      time.Time `json:"started_at"`
}

// LoadRecords reads runs.jsonl. A job that ran more than once (a resumed
// benchmark retries failed runs) keeps only its latest record.
func LoadRecords(path string) ([]RunRecord, error) {
	file, openErr := os.Open(path)

	if openErr != nil {
		return nil, openErr
	}

	defer file.Close()

	latest := make(map[string]int)

	var records []RunRecord

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), maxStreamLine)

	for scanner.Scan() {
		if len(strings.TrimSpace(scanner.Text())) == 0 {
			continue
		}

		var record RunRecord

		if decodeErr := json.Unmarshal(scanner.Bytes(), &record); decodeErr != nil {
			return nil, fmt.Errorf("decoding %s: %w", path, decodeErr)
		}

		if index, seen := latest[record.Key()]; seen {
			records[index] = record

			continue
		}

		latest[record.Key()] = len(records)
		records = append(records, record)
	}

	return records, scanner.Err()
}

// cell is one question under one arm, summarized across its repetitions.
type cell struct {
	runs   int
	failed int
	// Medians over the runs that reported usage, failed ones included:
	// a run that failed still spent what it spent.
	totalInput float64
	toolCalls  float64
	toolOutput float64
	durationS  float64
	cost       float64
	// sectionRate counts a failed run as a miss. correctRate does too, and
	// leaves out runs whose judge call failed; NaN when nothing was judged.
	sectionRate float64
	correctRate float64
}

type questionRow struct {
	taskID   string
	kind     string
	question string
	cells    map[string]*cell
}

func newCell(runs []RunRecord, judging bool) *cell {
	summary := &cell{runs: len(runs)}

	var input, calls, output, duration, cost []float64

	hits, correct, graded := 0, 0, 0

	for _, run := range runs {
		if run.TotalInputTokens > 0 {
			input = append(input, float64(run.TotalInputTokens))
			calls = append(calls, float64(run.ToolCalls))
			output = append(output, float64(run.ToolOutputBytes))
			duration = append(duration, float64(run.DurationMS)/1000)
			cost = append(cost, run.CostUSD)
		}

		switch {
		case !run.OK:
			summary.failed++

			if judging {
				graded++
			}
		case run.JudgeCorrect != nil:
			graded++

			if *run.JudgeCorrect {
				correct++
			}
		}

		if run.OK && run.SectionHit {
			hits++
		}
	}

	summary.totalInput = median(input)
	summary.toolCalls = median(calls)
	summary.toolOutput = median(output)
	summary.durationS = median(duration)
	summary.cost = median(cost)
	summary.sectionRate = float64(hits) / float64(len(runs))
	summary.correctRate = math.NaN()

	if graded > 0 {
		summary.correctRate = float64(correct) / float64(graded)
	}

	return summary
}

func buildRows(records []RunRecord, judging bool) []*questionRow {
	grouped := make(map[string]map[string][]RunRecord)
	rows := make(map[string]*questionRow)

	for _, record := range records {
		if rows[record.TaskID] == nil {
			rows[record.TaskID] = &questionRow{taskID: record.TaskID, kind: record.Kind, question: record.Question, cells: make(map[string]*cell)}
			grouped[record.TaskID] = make(map[string][]RunRecord)
		}

		grouped[record.TaskID][record.Arm] = append(grouped[record.TaskID][record.Arm], record)
	}

	var ordered []*questionRow

	for taskID, arms := range grouped {
		for arm, runs := range arms {
			rows[taskID].cells[arm] = newCell(runs, judging)
		}

		ordered = append(ordered, rows[taskID])
	}

	slices.SortFunc(ordered, func(left, right *questionRow) int { return strings.Compare(left.taskID, right.taskID) })

	return ordered
}

// metric reads one figure out of a cell.
type metric struct {
	name   string
	value  func(*cell) float64
	format func(float64) string
}

var costMetrics = []metric{
	{"Input tokens", func(summary *cell) float64 { return summary.totalInput }, formatCount},
	{"Tool output", func(summary *cell) float64 { return summary.toolOutput }, formatBytes},
	{"Time", func(summary *cell) float64 { return summary.durationS }, formatSeconds},
	{"Cost", func(summary *cell) float64 { return summary.cost }, formatUSD},
}

var toolCallMetric = metric{"Tool calls", func(summary *cell) float64 { return summary.toolCalls }, formatCalls}

// WriteReport renders the markdown report. numbersOnly leaves out question
// text and task ids, so a report on a private corpus can be shared.
func WriteReport(records []RunRecord, meta *Meta, numbersOnly bool) string {
	judging := slices.ContainsFunc(records, func(record RunRecord) bool { return record.JudgeCorrect != nil })
	rows := buildRows(records, judging)
	arms := presentArms(records)

	var out strings.Builder

	writeHeader(&out, records, meta, len(rows))
	writePerArm(&out, rows, arms, judging)

	for _, arm := range arms {
		if arm == baselineArm || !slices.Contains(arms, baselineArm) {
			continue
		}

		writePaired(&out, rows, arm, judging)
		writeByKind(&out, rows, []string{baselineArm, arm}, judging)
		writeLosses(&out, rows, arm, judging, numbersOnly)
	}

	if !numbersOnly {
		writePerQuestion(&out, rows, arms, judging)
	}

	return out.String()
}

func presentArms(records []RunRecord) []string {
	var arms []string

	for _, known := range knownArms {
		if slices.ContainsFunc(records, func(record RunRecord) bool { return record.Arm == known.Name }) {
			arms = append(arms, known.Name)
		}
	}

	return arms
}

func writeHeader(out *strings.Builder, records []RunRecord, meta *Meta, questions int) {
	out.WriteString("# Agent retrieval benchmark\n\n")

	models := map[string]bool{}

	for _, record := range records {
		if record.Model != "" {
			models[record.Model] = true
		}
	}

	modelNames := make([]string, 0, len(models))

	for model := range models {
		modelNames = append(modelNames, model)
	}

	slices.Sort(modelNames)

	if meta != nil {
		fmt.Fprintf(out, "- Corpus: %d markdown files, %d sections (%s)\n", meta.CorpusFiles, meta.CorpusSections, strings.Join(meta.Paths, ", "))
		fmt.Fprintf(out, "- Questions: %d, %d repetitions per arm\n", meta.Questions, meta.Reps)
		fmt.Fprintf(out, "- Agent model: %s; judge: %s\n", strings.Join(modelNames, ", "), orNone(meta.JudgeModel))
		fmt.Fprintf(out, "- tusk %s; Claude Code %s; %d concurrent runs; %s\n\n", meta.TuskVersion, meta.ClaudeVersion, meta.Concurrency, meta.StartedAt.Format("2006-01-02"))
	} else {
		fmt.Fprintf(out, "- Questions: %d; agent model: %s\n\n", questions, strings.Join(modelNames, ", "))
	}
}

func orNone(text string) string {
	if text == "" {
		return "none"
	}

	return text
}

func writePerArm(out *strings.Builder, rows []*questionRow, arms []string, judging bool) {
	out.WriteString("## Per arm\n\n")
	out.WriteString("Each figure is the median over questions of that question's median run, with the 25th to 75th percentile in brackets.\n\n")

	header := "| Arm | Input tokens | Tool calls | Tool output | Time | Cost | Section cited |"
	divider := "| --- | --- | --- | --- | --- | --- | --- |"

	if judging {
		header += " Judged correct |"
		divider += " --- |"
	}

	out.WriteString(header + " Runs (failed) |\n" + divider + " --- |\n")

	for _, arm := range arms {
		cells := cellsFor(rows, arm)
		runs, failed := 0, 0

		for _, summary := range cells {
			runs += summary.runs
			failed += summary.failed
		}

		line := fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s |", arm,
			spread(cells, costMetrics[0]), spread(cells, toolCallMetric), spread(cells, costMetrics[1]),
			spread(cells, costMetrics[2]), spread(cells, costMetrics[3]),
			formatPercent(mean(collect(cells, func(summary *cell) float64 { return summary.sectionRate }))))

		if judging {
			line += " " + formatPercent(mean(collect(cells, func(summary *cell) float64 { return summary.correctRate }))) + " |"
		}

		fmt.Fprintf(out, "%s %d (%d) |\n", line, runs, failed)
	}

	out.WriteString("\n")
}

func cellsFor(rows []*questionRow, arm string) []*cell {
	var cells []*cell

	for _, row := range rows {
		if summary, found := row.cells[arm]; found {
			cells = append(cells, summary)
		}
	}

	return cells
}

// collect reads a figure from each cell, dropping NaNs.
func collect(cells []*cell, value func(*cell) float64) []float64 {
	var values []float64

	for _, summary := range cells {
		if figure := value(summary); !math.IsNaN(figure) {
			values = append(values, figure)
		}
	}

	return values
}

func spread(cells []*cell, figure metric) string {
	values := collect(cells, figure.value)

	return fmt.Sprintf("%s [%s–%s]", figure.format(median(values)), figure.format(quantile(values, 0.25)), figure.format(quantile(values, 0.75)))
}

// pairs returns, for every question both arms ran, the two figures.
func pairs(rows []*questionRow, arm string, value func(*cell) float64) ([]float64, []float64) {
	var baseline, other []float64

	for _, row := range rows {
		baseCell, baseFound := row.cells[baselineArm]
		armCell, armFound := row.cells[arm]

		if !baseFound || !armFound {
			continue
		}

		baseValue, armValue := value(baseCell), value(armCell)

		if math.IsNaN(baseValue) || math.IsNaN(armValue) {
			continue
		}

		baseline = append(baseline, baseValue)
		other = append(other, armValue)
	}

	return baseline, other
}

func writePaired(out *strings.Builder, rows []*questionRow, arm string, judging bool) {
	fmt.Fprintf(out, "## %s vs %s, paired by question\n\n", arm, baselineArm)
	fmt.Fprintf(out, "A ratio below 1 means %s used less than %s on the same question.\n\n", arm, baselineArm)
	fmt.Fprintf(out, "| Metric | Median ratio (%s / %s) | 95%% CI | Questions where %s used less |\n| --- | --- | --- | --- |\n", arm, baselineArm, arm)

	for _, figure := range costMetrics {
		baseline, other := pairs(rows, arm, figure.value)

		var ratios []float64

		lower := 0

		for index := range baseline {
			if baseline[index] <= 0 {
				continue
			}

			ratios = append(ratios, other[index]/baseline[index])

			if other[index] < baseline[index] {
				lower++
			}
		}

		low, high := bootstrapCI(ratios, median)

		fmt.Fprintf(out, "| %s | %s | %s–%s | %d of %d |\n", figure.name, formatRatio(median(ratios)), formatRatio(low), formatRatio(high), lower, len(ratios))
	}

	baseline, other := pairs(rows, arm, toolCallMetric.value)
	differences := subtract(other, baseline)
	low, high := bootstrapCI(differences, median)
	fewer := 0

	for _, difference := range differences {
		if difference < 0 {
			fewer++
		}
	}

	fmt.Fprintf(out, "\n| Metric | Median difference (%s − %s) | 95%% CI | Questions where %s used fewer |\n| --- | --- | --- | --- |\n", arm, baselineArm, arm)
	fmt.Fprintf(out, "| Tool calls | %s | %s to %s | %d of %d |\n\n", formatSigned(median(differences)), formatSigned(low), formatSigned(high), fewer, len(differences))

	fmt.Fprintf(out, "| Accuracy | %s | %s | Difference | 95%% CI |\n| --- | --- | --- | --- | --- |\n", baselineArm, arm)

	accuracy := []metric{{"Section cited", func(summary *cell) float64 { return summary.sectionRate }, formatPercent}}

	if judging {
		accuracy = append(accuracy, metric{"Judged correct", func(summary *cell) float64 { return summary.correctRate }, formatPercent})
	}

	for _, figure := range accuracy {
		baseRates, armRates := pairs(rows, arm, figure.value)
		rateDiffs := subtract(armRates, baseRates)
		diffLow, diffHigh := bootstrapCI(rateDiffs, mean)

		fmt.Fprintf(out, "| %s | %s | %s | %s | %s to %s |\n", figure.name, formatPercent(mean(baseRates)), formatPercent(mean(armRates)),
			formatPoints(mean(rateDiffs)), formatPoints(diffLow), formatPoints(diffHigh))
	}

	out.WriteString("\n")
}

func subtract(left []float64, right []float64) []float64 {
	differences := make([]float64, len(left))

	for index := range left {
		differences[index] = left[index] - right[index]
	}

	return differences
}

func writeByKind(out *strings.Builder, rows []*questionRow, arms []string, judging bool) {
	out.WriteString("### By question kind\n\n")

	header := "| Kind | Questions | Arm | Input tokens | Tool calls | Section cited |"
	divider := "| --- | --- | --- | --- | --- | --- |"

	if judging {
		header += " Judged correct |"
		divider += " --- |"
	}

	out.WriteString(header + "\n" + divider + "\n")

	for _, kind := range []string{KindLexical, KindParaphrase} {
		var kindRows []*questionRow

		for _, row := range rows {
			if row.kind == kind {
				kindRows = append(kindRows, row)
			}
		}

		if len(kindRows) == 0 {
			continue
		}

		for _, arm := range arms {
			cells := cellsFor(kindRows, arm)

			line := fmt.Sprintf("| %s | %d | %s | %s | %s | %s |", kind, len(kindRows), arm,
				formatCount(median(collect(cells, costMetrics[0].value))),
				formatCalls(median(collect(cells, toolCallMetric.value))),
				formatPercent(mean(collect(cells, func(summary *cell) float64 { return summary.sectionRate }))))

			if judging {
				line += " " + formatPercent(mean(collect(cells, func(summary *cell) float64 { return summary.correctRate }))) + " |"
			}

			out.WriteString(line + "\n")
		}
	}

	out.WriteString("\n")
}

// writeLosses lists every question where arm did worse than the baseline:
// more input tokens, or a lower judged-correct rate (section-cited rate
// when nothing was judged).
func writeLosses(out *strings.Builder, rows []*questionRow, arm string, judging bool, numbersOnly bool) {
	accuracy := func(summary *cell) float64 { return summary.sectionRate }
	accuracyName := "Section cited"

	if judging {
		accuracy = func(summary *cell) float64 { return summary.correctRate }
		accuracyName = "Judged correct"
	}

	var losses []*questionRow

	for _, row := range rows {
		baseCell, baseFound := row.cells[baselineArm]
		armCell, armFound := row.cells[arm]

		if !baseFound || !armFound {
			continue
		}

		if armCell.totalInput > baseCell.totalInput || accuracy(armCell) < accuracy(baseCell) {
			losses = append(losses, row)
		}
	}

	fmt.Fprintf(out, "### Where %s lost\n\n", arm)
	fmt.Fprintf(out, "%d of %d questions: %s read more input tokens than %s, or was %s less often.\n\n",
		len(losses), len(rows), arm, baselineArm, strings.ToLower(accuracyName))

	if numbersOnly || len(losses) == 0 {
		return
	}

	fmt.Fprintf(out, "| Question | Kind | Input tokens (%[1]s → %[2]s) | Tool calls (%[1]s → %[2]s) | %[3]s (%[1]s / %[2]s) |\n| --- | --- | --- | --- | --- |\n",
		baselineArm, arm, accuracyName)

	for _, row := range losses {
		baseCell, armCell := row.cells[baselineArm], row.cells[arm]

		fmt.Fprintf(out, "| `%s` %s | %s | %s → %s | %s → %s | %s / %s |\n", row.taskID, escapeCell(row.question), row.kind,
			formatCount(baseCell.totalInput), formatCount(armCell.totalInput),
			formatCalls(baseCell.toolCalls), formatCalls(armCell.toolCalls),
			formatPercent(accuracy(baseCell)), formatPercent(accuracy(armCell)))
	}

	out.WriteString("\n")
}

func writePerQuestion(out *strings.Builder, rows []*questionRow, arms []string, judging bool) {
	out.WriteString("## Per question\n\n")

	header := "| Question | Kind | Arm | Input tokens | Tool calls | Tool output | Time | Section cited |"
	divider := "| --- | --- | --- | --- | --- | --- | --- | --- |"

	if judging {
		header += " Judged correct |"
		divider += " --- |"
	}

	out.WriteString(header + " Failed |\n" + divider + " --- |\n")

	for _, row := range rows {
		for _, arm := range arms {
			summary, found := row.cells[arm]

			if !found {
				continue
			}

			line := fmt.Sprintf("| `%s` | %s | %s | %s | %s | %s | %s | %s |", row.taskID, row.kind, arm,
				formatCount(summary.totalInput), formatCalls(summary.toolCalls), formatBytes(summary.toolOutput),
				formatSeconds(summary.durationS), formatPercent(summary.sectionRate))

			if judging {
				line += " " + formatPercent(summary.correctRate) + " |"
			}

			fmt.Fprintf(out, "%s %d |\n", line, summary.failed)
		}
	}

	out.WriteString("\n")
}

func escapeCell(text string) string {
	return strings.ReplaceAll(text, "|", `\|`)
}

func formatCount(value float64) string {
	if math.IsNaN(value) {
		return "–"
	}

	digits := fmt.Sprintf("%.0f", value)

	var grouped strings.Builder

	for index, digit := range digits {
		if index > 0 && (len(digits)-index)%3 == 0 {
			grouped.WriteByte(',')
		}

		grouped.WriteRune(digit)
	}

	return grouped.String()
}

func formatCalls(value float64) string {
	if math.IsNaN(value) {
		return "–"
	}

	return fmt.Sprintf("%g", value)
}

func formatBytes(value float64) string {
	if math.IsNaN(value) {
		return "–"
	}

	return fmt.Sprintf("%.1f KB", value/1000)
}

func formatSeconds(value float64) string {
	if math.IsNaN(value) {
		return "–"
	}

	return fmt.Sprintf("%.1f s", value)
}

func formatUSD(value float64) string {
	if math.IsNaN(value) {
		return "–"
	}

	return fmt.Sprintf("$%.3f", value)
}

func formatPercent(value float64) string {
	if math.IsNaN(value) {
		return "–"
	}

	return fmt.Sprintf("%.0f%%", value*100)
}

func formatPoints(value float64) string {
	if math.IsNaN(value) {
		return "–"
	}

	return fmt.Sprintf("%+.0f pp", value*100)
}

func formatRatio(value float64) string {
	if math.IsNaN(value) {
		return "–"
	}

	return fmt.Sprintf("%.2f", value)
}

func formatSigned(value float64) string {
	if math.IsNaN(value) {
		return "–"
	}

	return fmt.Sprintf("%+g", value)
}
