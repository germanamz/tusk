package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

// TestGoldenCLI_ReindexEmbeddingsSettingsChange pins the `tusk reindex`
// summary after an [embeddings] document setting changes: no note changed,
// yet every embeddable node is re-queued and the summary says so, so a reindex
// that suddenly takes as long as a full re-embed isn't a mystery.
func TestGoldenCLI_ReindexEmbeddingsSettingsChange(test *testing.T) {
	ollama := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{"embedding": []float64{0.1, 0.2, 0.3}})
	}))

	test.Cleanup(ollama.Close)

	manifestWith := func(extra string) string {
		return "[workspace]\nname = \"test\"\nsub-units = false\n\n[embeddings]\nprovider = \"ollama\"\nmodel = \"fake\"\nendpoint = \"" +
			ollama.URL + "\"\ndim = 3\n" + extra + "\n[node-types.note]\nproperties = []\n"
	}

	runGoldenCLICases(test, []goldenCLICase{
		{
			name: "document-prefix change re-queues every node",
			setup: func(test *testing.T, root string) {
				writeFile(test, root, "tusk.toml", manifestWith(""))
				writeFile(test, root, "notes/a.md", "---\ntype: note\ntitle: A\n---\n\nAlpha.\n")
				writeFile(test, root, "notes/b.md", "---\ntype: note\ntitle: B\n---\n\nBeta.\n")

				if _, stderr, ok := runCLISplit(root, "reindex"); !ok {
					test.Fatalf("setup reindex: %s", stderr.String())
				}

				writeFile(test, root, "tusk.toml", manifestWith("document-prefix = \"search_document: \"\n"))
			},
			args:       []string{"reindex"},
			wantStdout: "Reindex done: 0 indexed, 0 removed, 0 skipped, 2 nodes re-queued for new [embeddings] settings\n",
		},
	})
}

// TestReload_AppliesEmbeddingsSettingsWithoutReindex: plain `tusk reload`
// (no --reindex) leaves the reindex to a running daemon, and a converging
// sibling daemon never reindexes. So reload itself records the new
// [embeddings] settings and queues the re-embed; otherwise the daemon's
// drainer, now on the new settings, would wait on a fingerprint nobody writes.
func TestReload_AppliesEmbeddingsSettingsWithoutReindex(test *testing.T) {
	ollama := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{"embedding": []float64{0.1, 0.2, 0.3}})
	}))

	test.Cleanup(ollama.Close)

	manifestWith := func(extra string) string {
		return "[workspace]\nname = \"test\"\nsub-units = false\n\n[embeddings]\nprovider = \"ollama\"\nmodel = \"fake\"\nendpoint = \"" +
			ollama.URL + "\"\ndim = 3\n" + extra + "\n[node-types.note]\nproperties = []\n"
	}

	root := goldenWorkspace(test, manifestWith(""))

	writeFile(test, root, "notes/a.md", "---\ntype: note\ntitle: A\n---\n\nAlpha.\n")
	writeFile(test, root, "notes/b.md", "---\ntype: note\ntitle: B\n---\n\nBeta.\n")

	if _, stderr, ok := runCLISplit(root, "reindex"); !ok {
		test.Fatalf("reindex: %s", stderr.String())
	}

	writeFile(test, root, "tusk.toml", manifestWith("document-prefix = \"search_document: \"\n"))

	stdout, stderr, ok := runCLISplit(root, "reload")

	if !ok {
		test.Fatalf("reload: %s", stderr.String())
	}

	var response map[string]any

	if decodeErr := json.Unmarshal(stdout.Bytes(), &response); decodeErr != nil {
		test.Fatalf("decode reload output: %v\n%s", decodeErr, stdout.String())
	}

	if requeued, _ := response["embed_settings_requeued"].(float64); requeued != 2 {
		test.Errorf("embed_settings_requeued = %v, want 2", response["embed_settings_requeued"])
	}

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer func() { _ = store.Close() }()

	if depth, _ := index.NewEmbedQueueRepo(store).DepthByKind("embed"); depth != 2 {
		test.Errorf("embed queue depth = %d, want 2 (queued for the daemon's drainer)", depth)
	}
}
