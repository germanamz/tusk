package embed_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/embed"
)

func TestOllamaEmbedder_PostsToEmbedEndpoint(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/embed" {
			test.Errorf("path = %q, want /api/embed", request.URL.Path)
		}

		var payload struct {
			Model    string `json:"model"`
			Input    string `json:"input"`
			Truncate *bool  `json:"truncate"`
		}

		if decodeErr := json.NewDecoder(request.Body).Decode(&payload); decodeErr != nil {
			test.Fatalf("decode: %v", decodeErr)
		}

		if payload.Model != "test-model" {
			test.Errorf("model = %q, want test-model", payload.Model)
		}

		if payload.Input != "hello world" {
			test.Errorf("input = %q", payload.Input)
		}

		if payload.Truncate == nil || *payload.Truncate {
			test.Errorf("truncate = %v, want false (an over-long input must be refused, not silently clipped)", payload.Truncate)
		}

		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"embeddings": [][]float64{{0.1, 0.2, 0.3}},
		})
	}))

	defer server.Close()

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: server.URL,
		Model:    "test-model",
		Dim:      3,
	})

	vector, embedErr := embedder.Embed(context.Background(), []byte("hello world"))

	if embedErr != nil {
		test.Fatalf("Embed: %v", embedErr)
	}

	if len(vector) != 3 {
		test.Fatalf("len = %d", len(vector))
	}

	if vector[0] != 0.1 || vector[1] != 0.2 || vector[2] != 0.3 {
		test.Errorf("vector = %v, want [0.1 0.2 0.3]", vector)
	}
}

func TestOllamaEmbedder_ErrorsOnNon200(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "boom", http.StatusInternalServerError)
	}))

	defer server.Close()

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: server.URL,
		Model:    "x",
		Dim:      3,
	})

	_, embedErr := embedder.Embed(context.Background(), []byte("hello"))

	if embedErr == nil {
		test.Fatalf("expected error on 500")
	}
}

func TestOllamaEmbedder_ErrorsOnDimMismatch(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"embeddings": [][]float64{{0.1, 0.2}}, // 2 dims, but config says 3
		})
	}))

	defer server.Close()

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: server.URL,
		Model:    "x",
		Dim:      3,
	})

	_, embedErr := embedder.Embed(context.Background(), []byte("x"))

	if embedErr == nil {
		test.Fatalf("expected dim-mismatch error")
	}
}

func TestOllamaEmbedder_ModelAndDim(test *testing.T) {
	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: "http://example",
		Model:    "nomic-embed-text",
		Dim:      768,
	})

	if embedder.Model() != "nomic-embed-text" {
		test.Errorf("Model = %q", embedder.Model())
	}

	if embedder.Dim() != 768 {
		test.Errorf("Dim = %d", embedder.Dim())
	}
}

func TestOllamaEmbedder_LogsWarnOnNon2xx(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(`{"error":"input length exceeds the context length"}`))
	}))

	defer server.Close()

	var buffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelDebug}))

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: server.URL,
		Model:    "nomic-embed-text",
		Dim:      768,
		Logger:   logger,
	})

	_, embedErr := embedder.Embed(context.Background(), []byte("hello"))

	if embedErr == nil {
		test.Fatal("expected error from non-2xx response")
	}

	output := buffer.String()

	if !strings.Contains(output, "level=WARN") {
		test.Errorf("expected WARN log; got %q", output)
	}

	for _, want := range []string{`msg="ollama non-2xx"`, "status=500", "model=nomic-embed-text", "input length exceeds the context length"} {
		if !strings.Contains(output, want) {
			test.Errorf("expected log to contain %q; got %q", want, output)
		}
	}
}

func TestOllamaEmbedder_LogsDebugOnRequest(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"embeddings":[[0.1,0.2,0.3]]}`))
	}))

	defer server.Close()

	var buffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelDebug}))

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: server.URL,
		Model:    "nomic-embed-text",
		Dim:      3,
		Logger:   logger,
	})

	_, embedErr := embedder.Embed(context.Background(), []byte("hello"))

	if embedErr != nil {
		test.Fatalf("embed: %v", embedErr)
	}

	output := buffer.String()

	if !strings.Contains(output, "level=DEBUG") {
		test.Errorf("expected DEBUG log; got %q", output)
	}

	for _, want := range []string{`msg="ollama request"`, "bytes_sent=", `msg="ollama success"`} {
		if !strings.Contains(output, want) {
			test.Errorf("expected log to contain %q; got %q", want, output)
		}
	}
}

func TestOllamaEmbedder_RespectsConfiguredTimeout(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// Sleep longer than the configured timeout to force a client-side abort.
		time.Sleep(200 * time.Millisecond)
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"embeddings":[[0.1,0.2,0.3]]}`))
	}))

	defer server.Close()

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: server.URL,
		Model:    "stub",
		Dim:      3,
		Timeout:  50 * time.Millisecond,
	})

	_, err := embedder.Embed(context.Background(), []byte("hello"))

	if err == nil {
		test.Fatalf("Embed: expected timeout error, got nil")
	}

	if !strings.Contains(err.Error(), "context deadline exceeded") &&
		!strings.Contains(err.Error(), "Client.Timeout") {
		test.Errorf("Embed error = %q, want a timeout-related error", err.Error())
	}
}

