package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// writeUnindexedCheckout writes a workspace the way a fresh clone looks on a
// CI runner: tusk.toml and markdown, no .tusk/ index. The note carries a
// dangling wikilink, so a built index has one error to find.
func writeUnindexedCheckout(test *testing.T, embeddings string) string {
	test.Helper()

	root := test.TempDir()

	writeWatchTestFile(test, root, "tusk.toml", `[workspace]
name = "ci"
`+embeddings+`
[node-types.note]
properties = []

[edge-types.references]
from        = ["*"]
to          = ["*"]
cardinality = "many-to-many"
wikilinks   = true
`)
	writeWatchTestFile(test, root, "notes/a.md", "---\ntype: note\n---\n# A\n\nSee [[notes/missing]].\n")

	return root
}

// Plain doctor on a never-indexed checkout can't vouch for the files, so it
// warns instead of reporting a clean vault; the warning alone exits 0 (#787).
func TestDoctor_WarnsOnNeverIndexedCheckout(test *testing.T) {
	root := writeUnindexedCheckout(test, "")

	stdout, stderr, ok := runCLISplit(root, "doctor")

	if !ok {
		test.Fatalf("doctor exited non-zero; a warning must not fail it. stderr:\n%s", stderr)
	}

	if !strings.Contains(stdout.String(), "warning  [index-not-built]") {
		test.Errorf("stdout = %q, want an index-not-built warning", stdout.String())
	}

	if strings.Contains(stdout.String(), "[dangling-edge]") {
		test.Errorf("stdout = %q, plain doctor must not index the checkout", stdout.String())
	}
}

// doctor --reindex builds the index before checking, so a fresh checkout's
// errors are found and fail the run.
func TestDoctor_ReindexFlagBuildsIndexFirst(test *testing.T) {
	root := writeUnindexedCheckout(test, "")

	stdout, stderr, ok := runCLISplit(root, "doctor", "--reindex")

	if ok {
		test.Fatalf("doctor --reindex exited 0; want the dangling link to fail it. stdout:\n%s", stdout)
	}

	if !strings.Contains(stdout.String(), "error    [dangling-edge] notes/a") {
		test.Errorf("stdout = %q, want the dangling-edge error", stdout.String())
	}

	if strings.Contains(stdout.String(), "[index-not-built]") {
		test.Errorf("stdout = %q, the index was built, so no index-not-built warning", stdout.String())
	}

	if !strings.Contains(stderr.String(), "Reindex done: 1 indexed") {
		test.Errorf("stderr = %q, want the reindex summary", stderr.String())
	}
}

// doctor --reindex is the structural pass only: it never calls the embedder,
// so a runner without Ollama checks the vault without a single embed attempt.
func TestDoctor_ReindexFlagNeverEmbeds(test *testing.T) {
	var calls atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusInternalServerError)
	}))

	defer server.Close()

	root := writeUnindexedCheckout(test, `
[embeddings]
provider = "ollama"
model = "test-model"
endpoint = "`+server.URL+`"
dim = 3
`)

	stdout, _, _ := runCLISplit(root, "doctor", "--reindex")

	if got := calls.Load(); got != 0 {
		test.Errorf("embedder received %d requests, want 0 from doctor --reindex", got)
	}

	if !strings.Contains(stdout.String(), "[dangling-edge] notes/a") {
		test.Errorf("stdout = %q, want the dangling-edge error", stdout.String())
	}
}
