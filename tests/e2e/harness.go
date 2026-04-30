// Package e2e drives the tusk binary as a black-box subprocess.
//
// All test commands route through newCmd, which sets cmd.Dir to a
// per-call test.TempDir(). Tusk's walk-up config resolver therefore
// never reaches the repo-root tusk.toml. Tests that need to exercise
// walk-up explicitly call env.InDir(...) to point CWD at a controlled
// directory. No test should construct exec.Command directly — newCmd
// is the only sanctioned construction path so the isolation invariant
// cannot drift.
//
// Caveat: cmd.Env starts from os.Environ(), so a developer shell with
// TUSK_CONFIG or TUSK_DB exported leaks into every test. CI runners
// have clean environments; locally, run `unset TUSK_CONFIG TUSK_DB`
// before `make test-e2e` if you need to verify isolation.

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

// newCmd returns an exec.Cmd that runs binPath with args. cmd.Dir is set
// to a per-call test.TempDir() so tusk's walk-up config resolver never
// reaches an ancestor's tusk.toml. cmd.Env starts as os.Environ() with
// NO_COLOR=1 appended; callers append further env vars and set Stdin /
// Stdout / Stderr as needed.
func newCmd(test *testing.T, binPath string, args ...string) *exec.Cmd {
	test.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Dir = test.TempDir()
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	return cmd
}

// currentStep carries per-step context used by Env.Run. runScenarios sets
// it before invoking Run so optional Stdin/Cwd fields propagate to the
// command without permanently mutating Env state.
type currentStep struct {
	stdin string
	cwd   string
}

// Result holds the output of a single CLI invocation.
type Result struct {
	Stdout string
	Stderr string
	Err    error // non-nil if exit code != 0
}

// Env is the test environment for a single scenario run.
// Each Env gets its own temp SQLite database file.
type Env struct {
	test       *testing.T
	binPath    string   // path to compiled tusk binary
	dbPath     string   // path to temp SQLite file
	configDir  string   // path to temp config directory (optional)
	workDir    string   // working directory for cmd.Dir (optional)
	extraEnv   []string // additional env vars appended to every Run (optional)
	homeDir    string   // set by WithHome; overrides HOME / USERPROFILE
	skipDBArg  bool     // set by WithoutDBArg; suppress --db / TUSK_DB injection
	skipFormat bool     // set by WithoutFormat; suppress --format injection
	dbMode     string   // "flag" or "env"
	format     string   // "text" or "json"
	results    []Result // stored results for inter-step references
	step       currentStep
}

// InDir sets the working directory used for subsequent Run invocations.
// Used by walk-up scenarios that need the binary to start inside a
// specific temp directory.
func (env *Env) InDir(dir string) { env.workDir = dir }

// WithEnv appends a KEY=VALUE environment variable to every subsequent Run
// invocation. Used by scenarios that need to inject extra env vars (e.g.
// TUSK_CONFIG) without adding first-class fields to Env.
func (env *Env) WithEnv(key, value string) {
	env.extraEnv = append(env.extraEnv, key+"="+value)
}

// WithHome overrides HOME (and USERPROFILE on Windows) for every
// subsequent Run invocation. Used by tests that drive tusk's config
// resolver from a synthetic home directory. Also clears configDir so
// TUSK_CONFIG_DIR is no longer injected — otherwise tusk's resolver
// (TUSK_CONFIG_DIR > ~/.config/tusk) would shadow the HOME-based
// lookup the caller is trying to exercise.
func (env *Env) WithHome(dir string) {
	env.homeDir = dir
	env.configDir = ""
}

// WithoutDBArg suppresses both the --db flag and TUSK_DB env var on
// every subsequent Run invocation, regardless of dbMode. Used by
// tests that exercise storage.path resolution from a config file.
func (env *Env) WithoutDBArg() {
	env.skipDBArg = true
}

// WithoutFormat suppresses the --format flag on every subsequent Run
// invocation. Used by tests that assert tusk's default output format.
func (env *Env) WithoutFormat() {
	env.skipFormat = true
}

