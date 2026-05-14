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

func TestQueryCmd_SemanticErrorsWhenEmbeddingsAbsent(test *testing.T) {
	initWorkspace(test)

	createCmd := newRootCmd()
	createCmd.SetArgs([]string{"node", "create", "--type", "note", "--title", "X", "--path", "x.md"})

	if execErr := createCmd.Execute(); execErr != nil {
		test.Fatalf("create: %v", execErr)
	}

	out := &bytes.Buffer{}

	queryCmd := newRootCmd()
	queryCmd.SetOut(out)
	queryCmd.SetErr(out)
	queryCmd.SetArgs([]string{"query", "type=note", "--semantic", "auth bug"})

	queryErr := queryCmd.Execute()

	if queryErr == nil {
		test.Fatalf("expected error when embeddings provider not configured")
	}

	if !strings.Contains(queryErr.Error(), "embeddings") {
		test.Errorf("error should mention embeddings: %v", queryErr)
	}
}

func TestQueryCmd_SemanticJSONIncludesSnippet(test *testing.T) {
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

	if writeErr := os.WriteFile(filepath.Join(tmpDir, "notes/snippet.md"), []byte("---\ntype: note\ntitle: Snippet\n---\n\nJSON snippet body content.\n"), 0o644); writeErr != nil {
		test.Fatalf("write node: %v", writeErr)
	}

	reindexCmd := newRootCmd()
	reindexCmd.SetArgs([]string{"reindex"})

	if execErr := reindexCmd.Execute(); execErr != nil {
		test.Fatalf("reindex: %v", execErr)
	}

	out := &bytes.Buffer{}

	queryCmd := newRootCmd()
	queryCmd.SetOut(out)
	queryCmd.SetErr(out)
	queryCmd.SetArgs([]string{"query", "type=note", "--semantic", "anything", "--json"})

	if execErr := queryCmd.Execute(); execErr != nil {
		test.Fatalf("query: %v\nout:\n%s", execErr, out.String())
	}

	var results []map[string]any

	if jsonErr := json.Unmarshal(out.Bytes(), &results); jsonErr != nil {
		test.Fatalf("unmarshal: %v\nout:\n%s", jsonErr, out.String())
	}

	if len(results) == 0 {
		test.Fatalf("empty result list:\n%s", out.String())
	}

	snippet, ok := results[0]["snippet"].(string)

	if !ok || snippet == "" {
		test.Errorf("result[0] missing snippet: %v", results[0])
	}

	if !strings.Contains(snippet, "JSON snippet body") {
		test.Errorf("snippet content unexpected: %q", snippet)
	}
}

func TestQueryCmd_SemanticRendersSnippetColumn(test *testing.T) {
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

	if writeErr := os.WriteFile(filepath.Join(tmpDir, "notes/snippet.md"), []byte("---\ntype: note\ntitle: Snippet\n---\n\nThis is the body of the chunk we want surfaced as a snippet.\n"), 0o644); writeErr != nil {
		test.Fatalf("write node: %v", writeErr)
	}

	reindexCmd := newRootCmd()
	reindexCmd.SetArgs([]string{"reindex"})

	if execErr := reindexCmd.Execute(); execErr != nil {
		test.Fatalf("reindex: %v", execErr)
	}

	out := &bytes.Buffer{}

	queryCmd := newRootCmd()
	queryCmd.SetOut(out)
	queryCmd.SetErr(out)
	queryCmd.SetArgs([]string{"query", "type=note", "--semantic", "authentication"})

	if execErr := queryCmd.Execute(); execErr != nil {
		test.Fatalf("query: %v\nout:\n%s", execErr, out.String())
	}

	if !strings.Contains(out.String(), "SNIPPET") {
		test.Errorf("output missing SNIPPET header:\n%s", out.String())
	}

	if !strings.Contains(out.String(), "body of the chunk") {
		test.Errorf("output missing snippet content:\n%s", out.String())
	}
}
