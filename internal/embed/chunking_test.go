package embed_test

import (
	"bytes"
	"reflect"
	"strings"
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

func TestMarkdownRecursive_EmptyInputReturnsSingleEmptyChunk(test *testing.T) {
	chunks := embed.MarkdownRecursive{}.Chunk(nil)

	if len(chunks) != 1 {
		test.Fatalf("len = %d, want 1", len(chunks))
	}

	if len(chunks[0]) != 0 {
		test.Errorf("chunks[0] should be empty, got %q", chunks[0])
	}
}

func TestMarkdownRecursive_SmallInputReturnsSingleChunk(test *testing.T) {
	payload := []byte("a small body that fits in one chunk")

	chunks := embed.MarkdownRecursive{}.Chunk(payload)

	if len(chunks) != 1 {
		test.Fatalf("len = %d, want 1", len(chunks))
	}

	if string(chunks[0]) != string(payload) {
		test.Errorf("chunks[0] = %q, want %q", chunks[0], payload)
	}
}

func TestMarkdownRecursive_SplitsOnH2Headings(test *testing.T) {
	// Use small budgets so we force splits without needing huge fixtures.
	strategy := embed.MarkdownRecursive{
		TargetBytes:  60,
		MaxBytes:     200,
		OverlapBytes: 0,
	}

	body := []byte("intro line\n## Section A\nalpha alpha alpha alpha\n## Section B\nbravo bravo bravo bravo\n## Section C\ncharlie charlie\n")

	chunks := strategy.Chunk(body)

	if len(chunks) < 2 {
		test.Fatalf("expected multiple chunks, got %d: %q", len(chunks), chunks)
	}

	// Every "## " marker except (possibly) the first should appear at the
	// start of some chunk — i.e. heading boundaries are preserved.
	headingChunks := 0

	for _, chunk := range chunks {
		if strings.HasPrefix(string(bytes.TrimLeft(chunk, "\n")), "## ") {
			headingChunks++
		}
	}

	if headingChunks < 2 {
		test.Errorf("expected at least 2 chunks to start at a heading, got %d. chunks=%q", headingChunks, chunks)
	}
}

func TestMarkdownRecursive_FallsBackToParagraphs(test *testing.T) {
	strategy := embed.MarkdownRecursive{
		TargetBytes:  40,
		MaxBytes:     120,
		OverlapBytes: 0,
	}

	body := []byte("para one with some words here.\n\npara two also with words here.\n\npara three with more words here.")

	chunks := strategy.Chunk(body)

	if len(chunks) < 2 {
		test.Fatalf("expected multiple chunks, got %d: %q", len(chunks), chunks)
	}

	for _, chunk := range chunks {
		if len(chunk) > strategy.MaxBytes {
			test.Errorf("chunk %q exceeds MaxBytes %d (len=%d)", chunk, strategy.MaxBytes, len(chunk))
		}
	}
}

func TestMarkdownRecursive_HardSplitsWhenNoSeparators(test *testing.T) {
	strategy := embed.MarkdownRecursive{
		TargetBytes:  10,
		MaxBytes:     20,
		OverlapBytes: 0,
	}

	body := bytes.Repeat([]byte("X"), 100)

	chunks := strategy.Chunk(body)

	if len(chunks) < 5 {
		test.Fatalf("expected at least 5 chunks for a 100-byte all-Xs input with maxBytes=20, got %d", len(chunks))
	}

	for idx, chunk := range chunks {
		if len(chunk) > strategy.MaxBytes {
			test.Errorf("chunk %d exceeds MaxBytes %d (len=%d)", idx, strategy.MaxBytes, len(chunk))
		}
	}
}

func TestMarkdownRecursive_OverlapAppearsAtStartOfNextChunk(test *testing.T) {
	strategy := embed.MarkdownRecursive{
		TargetBytes:  40,
		MaxBytes:     200,
		OverlapBytes: 10,
	}

	// Build a body with three distinct paragraph blocks so it splits cleanly.
	body := []byte("alpha alpha alpha alpha alpha alpha\n\nbravo bravo bravo bravo bravo bravo\n\ncharlie charlie charlie charlie charlie")

	chunks := strategy.Chunk(body)

	if len(chunks) < 2 {
		test.Fatalf("need at least 2 chunks for overlap test, got %d: %q", len(chunks), chunks)
	}

	prevTail := chunks[0][len(chunks[0])-strategy.OverlapBytes:]
	nextHead := chunks[1][:strategy.OverlapBytes]

	if !bytes.Equal(prevTail, nextHead) {
		test.Errorf("expected chunks[1] to start with last %d bytes of chunks[0].\nprev tail: %q\nnext head: %q", strategy.OverlapBytes, prevTail, nextHead)
	}
}

func TestMarkdownRecursive_LargeDocStaysUnderMax(test *testing.T) {
	// Reproducer for the 2026-05-13 incident: a large synthetic doc should
	// chunk to many pieces, all under MaxBytes. Includes a long unbroken
	// run that forces the splitter to produce a piece near MaxBytes so the
	// overlap-overgrowth invariant gets exercised.
	body := bytes.Repeat([]byte("Some prose with paragraphs.\n\n## Heading\n\nMore prose here.\n\n"), 4000)
	// Splice in a 6000-byte unbroken run to force a near-MaxBytes piece.
	body = append(body, bytes.Repeat([]byte("Y"), 6000)...)

	strategy := embed.MarkdownRecursive{
		TargetBytes:  1600,
		MaxBytes:     7200,
		OverlapBytes: 200,
	}

	chunks := strategy.Chunk(body)

	if len(chunks) < 30 {
		test.Errorf("expected many chunks for a large doc, got %d", len(chunks))
	}

	for idx, chunk := range chunks {
		if len(chunk) > strategy.MaxBytes {
			test.Errorf("chunk %d has len=%d, exceeds MaxBytes %d", idx, len(chunk), strategy.MaxBytes)
		}
	}
}

func TestMarkdownRecursive_OverlapPlusLargePieceStaysUnderMax(test *testing.T) {
	// Regression: with overlap > 0 and a piece near MaxBytes, the previous
	// chunk's tail used to push the new chunk over MaxBytes (chunk grew to
	// MaxBytes + OverlapBytes). MaxBytes must remain a true hard cap.
	strategy := embed.MarkdownRecursive{
		TargetBytes:  50,
		MaxBytes:     100,
		OverlapBytes: 30,
	}

	// 50 'A's, a space, then 100 'X's — the 100 X's are one word/piece
	// near MaxBytes that follows a chunk eligible for overlap seeding.
	payload := append(bytes.Repeat([]byte("A"), 50), ' ')
	payload = append(payload, bytes.Repeat([]byte("X"), 100)...)

	chunks := strategy.Chunk(payload)

	for idx, chunk := range chunks {
		if len(chunk) > strategy.MaxBytes {
			test.Errorf("chunk %d has len=%d, exceeds MaxBytes=%d (overlap-overgrowth bug)", idx, len(chunk), strategy.MaxBytes)
		}
	}
}