// newEnv creates a new Env with a fresh temp DB file.
// binPath is the path to the compiled tusk binary (set in TestMain).
// dbMode is "flag" (pass --db) or "env" (set TUSK_DB env var).
// format is "text" or "json" (appended as --format to every command).
func newEnv(test *testing.T, binPath, dbMode, format string) *Env {
	test.Helper()
	tmpFile, err := os.CreateTemp(test.TempDir(), "tusk-e2e-*.db")

	if err != nil {
		test.Fatalf("creating temp db: %v", err)
	}

	_ = tmpFile.Close()

	// Point the binary at an isolated empty config directory by default so
	// it never reads the developer's real ~/.config/tusk/config.toml. The
	// post-phase-2 legacy-section guard hard-errors on any stale
	// [projects.*] / [workflows.*] sections, which would otherwise make
	// every e2e scenario fail on contributor machines that haven't yet
	// cleaned up their global config.
	return &Env{
		test:      test,
		binPath:   binPath,
		dbPath:    tmpFile.Name(),
		configDir: test.TempDir(),
		workDir:   test.TempDir(),
		dbMode:    dbMode,
		format:    format,
	}
}

// Run executes the tusk binary with the given arguments.
// It automatically injects --db or TUSK_DB and --format based on the Env config.
// The result is stored in Env.results for inter-step references.
func (env *Env) Run(args ...string) Result {
	env.test.Helper()

	expanded := make([]string, len(args))
	for index, arg := range args {
		expanded[index] = env.expandRefs(arg)
	}

	var fullArgs []string
	if env.dbMode == "flag" && !env.skipDBArg {
		fullArgs = append(fullArgs, "--db", env.dbPath)
	}
	if !env.skipFormat {
		fullArgs = append(fullArgs, "--format", env.format)
	}
	fullArgs = append(fullArgs, expanded...)

	cmd := newCmd(env.test, env.binPath, fullArgs...)

	if env.step.cwd != "" {
		cmd.Dir = env.step.cwd
	} else if env.workDir != "" {
		cmd.Dir = env.workDir
	}
	if env.step.stdin != "" {
		cmd.Stdin = strings.NewReader(env.step.stdin)
	}

	if env.homeDir != "" {
		cmd.Env = append(cmd.Env, "HOME="+env.homeDir, "USERPROFILE="+env.homeDir)
	}
	if env.dbMode == "env" && !env.skipDBArg {
		cmd.Env = append(cmd.Env, "TUSK_DB="+env.dbPath)
	}
	if env.configDir != "" {
		cmd.Env = append(cmd.Env, "TUSK_CONFIG_DIR="+env.configDir)
	}
	cmd.Env = append(cmd.Env, env.extraEnv...)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	result := Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Err:    runErr,
	}
	env.results = append(env.results, result)
	return result
}

// shortIDPattern matches 8+ hex character short IDs in mutation output lines.
// Covers both task and note mutations. Examples:
//
//	"Created task a3f8b2c1"
//	"Created note 6ea3366c"
//	"Archived note 5f3e2d1c"
var shortIDPattern = regexp.MustCompile(`(?:Created|Modified|Started|Completed|Deleted|Annotated|Archived) (?:task|note) ([0-9a-f]{8,})`)

