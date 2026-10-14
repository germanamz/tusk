package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// agentSystemPrompt is the whole system prompt both arms get. It names no
// tool, so each arm finds its own way to the answer with the tools it has.
const agentSystemPrompt = `You answer questions about the documentation in the current working directory.

Look the answer up in the documentation with your tools; do not answer from memory. Then reply with a short answer and cite every section you relied on: the file path relative to the current directory and the section's heading, written exactly as it appears in the file. Cite the most specific heading that contains the answer.

If the documentation does not answer the question, say so.`

// answerSchema is the --json-schema both arms answer with.
const answerSchema = `{"type":"object","properties":{"answer":{"type":"string"},"citations":{"type":"array","items":{"type":"object","properties":{"file":{"type":"string"},"heading":{"type":"string"}},"required":["file","heading"]}}},"required":["answer","citations"]}`

// Job is one agent run: a task, an arm and a repetition number.
type Job struct {
	Task Task
	Arm  Arm
	Rep  int
}

// Key identifies a job across restarts, so a resumed benchmark skips the
// runs it already has.
func (job Job) Key() string {
	return fmt.Sprintf("%s|%s|%d", job.Task.ID, job.Arm.Name, job.Rep)
}

// RunRecord is one line of runs.jsonl: everything measured about one run.
type RunRecord struct {
	TaskID    string    `json:"task_id"`
	Kind      string    `json:"kind"`
	Question  string    `json:"question"`
	Arm       string    `json:"arm"`
	Rep       int       `json:"rep"`
	Model     string    `json:"model"`
	StartedAt time.Time `json:"started_at"`
	OK        bool      `json:"ok"`
	Error     string    `json:"error,omitempty"`

	ToolCalls        int            `json:"tool_calls"`
	ToolCallsByName  map[string]int `json:"tool_calls_by_name"`
	InputTokens      int            `json:"input_tokens"`
	CacheWriteTokens int            `json:"cache_write_tokens"`
	CacheReadTokens  int            `json:"cache_read_tokens"`
	TotalInputTokens int            `json:"total_input_tokens"`
	OutputTokens     int            `json:"output_tokens"`
	ToolOutputBytes  int            `json:"tool_output_bytes"`
	DurationMS       int64          `json:"duration_ms"`
	Turns            int            `json:"turns"`
	CostUSD          float64        `json:"cost_usd"`

	Answer     string     `json:"answer"`
	Citations  []Citation `json:"citations"`
	FileHit    bool       `json:"file_hit"`
	SectionHit bool       `json:"section_hit"`

	JudgeCorrect *bool   `json:"judge_correct,omitempty"`
	JudgeReason  string  `json:"judge_reason,omitempty"`
	JudgeCostUSD float64 `json:"judge_cost_usd,omitempty"`
}

// Key matches Job.Key.
func (record RunRecord) Key() string {
	return fmt.Sprintf("%s|%s|%d", record.TaskID, record.Arm, record.Rep)
}

// errToolMismatch marks a run whose tool list didn't match its arm. It
// stops the whole benchmark: every later run would be just as wrong.
var errToolMismatch = errors.New("tool list mismatch")

// Runner runs jobs against a stage.
type Runner struct {
	Claude     string
	Model      string
	MaxBudget  float64
	Timeout    time.Duration
	Stage      Stage
	Corpus     *Corpus
	MCPConfig  string
	TuskTools  []string
	StreamsDir string
	Judge      *Judge
}