// TestOllamaEmbedder_HTTP5xxIsTransportError pins the A2 classification: a 5xx
// means Ollama is unhealthy (overloaded, restarting), not that this node's
// content is bad — so the drain must back off, not drop the node. The error is
// marked transport so DrainQueue aborts the pass rather than burning attempts.
func TestOllamaEmbedder_HTTP5xxIsTransportError(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "overloaded", http.StatusServiceUnavailable)
	}))

	defer server.Close()

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: server.URL,
		Model:    "x",
		Dim:      3,
	})

	_, embedErr := embedder.Embed(context.Background(), []byte("hello"))

	if embedErr == nil {
		test.Fatal("expected error on 503")
	}

	if !embed.IsTransportError(embedErr) {
		test.Errorf("503 must be a TransportError; got %v", embedErr)
	}
}

// TestOllamaEmbedder_HTTP4xxIsNotTransportError confirms a 4xx (a bad request,
// a missing model) is not transient: it stays a plain error, and the drain's
// probe decides whether the input or the backend is at fault.
func TestOllamaEmbedder_HTTP4xxIsNotTransportError(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "bad request", http.StatusBadRequest)
	}))

	defer server.Close()

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: server.URL,
		Model:    "x",
		Dim:      3,
	})

	_, embedErr := embedder.Embed(context.Background(), []byte("hello"))

	if embedErr == nil {
		test.Fatal("expected error on 400")
	}

	if embed.IsTransportError(embedErr) {
		test.Errorf("400 must NOT be a TransportError; got %v", embedErr)
	}
}

// TestOllamaEmbedder_ConnectionRefusedIsTransportError closes the test server
// before the call so client.Do fails to connect — the canonical "Ollama is
// down" case. It must classify as transport so a brief outage doesn't evict
// every queued node from semantic results.
func TestOllamaEmbedder_ConnectionRefusedIsTransportError(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {}))
	endpoint := server.URL
	server.Close() // nothing is listening now

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: endpoint,
		Model:    "x",
		Dim:      3,
	})

	_, embedErr := embedder.Embed(context.Background(), []byte("hello"))

	if embedErr == nil {
		test.Fatal("expected a connection error against a closed server")
	}

	if !embed.IsTransportError(embedErr) {
		test.Errorf("connection refused must be a TransportError; got %v", embedErr)
	}
}

// TestOllamaEmbedder_DimMismatchIsNotTransportError confirms a dimension
// mismatch (the wrong model wired up) is a hard per-node fault, not transient —
// retrying the same payload will never succeed, so it stays a plain error.
func TestOllamaEmbedder_DimMismatchIsNotTransportError(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"embeddings": [][]float64{{0.1, 0.2}}, // 2 dims, config says 3
		})
	}))

	defer server.Close()

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: server.URL,
		Model:    "x",
		Dim:      3,
	})

	_, embedErr := embedder.Embed(context.Background(), []byte("x"))

	if embedErr == nil {
		test.Fatal("expected dim-mismatch error")
	}

	if embed.IsTransportError(embedErr) {
		test.Errorf("dim mismatch must NOT be a TransportError; got %v", embedErr)
	}
}

// TestOllamaEmbedder_RequestBody pins the wire bytes: with num-ctx unset the
// body carries no "options" key, so Ollama runs the model with its own
// defaults, and truncate is always sent as false.
func TestOllamaEmbedder_RequestBody(test *testing.T) {
	cases := []struct {
		name   string
		numCtx int
		want   string
	}{
		{name: "num-ctx unset", numCtx: 0, want: `{"model":"m","input":"hello","truncate":false}`},
		{name: "num-ctx set", numCtx: 512, want: `{"model":"m","input":"hello","truncate":false,"options":{"num_ctx":512}}`},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			var received []byte

			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				buffer := new(bytes.Buffer)
				_, _ = buffer.ReadFrom(request.Body)
				received = buffer.Bytes()

				_ = json.NewEncoder(writer).Encode(map[string]any{"embeddings": [][]float64{{0.1}}})
			}))

			defer server.Close()

			embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{Endpoint: server.URL, Model: "m", Dim: 1, NumCtx: testCase.numCtx})

			if _, embedErr := embedder.Embed(context.Background(), []byte("hello")); embedErr != nil {
				test.Fatalf("Embed: %v", embedErr)
			}

			if string(received) != testCase.want {
				test.Errorf("body = %s, want %s", received, testCase.want)
			}
		})
	}
}

