# E2E Tests Phase 1: Test Harness — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the test harness that compiles the `tusk` binary once, creates isolated temp DBs per test, runs commands via `exec.Command`, and loops scenarios over all 4 combinations (flag/env x text/json). Verify with one smoke test.

**Architecture:** A `tests/e2e/` package (external, no internal imports) containing `harness.go` (types + helpers), `main_test.go` (binary build in `TestMain`), and `task_lifecycle_test.go` with one smoke scenario to validate the harness works end-to-end.

**Tech Stack:** Go standard library (`os/exec`, `encoding/json`, `testing`, `regexp`), CGO for SQLite build.

---

### Task 1: `harness.go` — Core Types and Helpers

**Files:**
- Create: `tests/e2e/harness.go`

This file defines all shared types and utility functions. It is NOT a test file — it's compiled into the test binary but contains no `Test*` functions.

- [ ] **Step 1: Create the file with package declaration and imports**

```go
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
```

- [ ] **Step 2: Add the `Result` type**

```go
// Result holds the output of a single CLI invocation.
type Result struct {
	Stdout string
	Stderr string
	Err    error // non-nil if exit code != 0
}
```

- [ ] **Step 3: Add the `Env` type and `newEnv` constructor**

`Env` creates a temp DB file per test and knows how to run the binary with the right DB mode and format.

```go
// Env is the test environment for a single scenario run.
// Each Env gets its own temp SQLite database file.
type Env struct {
	t       *testing.T
	binPath string // path to compiled tusk binary
	dbPath  string // path to temp SQLite file
	dbMode  string // "flag" or "env"
	format  string // "text" or "json"
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
	tmpFile.Close()

	return &Env{
		t:       t,
		binPath: binPath,
		dbPath:  tmpFile.Name(),
		dbMode:  dbMode,
		format:  format,
	}
}
```

- [ ] **Step 4: Add the `Env.Run` method**

This method executes the tusk binary with the given args, injecting the DB path and format. It returns a `Result` and appends it to `Env.results` for inter-step reference.

```go
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
	fullArgs = append(fullArgs, expanded...)
	fullArgs = append(fullArgs, "--format", e.format)

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
```

- [ ] **Step 5: Add the `expandRefs` method for inter-step references**

This resolves `$N.field` in args by looking up previous step results. For JSON format, it parses the stored stdout as JSON. For text format, it uses a regex to extract the short_id from lines like "Created task XXXXXXXX".

```go
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
		fmt.Sscanf(parts[1], "%d", &idx)
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
```

- [ ] **Step 6: Add the `combinations` function**

```go
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
```

- [ ] **Step 7: Add the `Step`, `Scenario` types and `runScenarios` function**

```go
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
```

- [ ] **Step 8: Add assertion helper functions**

```go
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

// assertMatches fails if got does not match the regex pattern.
func assertMatches(t *testing.T, got, pattern string) {
	t.Helper()
	matched, err := regexp.MatchString(pattern, got)
	if err != nil {
		t.Fatalf("assertMatches: bad pattern %q: %v", pattern, err)
	}
	if !matched {
		t.Fatalf("assertMatches: %q does not match pattern %q", got, pattern)
	}
}

// jsonField extracts a field from a parsed JSON value.
// Supports dot-separated paths like "tags.0" for array indexing.
func jsonField(parsed any, path string) any {
	parts := strings.Split(path, ".")
	current := parsed
	for _, part := range parts {
		switch v := current.(type) {
		case map[string]any:
			current = v[part]
		case []any:
			idx := 0
			fmt.Sscanf(part, "%d", &idx)
			if idx < len(v) {
				current = v[idx]
			} else {
				return nil
			}
		default:
			return nil
		}
	}
	return current
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
```

- [ ] **Step 9: Verify the file compiles**

Run:
```bash
cd /Users/germanamz/projects/tusk && CGO_ENABLED=1 go vet ./tests/e2e/...
```

This will fail because `main_test.go` doesn't exist yet — that's expected. The `go vet` may report "no Go files" or similar. That's fine; we just want to check for syntax errors in `harness.go`. If it complains about no test files, that's OK — we'll add `main_test.go` next.

- [ ] **Step 10: Commit**

