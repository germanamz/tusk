package embed_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/embed"
)

func TestOllamaEmbedder_PostsToEmbeddingsEndpoint(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/embeddings" {
			test.Errorf("path = %q, want /api/embeddings", request.URL.Path)
		}

		var payload struct {
			Model  string `json:"model"`
			Prompt string `json:"prompt"`
		}

		if decodeErr := json.NewDecoder(request.Body).Decode(&payload); decodeErr != nil {
			test.Fatalf("decode: %v", decodeErr)
		}

		if payload.Model != "test-model" {
			test.Errorf("model = %q, want test-model", payload.Model)
		}

		if payload.Prompt != "hello world" {
			test.Errorf("prompt = %q", payload.Prompt)
		}

		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"embedding": []float64{0.1, 0.2, 0.3},
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
			"embedding": []float64{0.1, 0.2}, // 2 dims, but config says 3
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
		_, _ = writer.Write([]byte(`{"embedding":[0.1,0.2,0.3]}`))
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
		_, _ = writer.Write([]byte(`{"embedding":[0.1,0.2,0.3]}`))
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
