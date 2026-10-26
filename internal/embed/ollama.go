package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	NumCtx   int           // optional; > 0 is sent as options.num_ctx
	Format   Format        // optional; zero is no prefixes and the full header
}

// OllamaEmbedder calls Ollama's POST /api/embed (Ollama 0.3.4 or newer) to
// embed payloads.
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

// ollamaRequest is the /api/embed request body. Options is omitted when nil,
// so a request without num-ctx runs the model with Ollama's own defaults.
// Truncate is always sent (see Embed).
type ollamaRequest struct {
	Model    string         `json:"model"`
	Input    string         `json:"input"`
	Truncate bool           `json:"truncate"`
	Options  *ollamaOptions `json:"options,omitempty"`
}

type ollamaOptions struct {
	NumCtx int `json:"num_ctx"`
}

// ollamaStatusError is a non-2xx answer from Ollama, kept apart from the
// error Embed returns so Embed can tell a refusal it may retry (400) from the
// rest before logging anything.
type ollamaStatusError struct {
	status int
	body   []byte
}

func (statusErr *ollamaStatusError) Error() string {
	return fmt.Sprintf("ollama: HTTP %d: %s", statusErr.status, string(statusErr.body))
}

// Embed implements Embedder.
//
// The input goes out with truncate off, so a text longer than the model's
// context is refused (HTTP 400) rather than clipped without a word. Embed
// then sends it again with truncate on: when length was the problem, the model
// embeds the part that fits and Embed logs that it did; when it was not, the
// model refuses again and that refusal is returned. A text that fits costs one
// request (#845).
func (embedder *OllamaEmbedder) Embed(ctx context.Context, text []byte) ([]float32, error) {
	vector, embedErr := embedder.post(ctx, text, false)

	var statusErr *ollamaStatusError

	if errors.As(embedErr, &statusErr) && statusErr.status == http.StatusBadRequest {
		vector, embedErr = embedder.post(ctx, text, true)

		if embedErr == nil && embedder.config.Logger != nil {
			embedder.config.Logger.Warn("ollama input exceeds the model's context; embedded the part that fits",
				"model", embedder.config.Model,
				"payload_bytes", len(text),
			)
		}
	}

	if errors.As(embedErr, &statusErr) {
		return nil, embedder.statusFailure(statusErr)
	}

	return vector, embedErr
}

// statusFailure logs a non-2xx answer and classifies it. 5xx is Ollama failing
// to serve (overloaded, restarting), so it is a TransportError; a 4xx stays a
// plain error. Either way the drain probes the backend to tell a bad input
// from a backend that can't serve at all.
func (embedder *OllamaEmbedder) statusFailure(statusErr *ollamaStatusError) error {
	if embedder.config.Logger != nil {
		truncated := statusErr.body

		if len(truncated) > ollamaBodyLogLimit {
			truncated = truncated[:ollamaBodyLogLimit]
		}

		embedder.config.Logger.Warn("ollama non-2xx",
			"endpoint", embedder.config.Endpoint,
			"model", embedder.config.Model,
			"status", statusErr.status,
			"body", string(truncated),
		)
	}

	httpErr := errors.New(statusErr.Error())

	if statusErr.status >= http.StatusInternalServerError {
		return &TransportError{Err: httpErr}
	}

	return httpErr
}

// post sends one /api/embed request. A non-2xx answer comes back as an
// *ollamaStatusError, unlogged; Embed decides what it means.
func (embedder *OllamaEmbedder) post(ctx context.Context, text []byte, truncate bool) ([]float32, error) {
	body := ollamaRequest{
		Model:    embedder.config.Model,
		Input:    string(text),
		Truncate: truncate,
	}

	if embedder.config.NumCtx > 0 {
		body.Options = &ollamaOptions{NumCtx: embedder.config.NumCtx}
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
			"truncate", truncate,
		)
	}

	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, embedder.config.Endpoint+"/api/embed", bytes.NewReader(encoded))

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
		return nil, &ollamaStatusError{status: response.StatusCode, body: responseBody}
	}

	var decoded struct {
		Embeddings [][]float64 `json:"embeddings"`
	}

	if decodeErr := json.Unmarshal(responseBody, &decoded); decodeErr != nil {
		return nil, fmt.Errorf("ollama: decode: %w", decodeErr)
	}

	if len(decoded.Embeddings) != 1 {
		return nil, fmt.Errorf("ollama: returned %d embeddings for one input (model %q)", len(decoded.Embeddings), embedder.config.Model)
	}

	embedding := decoded.Embeddings[0]

	if len(embedding) != embedder.config.Dim {
		return nil, fmt.Errorf("ollama: returned %d dims, expected %d (model %q)", len(embedding), embedder.config.Dim, embedder.config.Model)
	}

	if embedder.config.Logger != nil {
		embedder.config.Logger.Debug("ollama success",
			"endpoint", embedder.config.Endpoint,
			"model", embedder.config.Model,
			"status", response.StatusCode,
			"latency_ms", latency.Milliseconds(),
			"vector_dim", len(embedding),
		)
	}

	vector := make([]float32, len(embedding))

	for index, value := range embedding {
		vector[index] = float32(value)
	}

	return vector, nil
}

// Model implements Embedder.
func (embedder *OllamaEmbedder) Model() string {
	return embedder.config.Model
}

// VectorKey implements Embedder.
func (embedder *OllamaEmbedder) VectorKey() string {
	return VectorKey(embedder.config.Model, embedder.config.NumCtx)
}

// Format implements Embedder.
func (embedder *OllamaEmbedder) Format() Format {
	return embedder.config.Format
}

// Dim implements Embedder.
func (embedder *OllamaEmbedder) Dim() int {
	return embedder.config.Dim
}