```bash
git add tests/e2e/harness.go
git commit -m "test(e2e): add test harness with Env, Step, Scenario types and helpers"
```

---

### Task 2: `main_test.go` — Binary Build in TestMain

**Files:**
- Create: `tests/e2e/main_test.go`

This file compiles the `tusk` binary once before all tests run. The binary path is stored in a package-level variable that `runScenarios` uses.

- [ ] **Step 1: Create the file**

```go
// tests/e2e/main_test.go
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// binPath is the path to the compiled tusk binary, set in TestMain.
var binPath string

func TestMain(m *testing.M) {
	// Build the tusk binary into a temp directory.
	tmpDir, err := os.MkdirTemp("", "tusk-e2e-bin-*")
	if err != nil {
		panic("creating temp dir for binary: " + err.Error())
	}
	defer os.RemoveAll(tmpDir)

	binPath = filepath.Join(tmpDir, "tusk")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/tusk")
	// Build from the project root. The test runs from tests/e2e/, so go up two levels.
	cmd.Dir = filepath.Join(mustGetwd(), "..", "..")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		panic("building tusk binary: " + err.Error())
	}

	os.Exit(m.Run())
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		panic("getwd: " + err.Error())
	}
	return wd
}
```

- [ ] **Step 2: Verify it compiles and runs (no tests yet, so exit 0)**

Run:
```bash
cd /Users/germanamz/projects/tusk && CGO_ENABLED=1 go test ./tests/e2e/... -v -count=1
```

Expected: `testing: warning: no tests to run` and exit 0. The binary should be built in the temp dir.

- [ ] **Step 3: Commit**

```bash
git add tests/e2e/main_test.go
git commit -m "test(e2e): add TestMain that builds tusk binary once for all e2e tests"
```

---

### Task 3: Smoke Test — Validate Harness End-to-End

**Files:**
- Create: `tests/e2e/task_lifecycle_test.go`

Add one simple scenario: create a task and verify the output. This proves the entire harness works (binary execution, DB modes, format modes, inter-step references, assertions).

- [ ] **Step 1: Create the file with the smoke scenario**

```go
// tests/e2e/task_lifecycle_test.go
package e2e

import "testing"

func TestTaskLifecycle(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "create_single_task",
			Steps: []Step{
				{
					Args: []string{"add", "Buy milk", "priority:3"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m, ok := parsed.(map[string]any)
						if !ok {
							t.Fatalf("expected JSON object, got %T", parsed)
						}
						assertEqual(t, m["title"], "Buy milk")
						assertEqual(t, m["status"], "pending")
						assertEqual(t, m["priority"], float64(3))
						if m["short_id"] == nil || m["short_id"] == "" {
							t.Fatal("expected short_id to be set")
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Created task")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
```

- [ ] **Step 2: Run the test**

Run:
```bash
cd /Users/germanamz/projects/tusk && CGO_ENABLED=1 go test ./tests/e2e/... -v -count=1 -run TestTaskLifecycle
```

Expected: 4 subtests all passing:
```
--- PASS: TestTaskLifecycle/create_single_task/flag/text
--- PASS: TestTaskLifecycle/create_single_task/flag/json
--- PASS: TestTaskLifecycle/create_single_task/env/text
--- PASS: TestTaskLifecycle/create_single_task/env/json
```

- [ ] **Step 3: Add a second scenario that tests inter-step references**

Add this scenario to the `scenarios` slice in `TestTaskLifecycle`, after the `create_single_task` scenario:

```go
		{
			Name: "create_then_start",
			Steps: []Step{
				{
					Args: []string{"add", "Reference test"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Created task")
					},
				},
				{
					Args: []string{"start", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "active")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Started task")
					},
				},
			},
		},
```

- [ ] **Step 4: Run the tests again**

Run:
```bash
cd /Users/germanamz/projects/tusk && CGO_ENABLED=1 go test ./tests/e2e/... -v -count=1 -run TestTaskLifecycle
```

Expected: 8 subtests all passing (4 for `create_single_task` + 4 for `create_then_start`).

- [ ] **Step 5: Commit**

```bash
git add tests/e2e/task_lifecycle_test.go
git commit -m "test(e2e): add smoke scenario validating harness with binary execution"
```