// Run executes one job in a fresh copy of the stage and returns its record.
// The error is non-nil only for failures that should stop the benchmark;
// an agent that fails or errors out is a failed record, not an error.
func (runner *Runner) Run(ctx context.Context, job Job) (RunRecord, error) {
	record := RunRecord{
		TaskID:    job.Task.ID,
		Kind:      job.Task.Kind,
		Question:  job.Task.Question,
		Arm:       job.Arm.Name,
		Rep:       job.Rep,
		StartedAt: time.Now().UTC(),
	}

	runDir, tempErr := os.MkdirTemp("", "agentbench-run-")

	if tempErr != nil {
		return record, tempErr
	}

	defer func() { _ = os.RemoveAll(runDir) }()

	source := runner.Stage.Corpus

	if job.Arm.UsesTusk() {
		source = runner.Stage.Vault
	}

	if copyErr := copyPath(source, runDir); copyErr != nil {
		return record, fmt.Errorf("staging run: %w", copyErr)
	}

	args := []string{
		"--model", runner.Model,
		"--system-prompt", agentSystemPrompt,
		"--json-schema", answerSchema,
	}
	args = append(args, job.Arm.ToolArgs(runner.TuskTools, runner.MCPConfig)...)

	streamPath := filepath.Join(runner.StreamsDir, strings.NewReplacer("|", "__").Replace(job.Key())+".jsonl")

	summary, claudeErr := runClaude(ctx, runner.Claude, runDir, job.Task.Question, args, runner.MaxBudget, runner.Timeout, streamPath)

	record.Model = summary.Model

	if summary.Tools != nil {
		if checkErr := job.Arm.CheckTools(summary.Tools); checkErr != nil {
			return record, fmt.Errorf("%w: %w", errToolMismatch, checkErr)
		}
	}

	// A run that failed still spent tokens; keep them on the record.
	if summary.Result != nil {
		fillUsage(&record, summary)
	}

	if claudeErr != nil {
		record.Error = claudeErr.Error()

		return record, nil
	}

	if job.Arm.UsesTusk() && summary.MCPServers[tuskServer] != "connected" {
		record.Error = fmt.Sprintf("tusk MCP server status %q", summary.MCPServers[tuskServer])

		return record, nil
	}

	var answer struct {
		Answer    string     `json:"answer"`
		Citations []Citation `json:"citations"`
	}

	if decodeErr := json.Unmarshal(summary.Result.StructuredOutput, &answer); decodeErr != nil || answer.Answer == "" {
		record.Error = "no structured answer"

		return record, nil
	}

	record.OK = true
	record.Answer = answer.Answer
	record.Citations = answer.Citations

	score := ScoreCitations(runner.Corpus, job.Task, answer.Citations, runDir)
	record.FileHit = score.FileHit
	record.SectionHit = score.SectionHit

	if runner.Judge != nil {
		verdict, cost, judgeErr := runner.Judge.Grade(ctx, job.Task, runner.Corpus, answer.Answer)

		record.JudgeCostUSD = cost

		if judgeErr != nil {
			record.JudgeReason = "judge failed: " + judgeErr.Error()
		} else {
			record.JudgeCorrect = &verdict.Correct
			record.JudgeReason = verdict.Reason
		}
	}

	return record, nil
}

func fillUsage(record *RunRecord, summary StreamSummary) {
	result := summary.Result

	record.ToolCalls = summary.TotalToolCalls()
	record.ToolCallsByName = summary.ToolCalls
	record.InputTokens = result.Usage.InputTokens
	record.CacheWriteTokens = result.Usage.CacheCreationInputTokens
	record.CacheReadTokens = result.Usage.CacheReadInputTokens
	record.TotalInputTokens = result.Usage.TotalInput()
	record.OutputTokens = result.Usage.OutputTokens
	record.ToolOutputBytes = summary.ToolOutputBytes
	record.DurationMS = result.DurationMS
	record.Turns = result.NumTurns
	record.CostUSD = result.TotalCostUSD
}

// runClaude runs one headless Claude Code session in dir with prompt on
// stdin and returns its parsed stream. The flags shared by the agent and
// the judge live here: restricted mode (no user, project or local settings,
// no code-running tools, file tools confined to dir), no MCP server unless
// args adds one, no session left on disk, and nothing that would prompt.
// streamPath, when set, keeps the raw transcript.
func runClaude(ctx context.Context, claude string, dir string, prompt string, args []string, maxBudget float64, timeout time.Duration, streamPath string) (StreamSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fullArgs := []string{
		"-p",
		"--restricted",
		"--strict-mcp-config",
		"--no-session-persistence",
		"--disable-slash-commands",
		"--permission-mode", "dontAsk",
		"--output-format", "stream-json",
		"--verbose",
		"--max-budget-usd", fmt.Sprintf("%.2f", maxBudget),
	}
	fullArgs = append(fullArgs, args...)

	command := exec.CommandContext(ctx, claude, fullArgs...)
	command.Dir = dir
	command.Stdin = strings.NewReader(prompt)

	var stdout, stderr bytes.Buffer

	command.Stdout = &stdout
	command.Stderr = &stderr

	runErr := command.Run()

	if streamPath != "" {
		if writeErr := os.WriteFile(streamPath, stdout.Bytes(), 0o600); writeErr != nil {
			return StreamSummary{}, writeErr
		}
	}

	summary, parseErr := ParseStream(&stdout)

	if parseErr != nil {
		return summary, parseErr
	}

	if summary.Result == nil {
		return summary, fmt.Errorf("no result event (exit: %v): %s", runErr, tail(stderr.String(), 500))
	}

	if summary.Result.IsError || summary.Result.Subtype != "success" {
		return summary, fmt.Errorf("run ended with %s: %s", summary.Result.Subtype, tail(summary.Result.Result, 300))
	}

	return summary, nil
}

func tail(text string, limit int) string {
	text = strings.TrimSpace(text)

	if len(text) <= limit {
		return text
	}

	return "…" + text[len(text)-limit:]
}
