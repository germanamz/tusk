package embed_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
