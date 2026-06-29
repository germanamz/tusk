package embed_test

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/manifest"
)

type stubEmbedder struct {
	model string
	dim   int
}

func (stub stubEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	return make([]float32, stub.dim), nil
}

func (stub stubEmbedder) Model() string { return stub.model }
func (stub stubEmbedder) Dim() int      { return stub.dim }

func TestEmbedder_InterfaceContract(test *testing.T) {
	var implementer embed.Embedder = stubEmbedder{model: "test", dim: 3}

	vector, embedErr := implementer.Embed(context.Background(), []byte("hello"))

	if embedErr != nil {
		test.Fatalf("Embed: %v", embedErr)
	}

	if len(vector) != 3 {
		test.Errorf("Vector len = %d", len(vector))
	}

	if implementer.Model() != "test" {
		test.Errorf("Model = %q", implementer.Model())
	}

	if implementer.Dim() != 3 {
		test.Errorf("Dim = %d", implementer.Dim())
	}
}

// TestNewFromManifest_Ollama pins C4: the shared constructor builds an Ollama
// embedder + a chunker from an [embeddings] block that selects ollama.
func TestNewFromManifest_Ollama(test *testing.T) {
	embedder, chunker := embed.NewFromManifest(manifest.EmbeddingsSection{
		Provider: "ollama",
		Endpoint: "http://localhost:11434",
		Model:    "nomic-embed-text",
		Dim:      768,
	}, nil)

	if embedder == nil {
		test.Fatal("embedder = nil, want a non-nil Ollama embedder")
	}

	if embedder.Model() != "nomic-embed-text" {
		test.Errorf("Model = %q, want nomic-embed-text", embedder.Model())
	}

	if embedder.Dim() != 768 {
		test.Errorf("Dim = %d, want 768", embedder.Dim())
	}

	if chunker == nil {
		test.Error("chunker = nil, want a ChunkingStrategy")
	}
}

// TestNewFromManifest_NoProvider confirms an absent or unsupported provider
// yields (nil, nil), so callers keep their "embedder == nil disables semantic
// features" handling.
func TestNewFromManifest_NoProvider(test *testing.T) {
	for _, provider := range []string{"", "openai"} {
		embedder, chunker := embed.NewFromManifest(manifest.EmbeddingsSection{Provider: provider}, nil)

		if embedder != nil || chunker != nil {
			test.Errorf("provider %q: got (%v, %v), want (nil, nil)", provider, embedder, chunker)
		}
	}
}
