package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/germanamz/tusk/internal/embed"
)

// fakeOllama is an /api/embeddings stand-in that records every prompt, so a
// test can drive the real Ollama embedder NewFromManifest builds on reload.
type fakeOllama struct {
	server *httptest.Server

	mu      sync.Mutex
	prompts []string
}

func newFakeOllama(test *testing.T) *fakeOllama {
	test.Helper()

	fake := &fakeOllama{}
	fake.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Prompt string `json:"prompt"`
		}

		_ = json.NewDecoder(request.Body).Decode(&body)

		fake.mu.Lock()
		fake.prompts = append(fake.prompts, body.Prompt)
		fake.mu.Unlock()

		_ = json.NewEncoder(writer).Encode(map[string]any{"embedding": []float64{0.1, 0.2, 0.3}})
	}))

	test.Cleanup(fake.server.Close)

	return fake
}

func (fake *fakeOllama) takePrompts() []string {
	fake.mu.Lock()
	defer fake.mu.Unlock()

	taken := fake.prompts
	fake.prompts = nil

	return taken
}

func callReload(test *testing.T, srv *Server) {
	test.Helper()

	result, callErr := reloadToolHandler(context.Background(), mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{Name: "tusk_reload", Arguments: map[string]any{}},
	}, srv)

	if callErr != nil {
		test.Fatalf("reload tool: %v", callErr)
	}

	if result.IsError {
		test.Fatalf("reload tool error: %s", textOf(result))
	}
}

// drainOnce runs one background-drainer tick against the server's current
// runtime, as RunDrainer would.
func drainOnce(test *testing.T, srv *Server) {
	test.Helper()

	if _, drainErr := drainTick(context.Background(), srv.snapshotRuntime(), nil); drainErr != nil {
		test.Fatalf("drain tick: %v", drainErr)
	}
}

// TestReloadTool_EmbeddingsSettingsChangeReembeds: editing a document-side
// [embeddings] key and calling tusk_reload re-embeds every node with the new
// settings, although no note changed. This is the path agents use after
// editing tusk.toml. Its reindex walk is async and doesn't drain, so the
// re-embed has to come from the settings check queueing work that the
// background drainer then does under the reloaded settings.
func TestReloadTool_EmbeddingsSettingsChangeReembeds(test *testing.T) {
	ollama := newFakeOllama(test)
	root := setupServerWorkspace(test)

	manifestWith := func(extra string) string {
		return "[workspace]\nname = \"test\"\n\n[embeddings]\nprovider = \"ollama\"\nmodel = \"fake\"\nendpoint = \"" +
			ollama.server.URL + "\"\ndim = 3\n" + extra
	}

	rewriteManifest(test, root, manifestWith(""))

	srv := newServerForRoot(test, root)

	callReload(test, srv)
	drainOnce(test, srv)

	if initial := ollama.takePrompts(); len(initial) == 0 {
		test.Fatalf("setup: the first reload embedded nothing")
	}

	rewriteManifest(test, root, manifestWith("document-prefix = \"search_document: \"\n"))
	callReload(test, srv)

	if early := ollama.takePrompts(); len(early) != 0 {
		test.Errorf("tusk_reload embedded %d texts inline; the background drainer owns that work", len(early))
	}

	drainOnce(test, srv)

	prompts := ollama.takePrompts()

	if len(prompts) == 0 {
		test.Fatalf("tusk_reload after a document-prefix change embedded nothing")
	}

	sawFile := false

	for _, prompt := range prompts {
		if !strings.HasPrefix(prompt, "search_document: ") {
			test.Errorf("prompt without the new document prefix: %q", prompt)
		}

		if prompt == "search_document: [type] note\n[title] Hi\n---\nhello\n" {
			sawFile = true
		}
	}

	if !sawFile {
		test.Errorf("the unchanged file notes/hi was not re-embedded; prompts = %q", prompts)
	}
}

// TestRunDrainer_WarnsOncePerSupersededEpisode: a daemon whose [embeddings]
// settings are stale holds its drainer back on every tick until it reloads.
// It says so once, not every two seconds.
func TestRunDrainer_WarnsOncePerSupersededEpisode(test *testing.T) {
	ollama := newFakeOllama(test)
	root := setupServerWorkspace(test)

	rewriteManifest(test, root, "[workspace]\nname = \"test\"\n\n[embeddings]\nprovider = \"ollama\"\nmodel = \"fake\"\nendpoint = \""+
		ollama.server.URL+"\"\ndim = 3\n")

	srv := newServerForRoot(test, root)
	rt := srv.snapshotRuntime()

	if setErr := rt.Meta.Set(embed.DocumentFingerprintKey, "settings-recorded-by-another-process"); setErr != nil {
		test.Fatalf("stamp: %v", setErr)
	}

	if depth, _ := rt.EmbedQueue.DepthByKind("embed"); depth == 0 {
		test.Fatalf("setup: nothing queued for the drainer")
	}

	var logs safeBuffer

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	if runErr := RunDrainer(ctx, DrainerConfig{
		Server:   srv,
		Interval: 10 * time.Millisecond,
		Logger:   slog.New(slog.NewTextHandler(&logs, nil)),
	}); runErr != nil {
		test.Fatalf("RunDrainer: %v", runErr)
	}

	if warnings := strings.Count(logs.String(), "settings changed since this process loaded tusk.toml"); warnings != 1 {
		test.Errorf("logged the superseded-settings warning %d times over many ticks, want 1:\n%s", warnings, logs.String())
	}

	if prompts := ollama.takePrompts(); len(prompts) != 0 {
		test.Errorf("stale drainer embedded %d texts, want 0", len(prompts))
	}
}

// safeBuffer is a goroutine-safe log sink.
type safeBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (sink *safeBuffer) Write(data []byte) (int, error) {
	sink.mu.Lock()
	defer sink.mu.Unlock()

	return sink.buffer.Write(data)
}

func (sink *safeBuffer) String() string {
	sink.mu.Lock()
	defer sink.mu.Unlock()

	return sink.buffer.String()
}