// contextLimitServer stands in for Ollama's /api/embed with a model whose
// context holds limit bytes: it refuses a longer input with the HTTP 400 Ollama
// sends when truncate is false, and embeds the leading limit bytes when it is
// true. truncates returns the truncate flag of every request so far.
func contextLimitServer(test *testing.T, limit int) (*httptest.Server, func() []bool) {
	test.Helper()

	var (
		mu        sync.Mutex
		truncates []bool
	)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Input    string `json:"input"`
			Truncate bool   `json:"truncate"`
		}

		if decodeErr := json.NewDecoder(request.Body).Decode(&payload); decodeErr != nil {
			test.Errorf("decode: %v", decodeErr)
		}

		mu.Lock()
		truncates = append(truncates, payload.Truncate)
		mu.Unlock()

		if len(payload.Input) > limit && !payload.Truncate {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":"the input length exceeds the context length"}`))

			return
		}

		_ = json.NewEncoder(writer).Encode(map[string]any{"embeddings": [][]float64{{0.1, 0.2, 0.3}}})
	}))

	test.Cleanup(server.Close)

	return server, func() []bool {
		mu.Lock()
		defer mu.Unlock()

		return slices.Clone(truncates)
	}
}

// TestOllamaEmbedder_TruncatesInputLongerThanTheContext: an input the model
// refuses for length is sent again with truncate on, so it embeds the part
// that fits instead of failing, and the clipping is logged (#845).
func TestOllamaEmbedder_TruncatesInputLongerThanTheContext(test *testing.T) {
	server, truncates := contextLimitServer(test, 10)

	var buffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelDebug}))

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{Endpoint: server.URL, Model: "m", Dim: 3, Logger: logger})

	vector, embedErr := embedder.Embed(context.Background(), []byte(strings.Repeat("x", 50)))

	if embedErr != nil {
		test.Fatalf("Embed: %v", embedErr)
	}

	if len(vector) != 3 {
		test.Errorf("vector = %v, want 3 dims", vector)
	}

	if want := []bool{false, true}; !slices.Equal(truncates(), want) {
		test.Errorf("truncate flags = %v, want %v", truncates(), want)
	}

	output := buffer.String()

	if !strings.Contains(output, `msg="ollama input exceeds the model's context; embedded the part that fits"`) || !strings.Contains(output, "payload_bytes=50") {
		test.Errorf("expected a truncation warning naming the payload size; got %q", output)
	}

	if strings.Contains(output, `msg="ollama non-2xx"`) {
		test.Errorf("a refusal the retry recovered from must not log as a failure; got %q", output)
	}

	if _, shortErr := embedder.Embed(context.Background(), []byte("short")); shortErr != nil {
		test.Fatalf("Embed short: %v", shortErr)
	}

	if want := []bool{false, true, false}; !slices.Equal(truncates(), want) {
		test.Errorf("an input that fits must take one request; truncate flags = %v, want %v", truncates(), want)
	}
}

// TestOllamaEmbedder_RefusalTruncationCannotFix: a 400 that truncating does
// not cure is returned as the plain error it is, logged once.
func TestOllamaEmbedder_RefusalTruncationCannotFix(test *testing.T) {
	var requests atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)

		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":"invalid input type"}`))
	}))

	defer server.Close()

	var buffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelDebug}))

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{Endpoint: server.URL, Model: "m", Dim: 3, Logger: logger})

	_, embedErr := embedder.Embed(context.Background(), []byte("hello"))

	if embedErr == nil || !strings.Contains(embedErr.Error(), "invalid input type") {
		test.Fatalf("Embed error = %v, want the 400 body", embedErr)
	}

	if embed.IsTransportError(embedErr) {
		test.Errorf("a refusal must not be a TransportError; got %v", embedErr)
	}

	if got := requests.Load(); got != 2 {
		test.Errorf("requests = %d, want 2 (the refusal and the truncated retry)", got)
	}

	if count := strings.Count(buffer.String(), `msg="ollama non-2xx"`); count != 1 {
		test.Errorf("non-2xx logged %d times, want once", count)
	}
}

// TestOllamaEmbedder_RejectsResponseWithoutOneEmbedding: a response that does
// not carry exactly one vector for the one input is an error, not a panic or a
// silent empty vector.
func TestOllamaEmbedder_RejectsResponseWithoutOneEmbedding(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"embeddings":[]}`))
	}))

	defer server.Close()

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{Endpoint: server.URL, Model: "m", Dim: 3})

	if _, embedErr := embedder.Embed(context.Background(), []byte("hello")); embedErr == nil {
		test.Fatal("expected an error for a response with no embeddings")
	}
}
