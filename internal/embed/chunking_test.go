package embed_test

import (
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/embed"
)

func TestWholeDocument_ReturnsSingleChunk(test *testing.T) {
	strategy := embed.WholeDocument{}

	chunks := strategy.Chunk([]byte("hello world"))

	if len(chunks) != 1 {
		test.Fatalf("len = %d, want 1", len(chunks))
	}

	if !reflect.DeepEqual(chunks[0], []byte("hello world")) {
		test.Errorf("chunks[0] = %q", chunks[0])
	}
}

func TestWholeDocument_HandlesEmptyPayload(test *testing.T) {
	chunks := embed.WholeDocument{}.Chunk([]byte(""))

	if len(chunks) != 1 || len(chunks[0]) != 0 {
		test.Errorf("got %v, want one empty chunk", chunks)
	}
}
