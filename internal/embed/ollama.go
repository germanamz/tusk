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
	Logger   *slog.Logger  // optional; nil silences output
	Timeout  time.Duration // optional; zero falls back to 30s
}

// OllamaEmbedder calls Ollama's POST /api/embeddings to embed payloads.
type OllamaEmbedder struct {
	config OllamaConfig
	client *http.Client
}

// defaultOllamaTimeout is the fallback HTTP client timeout when
// OllamaConfig.Timeout is unset. Production callers pass a larger value
// from manifest.Embeddings.TimeoutSeconds (default 120s); this constant
// preserves prior behavior for callers that don't set the field.
const defaultOllamaTimeout = 30 * time.Second

// NewOllamaEmbedder constructs an OllamaEmbedder with sensible HTTP defaults.
func NewOllamaEmbedder(config OllamaConfig) *OllamaEmbedder {
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultOllamaTimeout
	}

	return &OllamaEmbedder{
		config: config,
		client: &http.Client{Timeout: timeout},
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
		// Connection refused, DNS failure, timeout: Ollama is unreachable, not a
		// fault of this payload. Mark transport so the drain backs off.
		return nil, &TransportError{Err: fmt.Errorf("ollama: post: %w", doErr)}
	}

	defer response.Body.Close()

	responseBody, readErr := io.ReadAll(response.Body)

	if readErr != nil {
		// A mid-read connection drop is the same class of transient trouble.
		return nil, &TransportError{Err: fmt.Errorf("ollama: read body: %w", readErr)}
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

		httpErr := fmt.Errorf("ollama: HTTP %d: %s", response.StatusCode, string(responseBody))

		// 5xx is Ollama failing to serve a healthy request (overloaded,
		// restarting) — transient, so the drain should back off and retry. A 4xx
		// is a per-request fault (bad payload, missing model) that retrying
		// verbatim won't fix, so it stays a plain error subject to the drop policy.
		if response.StatusCode >= http.StatusInternalServerError {
			return nil, &TransportError{Err: httpErr}
		}

		return nil, httpErr
	}

	var decoded struct {
		Embedding []float64 `json:"embedding"`
	}

	if decodeErr := json.Unmarshal(responseBody, &decoded); decodeErr != nil {
		return nil, fmt.Errorf("ollama: decode: %w", decodeErr)
	}

	if len(decoded.Embedding) != embedder.config.Dim {
		return nil, fmt.Errorf("ollama: returned %d dims, expected %d (model %q)", len(decoded.Embedding), embedder.config.Dim, embedder.config.Model)
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
