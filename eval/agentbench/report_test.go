package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func boolRef(value bool) *bool {
	return &value
}

func syntheticRecords() []RunRecord {
	var records []RunRecord

	add := func(taskID string, kind string, arm string, tokens int, calls int, correct bool) {
		for rep := 1; rep <= 2; rep++ {
			records = append(records, RunRecord{
				TaskID: taskID, Kind: kind, Question: "What about " + taskID + "?", Arm: arm, Rep: rep,
				Model: "claude-sonnet-5-5", OK: true,
				TotalInputTokens: tokens, ToolCalls: calls, ToolOutputBytes: tokens / 2, DurationMS: int64(tokens), CostUSD: float64(tokens) / 1e6,
				SectionHit: correct, JudgeCorrect: boolRef(correct),
			})
		}
	}

	add("alpha", KindLexical, "grep", 40000, 5, true)
	add("alpha", KindLexical, "tusk", 20000, 2, true)
	add("beta", KindParaphrase, "grep", 60000, 8, false)
	add("beta", KindParaphrase, "tusk", 30000, 2, true)
	// gamma: tusk reads more and gets it wrong.
	add("gamma", KindLexical, "grep", 10000, 2, true)
	add("gamma", KindLexical, "tusk", 15000, 3, false)

	return records
}

func TestWriteReport(test *testing.T) {
	report := WriteReport(syntheticRecords(), nil, false)

	for _, fragment := range []string{
		"## Per arm",
		"| grep | 40,000 [25,000–50,000] | 5 [3.5–6.5] |",
		"## tusk vs grep, paired by question",
		"| Input tokens | 0.50 |",
		"| 2 of 3 |",
		"| Tool calls | -3 |",
		"| Judged correct | 67% | 67% | +0 pp |",
		"### Where tusk lost",
		"1 of 3 questions",
		"`gamma` What about gamma?",
		"## Per question",
	} {
		if !strings.Contains(report, fragment) {
			test.Errorf("report lacks %q:\n%s", fragment, report)
		}
	}
}

func TestWriteReportNumbersOnly(test *testing.T) {
	report := WriteReport(syntheticRecords(), nil, true)

	for _, leak := range []string{"What about", "alpha", "gamma", "## Per question"} {
		if strings.Contains(report, leak) {
			test.Errorf("numbers-only report contains %q:\n%s", leak, report)
		}
	}

	if !strings.Contains(report, "1 of 3 questions") {
		test.Errorf("numbers-only report lost the loss count:\n%s", report)
	}
}

func TestFailedRunsCountAgainstAccuracy(test *testing.T) {
	records := []RunRecord{
		{TaskID: "alpha", Arm: "tusk", Rep: 1, OK: true, TotalInputTokens: 100, SectionHit: true, JudgeCorrect: boolRef(true)},
		{TaskID: "alpha", Arm: "tusk", Rep: 2, OK: false, TotalInputTokens: 900, Error: "budget"},
	}

	summary := newCell(records, true)

	if summary.failed != 1 || summary.sectionRate != 0.5 || summary.correctRate != 0.5 {
		test.Errorf("cell = %+v", summary)
	}

	// The failed run spent tokens; they stay in the median.
	if summary.totalInput != 500 {
		test.Errorf("total input median = %v, want 500", summary.totalInput)
	}
}

func TestLoadRecordsKeepsLatestAndPendingJobsSkipsDone(test *testing.T) {
	path := filepath.Join(test.TempDir(), "runs.jsonl")
	lines := `{"task_id":"alpha","arm":"grep","rep":1,"ok":false,"error":"timeout"}
{"task_id":"alpha","arm":"grep","rep":1,"ok":true,"total_input_tokens":10}
{"task_id":"alpha","arm":"tusk","rep":1,"ok":false,"error":"timeout"}
`

	if writeErr := os.WriteFile(path, []byte(lines), 0o644); writeErr != nil {
		test.Fatal(writeErr)
	}

	records, loadErr := LoadRecords(path)

	if loadErr != nil {
		test.Fatal(loadErr)
	}

	if len(records) != 2 || !records[0].OK || records[0].TotalInputTokens != 10 {
		test.Errorf("records = %+v", records)
	}

	arms, _ := LookupArms("grep,tusk")

	jobs, jobsErr := pendingJobs(path, []Task{{ID: "alpha"}}, arms, 1, 1)

	if jobsErr != nil {
		test.Fatal(jobsErr)
	}

	if len(jobs) != 1 || jobs[0].Arm.Name != "tusk" {
		test.Errorf("pending jobs = %+v, want only the failed tusk run", jobs)
	}
}
