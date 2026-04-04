// tests/e2e/harness.go
package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// Result holds the output of a single CLI invocation.
type Result struct {
	Stdout string
	Stderr string
	Err    error // non-nil if exit code != 0
}

// Env is the test environment for a single scenario run.
// Each Env gets its own temp SQLite database file.
type Env struct {
	t       *testing.T
	binPath string   // path to compiled tusk binary
	dbPath  string   // path to temp SQLite file
	dbMode  string   // "flag" or "env"
	format  string   // "text" or "json"
	results []Result // stored results for inter-step references
}

// newEnv creates a new Env with a fresh temp DB file.
// binPath is the path to the compiled tusk binary (set in TestMain).
// dbMode is "flag" (pass --db) or "env" (set TUSK_DB env var).
// format is "text" or "json" (appended as --format to every command).
func newEnv(t *testing.T, binPath, dbMode, format string) *Env {
	t.Helper()
	tmpFile, err := os.CreateTemp(t.TempDir(), "tusk-e2e-*.db")
	if err != nil {
		t.Fatalf("creating temp db: %v", err)
	}
	_ = tmpFile.Close()

	return &Env{
		t:       t,
		binPath: binPath,
		dbPath:  tmpFile.Name(),
		dbMode:  dbMode,
		format:  format,
	}
}

// Run executes the tusk binary with the given arguments.
// It automatically injects --db or TUSK_DB and --format based on the Env config.
// The result is stored in Env.results for inter-step references.
func (e *Env) Run(args ...string) Result {
	e.t.Helper()

	// Expand $N.field references in args
	expanded := make([]string, len(args))
	for i, arg := range args {
		expanded[i] = e.expandRefs(arg)
	}

	// Build the full argument list
	var fullArgs []string
	if e.dbMode == "flag" {
		fullArgs = append(fullArgs, "--db", e.dbPath)
	}
	fullArgs = append(fullArgs, "--format", e.format)
	fullArgs = append(fullArgs, expanded...)

	cmd := exec.Command(e.binPath, fullArgs...)

	// Set TUSK_DB env var if using env mode
	if e.dbMode == "env" {
		cmd.Env = append(os.Environ(), "TUSK_DB="+e.dbPath)
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	r := Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Err:    err,
	}
	e.results = append(e.results, r)
	return r
}

// shortIDPattern matches 8+ hex character short IDs in mutation output lines.
// Examples: "Created task a3f8b2c1", "Modified task b7c9d4e2"
var shortIDPattern = regexp.MustCompile(`(?:Created|Modified|Started|Completed|Deleted|Annotated) task ([0-9a-f]{8,})`)

// expandRefs replaces $N.field references with values from previous step results.
// For example, "$0.short_id" is replaced with the short_id from step 0's output.
func (e *Env) expandRefs(arg string) string {
	if !strings.Contains(arg, "$") {
		return arg
	}

	// Match $N.field pattern
	refPattern := regexp.MustCompile(`\$(\d+)\.(\w+)`)
	return refPattern.ReplaceAllStringFunc(arg, func(match string) string {
		parts := refPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		idx := 0
		_, _ = fmt.Sscanf(parts[1], "%d", &idx)
		field := parts[2]

		if idx >= len(e.results) {
			e.t.Fatalf("reference $%d.%s: step %d has not run yet (only %d results)", idx, field, idx, len(e.results))
		}

		prev := e.results[idx]

		// Try JSON parse first (works for both formats since we store raw stdout)
		var parsed map[string]any
		if err := json.Unmarshal([]byte(prev.Stdout), &parsed); err == nil {
			if val, ok := parsed[field]; ok {
				return fmt.Sprintf("%v", val)
			}
		}

		// Fallback: extract short_id from text output using regex
		if field == "short_id" {
			if m := shortIDPattern.FindStringSubmatch(prev.Stdout); len(m) == 2 {
				return m[1]
			}
		}

		e.t.Fatalf("reference $%d.%s: could not resolve from output:\n%s", idx, field, prev.Stdout)
		return match
	})
}

