package doctor_test

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/doctor"
)

// fakeCheckRunner returns a scripted result per command and records each call.
type fakeCheckRunner struct {
	mu      sync.Mutex
	results map[string]doctor.CheckResult
	errs    map[string]error
	calls   []fakeCheckCall
}

type fakeCheckCall struct {
	Command string
	Dir     string
	Timeout time.Duration
}

func (runner *fakeCheckRunner) Run(command, dir string, timeout time.Duration) (doctor.CheckResult, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()

	runner.calls = append(runner.calls, fakeCheckCall{Command: command, Dir: dir, Timeout: timeout})

	return runner.results[command], runner.errs[command]
}

const checksManifest = `[workspace]
name = "brain"

[check.pass]
command = "make ok"

[check.lines]
command  = "python3 lint.py"
severity = "warning"

[check.tail]
description = "schema export"
command     = "make export"

[check.slow]
command         = "sleep 99"
timeout-seconds = 5

[check.no-shell]
command = "make anything"

[check.no-command]
description = "forgot the command"

[check.bad-severity]
command  = "true"
severity = "fatal"

[check.negative-timeout]
command         = "true"
timeout-seconds = -1

[check.typo]
comand = "make lint"
`

func TestCompileChecks_RejectsInvalidChecks(test *testing.T) {
	loaded := loadGraphExpansionManifest(test, checksManifest)

	compiled, checkErrs := doctor.CompileChecks(loaded)

	var names []string

	for _, check := range compiled {
		names = append(names, check.Name)
	}

	if want := []string{"lines", "no-shell", "pass", "slow", "tail"}; !reflect.DeepEqual(names, want) {
		test.Fatalf("compiled = %v, want %v", names, want)
	}

	want := []string{
		`check "bad-severity": severity "fatal" is not one of error, warning, advice`,
		`check "negative-timeout": timeout-seconds -1 is negative; leave it out for the 60s default`,
		`check "no-command": command is empty; set the shell command to run`,
		`check "typo": unknown key "comand" (a check takes description, command, severity, timeout-seconds)`,
		`check "typo": command is empty; set the shell command to run`,
	}

	var got []string

	for _, checkErr := range checkErrs {
		got = append(got, checkErr.Error())
	}

	if !reflect.DeepEqual(got, want) {
		test.Fatalf("check errors =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Under --checks doctor runs each valid check from the workspace root and
// turns its result into findings (#852): exit 0 passes whatever it prints;
// otherwise each `path:line: message` line is a finding, and a check that
// prints none is one finding with the end of its output.
func TestRun_RunsProjectChecks(test *testing.T) {
	loaded := loadGraphExpansionManifest(test, checksManifest)

	runner := &fakeCheckRunner{
		results: map[string]doctor.CheckResult{
			"make ok":         {ExitCode: 0, Output: []byte("docs/a.md:1: looks like a finding, but the check passed\n")},
			"python3 lint.py": {ExitCode: 1, Output: []byte("checking 3 files\ndocs/a.md:12: missing owner\n2026-11-03 12:30:45: not a finding\ndocs/b.md:3:7: bad link\n")},
			"make export":     {ExitCode: 2, Output: []byte("starting\n\nstep one\nstep two\nexport failed\n")},
			"sleep 99":        {ExitCode: -1, TimedOut: true},
		},
		errs: map[string]error{"make anything": errors.New("exec: \"sh\": executable file not found in $PATH")},
	}

	report, runErr := doctor.Run(doctor.Config{Manifest: loaded, Root: "/ws", Checks: runner})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	var got []string

	for _, issue := range report.Issues {
		if strings.HasPrefix(issue.Kind, doctor.CheckKindPrefix) {
			got = append(got, issue.Severity+" "+issue.Kind+" "+issue.NodeID+": "+issue.Message)
		}
	}

	want := []string{
		`error check:no-shell : could not run: exec: "sh": executable file not found in $PATH`,
		`error check:slow : timed out after 5s`,
		`error check:tail : schema export (exited 2): step one; step two; export failed`,
		`warning check:lines docs/a.md:12: missing owner`,
		`warning check:lines docs/b.md:3: bad link`,
	}

	if !reflect.DeepEqual(got, want) {
		test.Fatalf("check issues =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	wantCalls := []fakeCheckCall{
		{Command: "python3 lint.py", Dir: "/ws", Timeout: doctor.DefaultCheckTimeout},
		{Command: "make anything", Dir: "/ws", Timeout: doctor.DefaultCheckTimeout},
		{Command: "make ok", Dir: "/ws", Timeout: doctor.DefaultCheckTimeout},
		{Command: "sleep 99", Dir: "/ws", Timeout: 5 * time.Second},
		{Command: "make export", Dir: "/ws", Timeout: doctor.DefaultCheckTimeout},
	}

	if !reflect.DeepEqual(runner.calls, wantCalls) {
		test.Fatalf("calls =\n%+v\nwant\n%+v", runner.calls, wantCalls)
	}

	if report.ChecksSkipped != 0 {
		test.Errorf("ChecksSkipped = %d, want 0 when a runner is set", report.ChecksSkipped)
	}
}

// Without a runner (MCP, or the CLI without --checks) nothing runs: the report
// counts the valid checks it skipped, and invalid ones are still reported.
func TestRun_SkipsProjectChecksWithoutARunner(test *testing.T) {
	loaded := loadGraphExpansionManifest(test, checksManifest)

	report, runErr := doctor.Run(doctor.Config{Manifest: loaded, Root: "/ws"})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.ChecksSkipped != 5 {
		test.Errorf("ChecksSkipped = %d, want 5", report.ChecksSkipped)
	}

	invalid := 0

	for _, issue := range report.Issues {
		if strings.HasPrefix(issue.Kind, doctor.CheckKindPrefix) {
			test.Errorf("unexpected check finding without a runner: %+v", issue)
		}

		if issue.Kind == doctor.IssueCheckInvalid {
			invalid++
		}
	}

	if invalid != 5 {
		test.Errorf("check-invalid issues = %d, want 5", invalid)
	}
}
