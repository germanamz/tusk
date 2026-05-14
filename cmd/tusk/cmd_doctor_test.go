package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctor_RendersPropertyTypeMismatch(test *testing.T) {
	root := newWorkspaceWithNodeTypes(test)

	if mkErr := os.MkdirAll(filepath.Join(root, "tickets"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "tickets/bar.md"), []byte(`---
type: ticket
summary: hi
priority: high
---
`), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	if _, _, reindexOk := runCLISplit(root, "reindex"); !reindexOk {
		test.Fatalf("reindex failed")
	}

	stdout, _, ok := runCLISplit(root, "doctor")

	if !ok {
		test.Errorf("exit non-zero, want 0")
	}

	if !strings.Contains(stdout.String(), "type-mismatch") {
		test.Errorf("stdout = %q, want mention of type-mismatch", stdout.String())
	}

	if !strings.Contains(stdout.String(), "tickets/bar") {
		test.Errorf("stdout = %q, want mention of tickets/bar", stdout.String())
	}
}

func TestDoctor_PrintsCleanReport(test *testing.T) {
	root := setupTempWorkspace(test)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("doctor")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	if !strings.Contains(out, "no issues") {
		test.Errorf("expected 'no issues', got:\n%s", out)
	}
}

func TestDoctor_RendersWorkflowViolation(test *testing.T) {
	root := newWorkspaceWithWorkflow(test)

	// Use mustCreateNode to create a node with off-schema status.
	mustCreateNode(test, root, "tickets/foo", "ticket", map[string]string{"status": "blocked"})

	stdout, _, ok := runCLISplit(root, "doctor")

	if !ok {
		test.Errorf("exit non-zero, want 0")
	}

	if !strings.Contains(stdout.String(), "workflow-violation") {
		test.Errorf("stdout = %q, want mention of workflow-violation", stdout.String())
	}

	if !strings.Contains(stdout.String(), "tickets/foo") {
		test.Errorf("stdout = %q, want mention of tickets/foo", stdout.String())
	}
}

func TestDoctor_PrintsEmbedStatsBlock(test *testing.T) {
	tmpDir := initWorkspace(test)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{"embedding": []float64{1, 0, 0}})
	}))

	defer server.Close()

	manifestBody := `[workspace]
name = "test"

[embeddings]
provider = "ollama"
model = "test-model"
endpoint = "` + server.URL + `"
dim = 3
`

	if writeErr := os.WriteFile(filepath.Join(tmpDir, "tusk.toml"), []byte(manifestBody), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	if mkErr := os.MkdirAll(filepath.Join(tmpDir, "notes"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(tmpDir, "notes/a.md"), []byte("---\ntype: note\ntitle: A\n---\n\nBody for A.\n"), 0o644); writeErr != nil {
		test.Fatalf("write A: %v", writeErr)
	}

	reindexCmd := newRootCmd()
	reindexCmd.SetArgs([]string{"reindex"})

	if execErr := reindexCmd.Execute(); execErr != nil {
		test.Fatalf("reindex: %v", execErr)
	}

	out := &bytes.Buffer{}

	doctorCmd := newRootCmd()
	doctorCmd.SetOut(out)
	doctorCmd.SetErr(out)
	doctorCmd.SetArgs([]string{"doctor"})

	if execErr := doctorCmd.Execute(); execErr != nil {
		test.Fatalf("doctor: %v\nout:\n%s", execErr, out.String())
	}

	if !strings.Contains(out.String(), "embed stats:") {
		test.Errorf("output missing 'embed stats:' line:\n%s", out.String())
	}

	if !strings.Contains(out.String(), "top by chunks:") {
		test.Errorf("output missing 'top by chunks:' block:\n%s", out.String())
	}
}

func TestDoctor_OmitsEmbedStatsWithoutConfig(test *testing.T) {
	// initWorkspace creates a workspace without an [embeddings] section, so
	// loaded.Embeddings.Provider == "" and the stats branch must be skipped.
	_ = initWorkspace(test)

	out := &bytes.Buffer{}

	doctorCmd := newRootCmd()
	doctorCmd.SetOut(out)
	doctorCmd.SetErr(out)
	doctorCmd.SetArgs([]string{"doctor"})

	if execErr := doctorCmd.Execute(); execErr != nil {
		test.Fatalf("doctor: %v", execErr)
	}

	if strings.Contains(out.String(), "embed stats:") {
		test.Errorf("output includes 'embed stats:' when no embeddings configured:\n%s", out.String())
	}
}