// combinations returns the cartesian product of all provided string slices.
// Example: combinations([]string{"a","b"}, []string{"1","2"})
// returns: [["a","1"], ["a","2"], ["b","1"], ["b","2"]]
func combinations(lists ...[]string) [][]string {
	if len(lists) == 0 {
		return [][]string{{}}
	}
	rest := combinations(lists[1:]...)
	var result [][]string
	for _, item := range lists[0] {
		for _, combo := range rest {
			row := make([]string, 0, len(combo)+1)
			row = append(row, item)
			row = append(row, combo...)
			result = append(result, row)
		}
	}
	return result
}

// Step is a single CLI command invocation within a scenario.
type Step struct {
	// Args are the CLI arguments (without --db, --format — those are injected by Env).
	// Supports $N.field references to previous step outputs.
	Args []string

	// WantErr indicates that this step should produce a non-zero exit code.
	WantErr bool

	// Assert runs for both text and json formats.
	Assert func(t *testing.T, r Result)

	// AssertJSON runs only when format is "json". parsed is the unmarshaled stdout.
	AssertJSON func(t *testing.T, parsed any)

	// AssertText runs only when format is "text". output is raw stdout.
	AssertText func(t *testing.T, output string)
}

// Scenario is a named sequence of Steps that tests a specific workflow.
type Scenario struct {
	Name  string
	Steps []Step
}

// runScenarios runs each scenario across all 4 combinations (flag/env x text/json).
// binPath must be set before calling this (typically in TestMain).
func runScenarios(t *testing.T, binPath string, scenarios []Scenario) {
	combos := combinations(
		[]string{"flag", "env"},
		[]string{"text", "json"},
	)
	for _, sc := range scenarios {
		for _, combo := range combos {
			dbMode, format := combo[0], combo[1]
			name := sc.Name + "/" + dbMode + "/" + format
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				env := newEnv(t, binPath, dbMode, format)
				for i, step := range sc.Steps {
					r := env.Run(step.Args...)

					if step.WantErr && r.Err == nil {
						t.Fatalf("step %d: expected error, got none. stdout:\n%s", i, r.Stdout)
					}
					if !step.WantErr && r.Err != nil {
						t.Fatalf("step %d: unexpected error: %v\nstderr: %s\nstdout: %s", i, r.Err, r.Stderr, r.Stdout)
					}

					if step.Assert != nil {
						step.Assert(t, r)
					}

					if format == "json" && step.AssertJSON != nil {
						var parsed any
						if err := json.Unmarshal([]byte(r.Stdout), &parsed); err != nil {
							t.Fatalf("step %d: failed to parse JSON stdout: %v\nraw:\n%s", i, err, r.Stdout)
						}
						step.AssertJSON(t, parsed)
					}

					if format == "text" && step.AssertText != nil {
						step.AssertText(t, r.Stdout)
					}
				}
			})
		}
	}
}

// assertEqual fails the test if got != want.
func assertEqual(t *testing.T, got, want any) {
	t.Helper()
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Fatalf("assertEqual: got %v, want %v", got, want)
	}
}

// assertContains fails if substr is not found in got.
func assertContains(t *testing.T, got, substr string) {
	t.Helper()
	if !strings.Contains(got, substr) {
		t.Fatalf("assertContains: %q not found in:\n%s", substr, got)
	}
}

// assertNotContains fails if substr IS found in got.
func assertNotContains(t *testing.T, got, substr string) {
	t.Helper()
	if strings.Contains(got, substr) {
		t.Fatalf("assertNotContains: %q unexpectedly found in:\n%s", substr, got)
	}
}

// jsonArray asserts the parsed value is a JSON array and returns it.
func jsonArray(t *testing.T, parsed any) []any {
	t.Helper()
	arr, ok := parsed.([]any)
	if !ok {
		t.Fatalf("jsonArray: expected []any, got %T", parsed)
	}
	return arr
}

// assertStderrContains fails if substr is not found in r.Stderr.
func assertStderrContains(t *testing.T, r Result, substr string) {
	t.Helper()
	if !strings.Contains(r.Stderr, substr) {
		t.Fatalf("assertStderrContains: %q not found in stderr:\n%s", substr, r.Stderr)
	}
}
