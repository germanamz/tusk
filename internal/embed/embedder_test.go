package embed_test

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/embed"
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
