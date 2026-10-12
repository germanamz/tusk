package main

import (
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

	if !strings.Contains(stdout.String(), "queued for embedding (embedder unreachable)") {
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

	if strings.Contains(stdout.String(), "embedder unreachable") {
		test.Errorf("stdout = %q, --no-embed must not report the embedder as unreachable", stdout.String())
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
