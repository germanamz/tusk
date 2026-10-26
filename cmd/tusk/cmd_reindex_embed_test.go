package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// writeEmbedWorkspace writes a workspace whose [embeddings] endpoint is
// endpoint, with one note.
func writeEmbedWorkspace(test *testing.T, endpoint string) string {
	test.Helper()

	root := test.TempDir()

	writeWatchTestFile(test, root, "tusk.toml", `[workspace]
name = "t"

[embeddings]
provider = "ollama"
model = "test-model"
endpoint = "`+endpoint+`"
dim = 3

[node-types.note]
properties = []
`)
	writeWatchTestFile(test, root, "notes/base.md", "---\ntype: note\n---\n# Base\n\nbody\n")

	return root
}

// unreachableEndpoint returns the URL of a server that is already closed, so
// every connection is refused: a transport error, fast and deterministic.
func unreachableEndpoint() string {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := server.URL
	server.Close()

	return endpoint
}

// An unreachable embedder no longer fails the reindex: the index is built, the
// summary says embedding is still queued, and the exit is 0 (#787).
func TestReindexCmd_UnreachableEmbedderExitsZero(test *testing.T) {
	root := writeEmbedWorkspace(test, unreachableEndpoint())

	stdout, stderr, ok := runCLISplit(root, "reindex")

	if !ok {
		test.Fatalf("reindex exited non-zero with an unreachable embedder; stderr:\n%s", stderr)
	}

	if !strings.Contains(stdout.String(), "Reindex done: 1 indexed") {
		test.Errorf("stdout = %q, want the reindex summary", stdout.String())
	}

	if !strings.Contains(stdout.String(), "queued for embedding (embedding backend unavailable)") {
		test.Errorf("stdout = %q, want the deferred-embedding clause", stdout.String())
	}

	if !strings.Contains(stderr.String(), "connection refused") {
		test.Errorf("stderr = %q, want the transport error named", stderr.String())
	}
}

// --strict-embed keeps the old contract: the index is built and summarized,
// then the run fails on the unreachable embedder.
func TestReindexCmd_StrictEmbedFailsOnUnreachableEmbedder(test *testing.T) {
	root := writeEmbedWorkspace(test, unreachableEndpoint())

	stdout, stderr, ok := runCLISplit(root, "reindex", "--strict-embed")

	if ok {
		test.Fatalf("reindex --strict-embed exited 0 with an unreachable embedder")
	}

	if !strings.Contains(stdout.String(), "Reindex done: 1 indexed") {
		test.Errorf("stdout = %q, want the summary printed before the failure", stdout.String())
	}

	if !strings.Contains(stderr.String(), "connection refused") {
		test.Errorf("stderr = %q, want the transport error", stderr.String())
	}
}

// --no-embed never calls the embedder.
func TestReindexCmd_NoEmbedSkipsEmbedder(test *testing.T) {
	var calls atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusInternalServerError)
	}))

	defer server.Close()

	root := writeEmbedWorkspace(test, server.URL)

	stdout, stderr, ok := runCLISplit(root, "reindex", "--no-embed")

	if !ok {
		test.Fatalf("reindex --no-embed failed; stderr:\n%s", stderr)
	}

	if got := calls.Load(); got != 0 {
		test.Errorf("embedder received %d requests, want 0 with --no-embed", got)
	}

	if !strings.Contains(stdout.String(), "Reindex done: 1 indexed") {
		test.Errorf("stdout = %q, want the reindex summary", stdout.String())
	}

	if strings.Contains(stdout.String(), "embedding backend unavailable") {
		test.Errorf("stdout = %q, --no-embed must not report the embedder as unavailable", stdout.String())
	}
}

// refusingEndpoint serves /api/embed like a model that refuses every input
// containing marker, truncated or not, and embeds everything else (the
// drain's probe included).
func refusingEndpoint(test *testing.T, marker string) string {
	test.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Input string `json:"input"`
		}

		_ = json.NewDecoder(request.Body).Decode(&payload)

		if strings.Contains(payload.Input, marker) {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":"invalid input"}`))

			return
		}

		_ = json.NewEncoder(writer).Encode(map[string]any{"embeddings": [][]float64{{1, 0, 0}}})
	}))

	test.Cleanup(server.Close)

	return server.URL
}

// A node the model refuses no longer stalls the drain or reads as an
// unreachable backend: reindex exits 0 and its summary counts the failure;
// --strict-embed still fails on it (#845).
func TestReindexCmd_ReportsFailedEmbeds(test *testing.T) {
	root := writeEmbedWorkspace(test, refusingEndpoint(test, "REFUSE"))
	writeWatchTestFile(test, root, "notes/base.md", "---\ntype: note\n---\n# Base\n\nREFUSE\n")

	stdout, stderr, ok := runCLISplit(root, "reindex")

	if !ok {
		test.Fatalf("reindex exited non-zero on a refused input; stderr:\n%s", stderr)
	}

	if !strings.Contains(stdout.String(), "failed to embed (`tusk doctor` lists them)") || strings.Contains(stdout.String(), "unavailable") {
		test.Errorf("stdout = %q, want the failed-embed clause and no outage", stdout.String())
	}

	_, strictStderr, strictOK := runCLISplit(root, "reindex", "--strict-embed")

	if strictOK {
		test.Fatal("reindex --strict-embed exited 0 with failed embeds")
	}

	if !strings.Contains(strictStderr.String(), "failed to embed (--strict-embed)") {
		test.Errorf("stderr = %q, want the strict failure", strictStderr.String())
	}
}

func TestReindexCmd_NoEmbedAndStrictEmbedConflict(test *testing.T) {
	root := writeEmbedWorkspace(test, unreachableEndpoint())

	_, stderr, ok := runCLISplit(root, "reindex", "--no-embed", "--strict-embed")

	if ok {
		test.Fatalf("reindex --no-embed --strict-embed exited 0, want a usage error")
	}

	if !strings.Contains(stderr.String(), "none of the others can be") {
		test.Errorf("stderr = %q, want cobra's mutually-exclusive flags error", stderr.String())
	}
}