// expandRefs replaces $N.field references with values from previous step results.
// For example, "$0.short_id" is replaced with the short_id from step 0's output.
// Dotted paths ("$0.note.id") descend into nested JSON objects.
func (env *Env) expandRefs(arg string) string {
	if !strings.Contains(arg, "$") {
		return arg
	}

	refPattern := regexp.MustCompile(`\$(\d+)\.([\w.]+)`)
	return refPattern.ReplaceAllStringFunc(arg, func(match string) string {
		parts := refPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		idx := 0
		_, _ = fmt.Sscanf(parts[1], "%d", &idx)
		field := parts[2]

		if idx >= len(env.results) {
			env.test.Fatalf("reference $%d.%s: step %d has not run yet (only %d results)", idx, field, idx, len(env.results))
		}

		prev := env.results[idx]

		var parsed map[string]any
		if parseErr := json.Unmarshal([]byte(prev.Stdout), &parsed); parseErr == nil {
			segments := strings.Split(field, ".")
			var cur any = parsed
			ok := true
			for _, seg := range segments {
				mapped, isMap := cur.(map[string]any)
				if !isMap {
					ok = false
					break
				}
				val, exists := mapped[seg]
				if !exists {
					ok = false
					break
				}
				cur = val
			}
			if ok {
				return fmt.Sprintf("%v", cur)
			}
		}

		if field == "short_id" || field == "id" || strings.HasSuffix(field, ".id") {
			if matched := shortIDPattern.FindStringSubmatch(prev.Stdout); len(matched) == 2 {
				return matched[1]
			}
		}

		env.test.Fatalf("reference $%d.%s: could not resolve from output:\n%s", idx, field, prev.Stdout)
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

	// Stdin is piped to the command's standard input when non-empty.
	Stdin string

	// Cwd overrides the working directory for this step only.
	Cwd string

	// Setup runs before the step executes. Receives a test.TempDir() scratch
	// directory for seeding inputs; return a Cwd override (or "") so the
	// step can run in that directory. Runs before Args are expanded.
	Setup func(test *testing.T, dir string) (cwd string)

	// Assert runs for both text and json formats.
	Assert func(test *testing.T, result Result)

	// AssertJSON runs only when format is "json". parsed is the unmarshaled stdout.
	AssertJSON func(test *testing.T, parsed any)

	// AssertText runs only when format is "text". output is raw stdout.
	AssertText func(test *testing.T, output string)
}

// Scenario is a named sequence of Steps that tests a specific workflow.
type Scenario struct {
	Name  string
	Steps []Step
}

// runScenarios runs each scenario across all 4 combinations (flag/env x text/json).
// binPath must be set before calling this (typically in TestMain).
func runScenarios(test *testing.T, binPath string, scenarios []Scenario) {
	combos := combinations(
		[]string{"flag", "env"},
		[]string{"text", "json"},
	)
	for _, scenario := range scenarios {
		for _, combo := range combos {
			dbMode, format := combo[0], combo[1]
			name := scenario.Name + "/" + dbMode + "/" + format
			test.Run(name, func(test *testing.T) {
				test.Parallel()
				env := newEnv(test, binPath, dbMode, format)
				for stepIndex, step := range scenario.Steps {
					env.step = currentStep{stdin: step.Stdin, cwd: step.Cwd}
					if step.Setup != nil {
						dir := test.TempDir()
						cwd := step.Setup(test, dir)
						if cwd != "" {
							env.step.cwd = cwd
						}
					}
					result := env.Run(step.Args...)
					env.step = currentStep{}

					if step.WantErr && result.Err == nil {
						test.Fatalf("step %d: expected error, got none. stdout:\n%s", stepIndex, result.Stdout)
					}
					if !step.WantErr && result.Err != nil {
						test.Fatalf("step %d: unexpected error: %v\nstderr: %s\nstdout: %s", stepIndex, result.Err, result.Stderr, result.Stdout)
					}

					if step.Assert != nil {
						step.Assert(test, result)
					}

					if format == "json" && step.AssertJSON != nil {
						var parsed any
						if parseErr := json.Unmarshal([]byte(result.Stdout), &parsed); parseErr != nil {
							test.Fatalf("step %d: failed to parse JSON stdout: %v\nraw:\n%s", stepIndex, parseErr, result.Stdout)
						}

						step.AssertJSON(test, parsed)
					}

					if format == "text" && step.AssertText != nil {
						step.AssertText(test, result.Stdout)
					}
				}
			})
		}
	}
}

// assertEqual fails the test if got != want.
func assertEqual(test *testing.T, got, want any) {
	test.Helper()
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		test.Fatalf("assertEqual: got %v, want %v", got, want)
	}
}

// assertContains fails if substr is not found in got.
func assertContains(test *testing.T, got, substr string) {
	test.Helper()
	if !strings.Contains(got, substr) {
		test.Fatalf("assertContains: %q not found in:\n%s", substr, got)
	}
}

// assertNotContains fails if substr IS found in got.
func assertNotContains(test *testing.T, got, substr string) {
	test.Helper()
	if strings.Contains(got, substr) {
		test.Fatalf("assertNotContains: %q unexpectedly found in:\n%s", substr, got)
	}
}

// jsonArray asserts the parsed value is a JSON array and returns it.
func jsonArray(test *testing.T, parsed any) []any {
	test.Helper()
	arr, ok := parsed.([]any)
	if !ok {
		test.Fatalf("jsonArray: expected []any, got %T", parsed)
	}
	return arr
}

// assertStderrContains fails if substr is not found in r.Stderr.
func assertStderrContains(test *testing.T, result Result, substr string) {
	test.Helper()
	if !strings.Contains(result.Stderr, substr) {
		test.Fatalf("assertStderrContains: %q not found in stderr:\n%s", substr, result.Stderr)
	}
}
