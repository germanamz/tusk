// Command agentbench measures what it costs an agent to answer questions
// about a markdown corpus with and without tusk: tool calls, input tokens,
// tool output, time, and whether the answer cites and states the right
// section. It drives Claude Code headless (claude -p) once per question,
// arm and repetition, and writes raw runs plus a markdown report.
//
// See eval/README.md for how to run it and docs/guide/benchmarks.md for the
// published results.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error

	switch os.Args[1] {
	case "run":
		err = runCommand(os.Args[2:])
	case "check":
		err = checkCommand(os.Args[2:])
	case "report":
		err = reportCommand(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "agentbench:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  agentbench check  --tasks FILE --paths DIR[,DIR] [--vault DIR]
  agentbench run    --tasks FILE --paths DIR[,DIR] --out DIR [flags]
  agentbench report --out DIR [--numbers-only]

Run "agentbench <command> -h" for flags.
`)
}

// checkCommand validates a task file against the corpus without staging
// or running anything, and prints how the questions spread over kinds and
// files, so a question set can be written and fixed in a tight loop.
func checkCommand(args []string) error {
	flags := flag.NewFlagSet("check", flag.ExitOnError)
	tasksPath := flags.String("tasks", "", "JSONL task file (required)")
	vault := flags.String("vault", ".", "vault root the corpus paths are relative to")
	paths := flags.String("paths", "", "comma-separated corpus paths, relative to --vault (required)")

	if parseErr := flags.Parse(args); parseErr != nil {
		return parseErr
	}

	if *tasksPath == "" || *paths == "" {
		flags.Usage()

		return errors.New("--tasks and --paths are required")
	}

	tasks, tasksErr := LoadTasks(*tasksPath)

	if tasksErr != nil {
		return tasksErr
	}

	corpus, corpusErr := LoadCorpus(*vault, strings.Split(*paths, ",")...)

	if corpusErr != nil {
		return corpusErr
	}

	if validateErr := ValidateTasks(tasks, corpus); validateErr != nil {
		return validateErr
	}

	kinds := make(map[string]int)
	files := make(map[string]int)

	for _, task := range tasks {
		kinds[task.Kind]++

		for _, file := range uniqueFiles(task.Targets) {
			files[file]++
		}
	}

	busiest := 0

	for _, count := range files {
		busiest = max(busiest, count)
	}

	fmt.Printf("%d questions valid: %d lexical, %d paraphrase; targets in %d of %d corpus files (at most %d questions per file)\n",
		len(tasks), kinds[KindLexical], kinds[KindParaphrase], len(files), corpus.FileCount(), busiest)

	return nil
}

func uniqueFiles(targets []Target) []string {
	var files []string

	for _, target := range targets {
		if !slices.Contains(files, target.File) {
			files = append(files, target.File)
		}
	}

	return files
}

type runOptions struct {
	tasks       string
	vault       string
	paths       string
	arms        string
	reps        int
	model       string
	judgeModel  string
	concurrency int
	tusk        string
	claude      string
	out         string
	maxBudget   float64
	timeout     time.Duration
	seed        uint64
	only        string
	label       string
}

func runCommand(args []string) error {
	var options runOptions

	flags := flag.NewFlagSet("run", flag.ExitOnError)
	flags.StringVar(&options.tasks, "tasks", "", "JSONL task file (required)")
	flags.StringVar(&options.vault, "vault", ".", "vault root holding tusk.toml and the corpus")
	flags.StringVar(&options.paths, "paths", "", "comma-separated corpus paths, relative to --vault (required)")
	flags.StringVar(&options.arms, "arms", "grep,tusk", "comma-separated arms: grep, tusk, both")
	flags.IntVar(&options.reps, "reps", 3, "repetitions per question and arm")
	flags.StringVar(&options.model, "model", "sonnet", "agent model, the same for every arm")
	flags.StringVar(&options.judgeModel, "judge-model", "haiku", `judge model; "" skips judging`)
	flags.IntVar(&options.concurrency, "concurrency", 4, "runs in flight at once")
	flags.StringVar(&options.tusk, "tusk", "bin/tusk", "tusk binary under test")
	flags.StringVar(&options.claude, "claude", "claude", "Claude Code binary")
	flags.StringVar(&options.out, "out", "", "output directory for runs.jsonl, meta.json, summary.md (required)")
	flags.Float64Var(&options.maxBudget, "max-budget", 1.0, "per-run spending cap in USD (claude --max-budget-usd)")
	flags.DurationVar(&options.timeout, "timeout", 10*time.Minute, "per-run timeout")
	flags.Uint64Var(&options.seed, "seed", 1, "seed for the run order")
	flags.StringVar(&options.only, "only", "", "comma-separated task ids to run (smoke tests)")
	flags.StringVar(&options.label, "label", "", "free-form label stored in meta.json")

	if parseErr := flags.Parse(args); parseErr != nil {
		return parseErr
	}

	if options.tasks == "" || options.paths == "" || options.out == "" {
		flags.Usage()

		return errors.New("--tasks, --paths and --out are required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	return runBenchmark(ctx, options)
}

func runBenchmark(ctx context.Context, options runOptions) error {
	tasks, tasksErr := LoadTasks(options.tasks)

	if tasksErr != nil {
		return tasksErr
	}

	tasks = filterTasks(tasks, options.only)

	arms, armsErr := LookupArms(options.arms)

	if armsErr != nil {
		return armsErr
	}

	tuskBin, tuskErr := resolveBinary(options.tusk)

	if tuskErr != nil {
		return tuskErr
	}

	streamsDir := filepath.Join(options.out, "streams")

	if mkdirErr := os.MkdirAll(streamsDir, 0o755); mkdirErr != nil {
		return mkdirErr
	}

	stageDir, stageDirErr := os.MkdirTemp("", "agentbench-stage-")

	if stageDirErr != nil {
		return stageDirErr
	}

	defer func() { _ = os.RemoveAll(stageDir) }()

	fmt.Fprintln(os.Stderr, "staging and indexing the corpus…")

	stage, stageErr := BuildStage(ctx, stageDir, options.vault, strings.Split(options.paths, ","), tuskBin)

	if stageErr != nil {
		return stageErr
	}

	corpus, corpusErr := LoadCorpus(stage.Corpus)

	if corpusErr != nil {
		return corpusErr
	}

	if validateErr := ValidateTasks(tasks, corpus); validateErr != nil {
		return validateErr
	}

	runner := &Runner{
		Claude:     options.claude,
		Model:      options.model,
		MaxBudget:  options.maxBudget,
		Timeout:    options.timeout,
		Stage:      stage,
		Corpus:     corpus,
		MCPConfig:  filepath.Join(stageDir, "mcp.json"),
		StreamsDir: streamsDir,
	}

	if slices.ContainsFunc(arms, Arm.UsesTusk) {
		tools, listErr := ListMCPTools(ctx, tuskBin, stage.Vault)

		if listErr != nil {
			return listErr
		}

		runner.TuskTools = tools

		if configErr := WriteMCPConfig(runner.MCPConfig, tuskBin); configErr != nil {
			return configErr
		}
	}

	if options.judgeModel != "" {
		runner.Judge = &Judge{Claude: options.claude, Model: options.judgeModel, MaxBudget: options.maxBudget, Timeout: options.timeout}
	}

	meta, metaErr := prepareMeta(ctx, options, tasks, corpus, arms, tuskBin)

	if metaErr != nil {
		return metaErr
	}

	runsPath := filepath.Join(options.out, "runs.jsonl")

	jobs, jobsErr := pendingJobs(runsPath, tasks, arms, options.reps, options.seed)

	if jobsErr != nil {
		return jobsErr
	}

	fmt.Fprintf(os.Stderr, "%d runs to go (%d questions × %d arms × %d reps, finished runs skipped)\n", len(jobs), len(tasks), len(arms), options.reps)

	if executeErr := executeJobs(ctx, runner, jobs, runsPath, options.concurrency); executeErr != nil {
		return executeErr
	}

	records, recordsErr := LoadRecords(runsPath)

	if recordsErr != nil {
		return recordsErr
	}

	report, resultsErr := writeResults(options.out, records, meta)

	if resultsErr != nil {
		return resultsErr
	}

	fmt.Print(report)

	return nil
}

// writeResults writes summary.md and charts/ into out and returns the
// report for printing. summary.md ends with the charts; the terminal gets
// the tables only.
func writeResults(out string, records []RunRecord, meta *Meta) (string, error) {
	report := WriteReport(records, meta, false)
	charts := BuildCharts(records)

	if chartsErr := WriteCharts(out, charts); chartsErr != nil {
		return "", chartsErr
	}

	summary := report + "## Charts\n\n" + ChartGallery(charts, "")

	if writeErr := os.WriteFile(filepath.Join(out, "summary.md"), []byte(summary), 0o644); writeErr != nil {
		return "", writeErr
	}

	return report, nil
}

func filterTasks(tasks []Task, only string) []Task {
	if only == "" {
		return tasks
	}

	ids := strings.Split(only, ",")

	return slices.DeleteFunc(tasks, func(task Task) bool { return !slices.Contains(ids, task.ID) })
}

// resolveBinary makes a binary path absolute: runs start in scratch
// directories, so a relative path like bin/tusk would no longer resolve.
func resolveBinary(binary string) (string, error) {
	if !strings.ContainsRune(binary, filepath.Separator) {
		return exec.LookPath(binary)
	}

	absolute, absErr := filepath.Abs(binary)

	if absErr != nil {
		return "", absErr
	}

	if _, statErr := os.Stat(absolute); statErr != nil {
		return "", fmt.Errorf("tusk binary: %w (run make build)", statErr)
	}

	return absolute, nil
}

// prepareMeta writes meta.json, or checks the one a previous invocation
// left: resuming into a directory that holds a different benchmark (other
// questions, model or repetitions) would mix incomparable runs.
func prepareMeta(ctx context.Context, options runOptions, tasks []Task, corpus *Corpus, arms []Arm, tuskBin string) (*Meta, error) {
	taskBytes, readErr := os.ReadFile(options.tasks)

	if readErr != nil {
		return nil, readErr
	}

	digest := sha256.Sum256(taskBytes)

	var armNames []string

	for _, arm := range arms {
		armNames = append(armNames, arm.Name)
	}

	meta := &Meta{
		Label:          options.label,
		TasksSHA256:    hex.EncodeToString(digest[:]),
		Questions:      len(tasks),
		Paths:          strings.Split(options.paths, ","),
		CorpusFiles:    corpus.FileCount(),
		CorpusSections: corpus.SectionCount(),
		Arms:           armNames,
		Reps:           options.reps,
		Model:          options.model,
		JudgeModel:     options.judgeModel,
		Concurrency:    options.concurrency,
		TuskVersion:    TuskVersion(ctx, tuskBin),
		ClaudeVersion:  claudeVersion(ctx, options.claude),
		StartedAt:      time.Now().UTC(),
	}

	metaPath := filepath.Join(options.out, "meta.json")

	if existing, existingErr := os.ReadFile(metaPath); existingErr == nil {
		var previous Meta

		if decodeErr := json.Unmarshal(existing, &previous); decodeErr != nil {
			return nil, fmt.Errorf("decoding %s: %w", metaPath, decodeErr)
		}

		if previous.TasksSHA256 != meta.TasksSHA256 || previous.Model != meta.Model || previous.Reps != meta.Reps || previous.JudgeModel != meta.JudgeModel {
			return nil, fmt.Errorf("%s holds a different benchmark (tasks, model, reps or judge changed); use a new --out", options.out)
		}

		meta.StartedAt = previous.StartedAt
		meta.Arms = mergeArms(previous.Arms, meta.Arms)
	}

	encoded, encodeErr := json.MarshalIndent(meta, "", "  ")

	if encodeErr != nil {
		return nil, encodeErr
	}

	return meta, os.WriteFile(metaPath, append(encoded, '\n'), 0o644)
}

func mergeArms(previous []string, current []string) []string {
	merged := slices.Clone(previous)

	for _, arm := range current {
		if !slices.Contains(merged, arm) {
			merged = append(merged, arm)
		}
	}

	return merged
}

func claudeVersion(ctx context.Context, claude string) string {
	output, versionErr := exec.CommandContext(ctx, claude, "--version").Output()

	if versionErr != nil {
		return "unknown"
	}

	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(string(output)), "(Claude Code)"))
}

// pendingJobs lists every question × arm × repetition that has no
// successful record yet, in a shuffled order so the arms share whatever
// load the machine is under rather than running back to back.
func pendingJobs(runsPath string, tasks []Task, arms []Arm, reps int, seed uint64) ([]Job, error) {
	done := make(map[string]bool)

	records, loadErr := LoadRecords(runsPath)

	if loadErr != nil && !errors.Is(loadErr, os.ErrNotExist) {
		return nil, loadErr
	}

	for _, record := range records {
		if record.OK {
			done[record.Key()] = true
		}
	}

	var jobs []Job

	for _, task := range tasks {
		for _, arm := range arms {
			for rep := 1; rep <= reps; rep++ {
				job := Job{Task: task, Arm: arm, Rep: rep}

				if !done[job.Key()] {
					jobs = append(jobs, job)
				}
			}
		}
	}

	random := rand.New(rand.NewPCG(seed, seed))
	random.Shuffle(len(jobs), func(left, right int) { jobs[left], jobs[right] = jobs[right], jobs[left] })

	return jobs, nil
}

// executeJobs runs jobs with a bounded worker pool and appends each record
// to runsPath as it lands, so an interrupted benchmark resumes where it
// stopped. A tool-list mismatch cancels everything still queued.
func executeJobs(ctx context.Context, runner *Runner, jobs []Job, runsPath string, concurrency int) error {
	runsFile, openErr := os.OpenFile(runsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)

	if openErr != nil {
		return openErr
	}

	defer runsFile.Close()

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	queue := make(chan Job)

	var (
		mutex    sync.Mutex
		finished int
		workers  sync.WaitGroup
	)

	for range max(concurrency, 1) {
		workers.Go(func() {
			for job := range queue {
				record, fatalErr := runner.Run(ctx, job)

				if fatalErr != nil {
					cancel(fatalErr)

					continue
				}

				if ctx.Err() != nil {
					continue
				}

				mutex.Lock()

				finished++

				appendErr := appendRecord(runsFile, record)

				if appendErr != nil {
					cancel(appendErr)
				}

				fmt.Fprintf(os.Stderr, "[%*d/%d] %s\n", len(fmt.Sprint(len(jobs))), finished, len(jobs), describe(record))

				mutex.Unlock()
			}
		})
	}

	for _, job := range jobs {
		select {
		case queue <- job:
		case <-ctx.Done():
		}

		if ctx.Err() != nil {
			break
		}
	}

	close(queue)
	workers.Wait()

	if cause := context.Cause(ctx); cause != nil {
		if errors.Is(cause, context.Canceled) {
			return errors.New("interrupted; rerun the same command to resume")
		}

		return cause
	}

	return nil
}

func appendRecord(file *os.File, record RunRecord) error {
	encoded, encodeErr := json.Marshal(record)

	if encodeErr != nil {
		return encodeErr
	}

	_, writeErr := file.Write(append(encoded, '\n'))

	return writeErr
}

func describe(record RunRecord) string {
	head := fmt.Sprintf("%-4s %s #%d", record.Arm, record.TaskID, record.Rep)

	if !record.OK {
		return head + "  FAILED: " + tail(record.Error, 200)
	}

	judged := "-"

	if record.JudgeCorrect != nil {
		judged = mark(*record.JudgeCorrect)
	}

	return fmt.Sprintf("%s  %s tok  %d calls  %.1fs  section %s  judge %s", head,
		formatCount(float64(record.TotalInputTokens)), record.ToolCalls, float64(record.DurationMS)/1000, mark(record.SectionHit), judged)
}

func mark(hit bool) string {
	if hit {
		return "✓"
	}

	return "✗"
}

func reportCommand(args []string) error {
	flags := flag.NewFlagSet("report", flag.ExitOnError)
	out := flags.String("out", "", "benchmark output directory holding runs.jsonl (required); summary.md and charts/ are rewritten there")
	numbersOnly := flags.Bool("numbers-only", false, "aggregate figures only: no question text or task ids, no charts, nothing written to disk")

	if parseErr := flags.Parse(args); parseErr != nil {
		return parseErr
	}

	if *out == "" {
		flags.Usage()

		return errors.New("--out is required")
	}

	records, recordsErr := LoadRecords(filepath.Join(*out, "runs.jsonl"))

	if recordsErr != nil {
		return recordsErr
	}

	var meta *Meta

	if encoded, metaErr := os.ReadFile(filepath.Join(*out, "meta.json")); metaErr == nil {
		meta = &Meta{}

		if decodeErr := json.Unmarshal(encoded, meta); decodeErr != nil {
			return decodeErr
		}
	}

	if *numbersOnly {
		fmt.Print(WriteReport(records, meta, true))

		return nil
	}

	report, resultsErr := writeResults(*out, records, meta)

	if resultsErr != nil {
		return resultsErr
	}

	fmt.Print(report)

	return nil
}
