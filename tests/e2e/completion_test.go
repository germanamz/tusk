package e2e

import (
	"strings"
	"testing"
)

func TestCompletion(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "completion_smoke",
			Steps: []Step{
				{
					Args: []string{"completion", "bash"},
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						if result.Err != nil {
							test.Fatalf("completion bash: unexpected error: %v\nstderr: %s", result.Err, result.Stderr)
						}
						if len(result.Stdout) < 100 {
							test.Fatalf("completion bash: stdout too short (%d bytes):\n%s", len(result.Stdout), result.Stdout)
						}
					},
				},
				{
					Args: []string{"completion", "zsh"},
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						if result.Err != nil {
							test.Fatalf("completion zsh: unexpected error: %v\nstderr: %s", result.Err, result.Stderr)
						}
						if len(result.Stdout) < 100 {
							test.Fatalf("completion zsh: stdout too short (%d bytes):\n%s", len(result.Stdout), result.Stdout)
						}
					},
				},
				{
					Args: []string{"completion", "fish"},
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						if result.Err != nil {
							test.Fatalf("completion fish: unexpected error: %v\nstderr: %s", result.Err, result.Stderr)
						}
						if len(result.Stdout) < 100 {
							test.Fatalf("completion fish: stdout too short (%d bytes):\n%s", len(result.Stdout), result.Stdout)
						}
					},
				},
				{
					Args: []string{"completion", "powershell"},
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						if result.Err != nil {
							test.Fatalf("completion powershell: unexpected error: %v\nstderr: %s", result.Err, result.Stderr)
						}
						if len(result.Stdout) < 100 {
							test.Fatalf("completion powershell: stdout too short (%d bytes):\n%s", len(result.Stdout), result.Stdout)
						}
					},
				},
			},
		},
		{
			// Regression coverage for "every root command still reachable via
			// completion". Cobra's generated scripts are fully dynamic — they
			// all delegate to `tusk __complete` at runtime — so the only
			// reliable way to assert command coverage is to invoke the hidden
			// RPC directly. The bypass path in cmd/tusk/main.go covers
			// __complete as well as `completion`, so this also exercises that
			// the RPC runs without opening the database.
			Name: "completion_lists_root_commands",
			Steps: []Step{
				{
					Args: []string{"__complete", ""},
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						if result.Err != nil {
							test.Fatalf("__complete: unexpected error: %v\nstderr: %s", result.Err, result.Stderr)
						}
						// NOTE: keep this list in sync with the AddCommand calls in
						// internal/tui/app.go (func New). A missing entry means a
						// root-level command silently lost its completion coverage.
						rootCmds := []string{
							"completion", "config", "mcp", "player",
							"project", "tag", "task", "version", "workflow",
							// TODO(v0.14): add "undo" here once the undo command is implemented.
						}
						for _, name := range rootCmds {
							if !strings.Contains(result.Stdout, name) {
								test.Fatalf("__complete output missing root command %q\nstdout:\n%s", name, result.Stdout)
							}
						}
					},
				},
			},
		},
	}
	runScenarios(test, binPath, scenarios)
}
