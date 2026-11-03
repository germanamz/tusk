package doctor

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/manifest"
)

// CheckKindPrefix starts the issue kind of a project check finding: a finding
// from [check.lint] is reported as check:lint, at the check's severity.
const CheckKindPrefix = "check:"

// DefaultCheckTimeout bounds a check that sets no timeout-seconds.
const DefaultCheckTimeout = 60 * time.Second

// CheckRunner runs a [check.<name>] command for doctor (#852). The command is
// shell text from tusk.toml, so running it runs whatever the manifest says:
// the CLI wires a runner only under `tusk doctor --checks`, and MCP never
// does. A nil runner means checks are validated but not run.
type CheckRunner interface {
	// Run runs command through the platform shell with dir as its working
	// directory and the caller's environment, and returns its exit code and
	// output. A command still running after timeout is stopped and reported
	// with TimedOut set. err is for a command that could not start at all; a
	// command that starts and fails is a result, not an error.
	Run(command, dir string, timeout time.Duration) (CheckResult, error)
}

// CheckResult is what a check's command did.
type CheckResult struct {
	// ExitCode is the command's exit status; meaningless when TimedOut.
	ExitCode int

	// Output is stdout and stderr as the command interleaved them. A runner
	// may cap it.
	Output []byte

	// TimedOut reports that the command ran past its timeout and was stopped.
	TimedOut bool
}

// CheckError is a [check.<name>] declaration CompileChecks rejected. Doctor
// reports it as check-invalid; reload lists it under warnings.
type CheckError struct {
	Name    string
	Message string
}

// Error renders the problem as reload lists it under warnings.
func (checkErr CheckError) Error() string {
	return fmt.Sprintf("check %q: %s", checkErr.Name, checkErr.Message)
}

// CompiledCheck is a valid [check.<name>] declaration, ready to run.
type CompiledCheck struct {
	Name        string
	Description string
	Command     string
	Severity    string
	Timeout     time.Duration
}

// knownCheckKeys lists the keys a [check.<name>] block accepts.
var knownCheckKeys = []string{"description", "command", "severity", "timeout-seconds"}

// CompileChecks validates every [check.<name>] declaration in loaded and
// returns the valid ones ready to run, plus one CheckError per problem with
// the rest, both sorted by check name. Pure: it reads the manifest and never
// fails, so doctor and reload can both call it.
func CompileChecks(loaded *manifest.Manifest) ([]CompiledCheck, []CheckError) {
	if loaded == nil || len(loaded.Checks) == 0 {
		return nil, nil
	}

	names := make([]string, 0, len(loaded.Checks))

	for name := range loaded.Checks {
		names = append(names, name)
	}

	sort.Strings(names)

	var (
		compiled  []CompiledCheck
		checkErrs []CheckError
	)

	for _, name := range names {
		check, problems := compileCheck(name, loaded.Checks[name])

		for _, problem := range problems {
			checkErrs = append(checkErrs, CheckError{Name: name, Message: problem})
		}

		if len(problems) == 0 {
			compiled = append(compiled, check)
		}
	}

	return compiled, checkErrs
}

// compileCheck returns every problem with one declaration, so one fix doesn't
// uncover the next.
func compileCheck(name string, check manifest.Check) (CompiledCheck, []string) {
	if check.DecodeError != "" {
		return CompiledCheck{}, []string{check.DecodeError}
	}

	var problems []string

	for _, key := range check.UnknownKeys {
		problems = append(problems, fmt.Sprintf("unknown key %q (a check takes %s)", key, strings.Join(knownCheckKeys, ", ")))
	}

	command := strings.TrimSpace(check.Command)

	if command == "" {
		problems = append(problems, "command is empty; set the shell command to run")
	}

	severity := check.Severity

	if severity == "" {
		severity = SeverityError
	}

	if !slices.Contains(ruleSeverities, severity) {
		problems = append(problems, fmt.Sprintf("severity %q is not one of %s", check.Severity, strings.Join(ruleSeverities, ", ")))
	}

	timeout := DefaultCheckTimeout

	switch {
	case check.TimeoutSeconds < 0:
		problems = append(problems, fmt.Sprintf("timeout-seconds %d is negative; leave it out for the %ds default", check.TimeoutSeconds, int(DefaultCheckTimeout/time.Second)))
	case check.TimeoutSeconds > 0:
		timeout = time.Duration(check.TimeoutSeconds) * time.Second
	}

	if len(problems) > 0 {
		return CompiledCheck{}, problems
	}

	return CompiledCheck{Name: name, Description: check.Description, Command: command, Severity: severity, Timeout: timeout}, nil
}

// runChecks reports a check-invalid issue per problem CompileChecks finds and,
// with a runner, runs each valid check in name order from the workspace root
// and reports its findings at the check's severity. Without a runner it
// returns how many valid checks it skipped. No-op without a manifest.
func runChecks(config Config) ([]Issue, int) {
	if config.Manifest == nil {
		return nil, 0
	}

	compiled, checkErrs := CompileChecks(config.Manifest)

	issues := make([]Issue, 0, len(checkErrs))

	for _, checkErr := range checkErrs {
		issues = append(issues, Issue{Kind: IssueCheckInvalid, NodeID: checkErr.Name, Message: checkErr.Message})
	}

	if config.Checks == nil {
		return issues, len(compiled)
	}

	for _, check := range compiled {
		result, runErr := config.Checks.Run(check.Command, config.Root, check.Timeout)

		issues = append(issues, checkFindings(check, result, runErr)...)
	}

	return issues, 0
}

// findingLine is one `path:line: message` line, the shape compilers and
// linters print (GNU style), optionally with a column after the line. The
// path holds no spaces or colons past a Windows drive letter, so a log line
// such as `2026-11-03 12:30:45: started` is not mistaken for one.
var findingLine = regexp.MustCompile(`^((?:[A-Za-z]:)?[^\s:]+):(\d+)(?::\d+)?:\s+(.+)$`)

// checkTailLines is how many of a failing check's last output lines a finding
// without a location quotes.
const checkTailLines = 3

// checkFindings turns one check's result into issues. Exit 0 passes,
// whatever the command printed. A failing command's `path:line: message`
// lines are one finding each, with path:line as the node id; a failing
// command that prints none is one finding quoting the end of its output.
func checkFindings(check CompiledCheck, result CheckResult, runErr error) []Issue {
	finding := func(nodeID, message string) Issue {
		return Issue{Kind: CheckKindPrefix + check.Name, Severity: check.Severity, NodeID: nodeID, Message: message}
	}

	switch {
	case runErr != nil:
		return []Issue{finding("", "could not run: "+runErr.Error())}
	case result.TimedOut:
		return []Issue{finding("", fmt.Sprintf("timed out after %ds", int(check.Timeout/time.Second)))}
	case result.ExitCode == 0:
		return nil
	}

	var (
		issues []Issue
		tail   []string
	)

	for line := range strings.Lines(string(result.Output)) {
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		if match := findingLine.FindStringSubmatch(line); match != nil {
			issues = append(issues, finding(match[1]+":"+match[2], match[3]))
		}

		tail = append(tail, line)
	}

	if len(issues) > 0 {
		return issues
	}

	message := fmt.Sprintf("exited %d", result.ExitCode)

	if check.Description != "" {
		message = fmt.Sprintf("%s (exited %d)", check.Description, result.ExitCode)
	}

	if len(tail) > checkTailLines {
		tail = tail[len(tail)-checkTailLines:]
	}

	if len(tail) > 0 {
		message += ": " + strings.Join(tail, "; ")
	}

	return []Issue{finding("", message)}
}
