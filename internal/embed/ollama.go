package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// OllamaConfig configures an OllamaEmbedder.
type OllamaConfig struct {
	Endpoint string
	Model    string
	Dim      int
	Logger   *slog.Logger // optional; nil silences output
}

// OllamaEmbedder calls Ollama's POST /api/embeddings to embed payloads.
type OllamaEmbedder struct {
	config OllamaConfig
	client *http.Client
}

// NewOllamaEmbedder constructs an OllamaEmbedder with sensible HTTP defaults.
func NewOllamaEmbedder(config OllamaConfig) *OllamaEmbedder {
	return &OllamaEmbedder{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

const ollamaBodyLogLimit = 512

// Embed implements Embedder.
func (embedder *OllamaEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	body := struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}{
		Model:  embedder.config.Model,
		Prompt: string(payload),
	}

	encoded, marshalErr := json.Marshal(body)

	if marshalErr != nil {
		return nil, fmt.Errorf("ollama: marshal: %w", marshalErr)
	}

	if embedder.config.Logger != nil {
		embedder.config.Logger.Debug("ollama request",
			"endpoint", embedder.config.Endpoint,
			"model", embedder.config.Model,
			"bytes_sent", len(encoded),
		)
	}

	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, embedder.config.Endpoint+"/api/embeddings", bytes.NewReader(encoded))

	if requestErr != nil {
		return nil, fmt.Errorf("ollama: new request: %w", requestErr)
	}

	request.Header.Set("Content-Type", "application/json")

	start := time.Now()

	response, doErr := embedder.client.Do(request)

	if doErr != nil {
		return nil, fmt.Errorf("ollama: post: %w", doErr)
	}

	defer response.Body.Close()

	responseBody, readErr := io.ReadAll(response.Body)

	if readErr != nil {
		return nil, fmt.Errorf("ollama: read body: %w", readErr)
	}

	latency := time.Since(start)

	if response.StatusCode != http.StatusOK {
		if embedder.config.Logger != nil {
			truncated := responseBody

			if len(truncated) > ollamaBodyLogLimit {
				truncated = truncated[:ollamaBodyLogLimit]
			}

			embedder.config.Logger.Warn("ollama non-2xx",
				"endpoint", embedder.config.Endpoint,
				"model", embedder.config.Model,
				"status", response.StatusCode,
				"latency_ms", latency.Milliseconds(),
				"body", string(truncated),
			)
		}

		return nil, fmt.Errorf("ollama: HTTP %d: %s", response.StatusCode, string(responseBody))
	}

	var decoded struct {
		Embedding []float64 `json:"embedding"`
	}

	if decodeErr := json.Unmarshal(responseBody, &decoded); decodeErr != nil {
		return nil, fmt.Errorf("ollama: decode: %w", decodeErr)
	}

	if embedder.config.Logger != nil {
		embedder.config.Logger.Debug("ollama success",
			"endpoint", embedder.config.Endpoint,
			"model", embedder.config.Model,
			"status", response.StatusCode,
			"latency_ms", latency.Milliseconds(),
			"vector_dim", len(decoded.Embedding),
		)
	}

	if len(decoded.Embedding) != embedder.config.Dim {
		return nil, fmt.Errorf("ollama: returned %d dims, expected %d (model %q)", len(decoded.Embedding), embedder.config.Dim, embedder.config.Model)
	}

	vector := make([]float32, len(decoded.Embedding))

	for index, value := range decoded.Embedding {
		vector[index] = float32(value)
	}

	return vector, nil
}

// Model implements Embedder.
func (embedder *OllamaEmbedder) Model() string {
	return embedder.config.Model
}

// Dim implements Embedder.
func (embedder *OllamaEmbedder) Dim() int {
	return embedder.config.Dim
}
