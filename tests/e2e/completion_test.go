package e2e

import (
	"strings"
	"testing"
)

func TestCompletion(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "completion_smoke",
			Steps: []Step{
				{
					Args: []string{"completion", "bash"},
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						if r.Err != nil {
							t.Fatalf("completion bash: unexpected error: %v\nstderr: %s", r.Err, r.Stderr)
						}
						if len(r.Stdout) < 100 {
							t.Fatalf("completion bash: stdout too short (%d bytes):\n%s", len(r.Stdout), r.Stdout)
						}
					},
				},
				{
					Args: []string{"completion", "zsh"},
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						if r.Err != nil {
							t.Fatalf("completion zsh: unexpected error: %v\nstderr: %s", r.Err, r.Stderr)
						}
						if len(r.Stdout) < 100 {
							t.Fatalf("completion zsh: stdout too short (%d bytes):\n%s", len(r.Stdout), r.Stdout)
						}
					},
				},
				{
					Args: []string{"completion", "fish"},
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						if r.Err != nil {
							t.Fatalf("completion fish: unexpected error: %v\nstderr: %s", r.Err, r.Stderr)
						}
						if len(r.Stdout) < 100 {
							t.Fatalf("completion fish: stdout too short (%d bytes):\n%s", len(r.Stdout), r.Stdout)
						}
					},
				},
				{
					Args: []string{"completion", "powershell"},
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						if r.Err != nil {
							t.Fatalf("completion powershell: unexpected error: %v\nstderr: %s", r.Err, r.Stderr)
						}
						if len(r.Stdout) < 100 {
							t.Fatalf("completion powershell: stdout too short (%d bytes):\n%s", len(r.Stdout), r.Stdout)
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
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						if r.Err != nil {
							t.Fatalf("__complete: unexpected error: %v\nstderr: %s", r.Err, r.Stderr)
						}
						// NOTE: keep this list in sync with the AddCommand calls in
						// internal/tui/app.go (func New). A missing entry means a
						// root-level command silently lost its completion coverage.
						rootCmds := []string{
							"completion", "config", "mcp", "player",
							"project", "tag", "task", "version", "workflow",
							// TODO(v0.11): add "undo" here once the workspace-wide verbs initiative registers it on the root.
						}
						for _, name := range rootCmds {
							if !strings.Contains(r.Stdout, name) {
								t.Fatalf("__complete output missing root command %q\nstdout:\n%s", name, r.Stdout)
							}
						}
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
