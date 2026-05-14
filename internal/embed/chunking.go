package embed

import "bytes"

// ChunkingStrategy splits a body payload into one or more chunks; each chunk
// is embedded independently. The drain loop prepends a per-node header to
// each chunk before embedding, so chunkers operate on the body only.
type ChunkingStrategy interface {
	Chunk(payload []byte) [][]byte
}

// WholeDocument returns the entire payload as a single chunk. Retained for
// short docs and tests; production paths use MarkdownRecursive.
type WholeDocument struct{}

// Chunk implements ChunkingStrategy.
func (strategy WholeDocument) Chunk(payload []byte) [][]byte {
	return [][]byte{payload}
}

// MarkdownRecursive splits a body using recursive descent through
// markdown-aware separators (H2 -> H3 -> paragraph -> line -> sentence ->
// word -> byte). Pieces are then greedily packed up to TargetBytes, with
// the previous chunk's tail seeding OverlapBytes of context into the next
// chunk. MaxBytes is a hard cap that keeps every chunk under the embedding
// model's context window.
//
// Zero-value defaults target ~400 tokens with ~50-token overlap for
// nomic-embed-text (2048-token window): TargetBytes=1600, MaxBytes=4000,
// OverlapBytes=200. MaxBytes is sized for ~2 bytes/token (code-dense
// markdown) so chunks stay under 2048 tokens worst case; prose chunks
// rarely approach the cap because TargetBytes triggers emit first.
type MarkdownRecursive struct {
	TargetBytes  int
	MaxBytes     int
	OverlapBytes int
}

const (
	defaultTargetBytes  = 1600
	defaultMaxBytes     = 4000
	defaultOverlapBytes = 200
)

var markdownSeparators = []string{
	"\n## ",
	"\n### ",
	"\n\n",
	"\n",
	". ",
	" ",
	"",
}

func (strategy MarkdownRecursive) target() int {
	if strategy.TargetBytes > 0 {
		return strategy.TargetBytes
	}

	return defaultTargetBytes
}

func (strategy MarkdownRecursive) maxSize() int {
	if strategy.MaxBytes > 0 {
		return strategy.MaxBytes
	}

	return defaultMaxBytes
}

func (strategy MarkdownRecursive) overlap() int {
	if strategy.OverlapBytes > 0 {
		return strategy.OverlapBytes
	}

	return defaultOverlapBytes
}

// Chunk implements ChunkingStrategy.
func (strategy MarkdownRecursive) Chunk(payload []byte) [][]byte {
	if len(payload) == 0 {
		return [][]byte{nil}
	}

	pieces := splitRecursive(payload, markdownSeparators, strategy.maxSize())
	chunks := packPieces(pieces, strategy.target(), strategy.overlap(), strategy.maxSize())

	if len(chunks) == 0 {
		return [][]byte{nil}
	}

	return chunks
}

// splitRecursive walks separators from highest priority to lowest, splitting
// text at the first separator that occurs in it. Pieces that still exceed
// maxBytes recurse to the next separator. The empty-string separator is the
// floor and guarantees termination.
func splitRecursive(text []byte, separators []string, maxBytes int) [][]byte {
	for idx, sep := range separators {
		if sep == "" {
			// Floor: byte-level hard split
			if len(text) <= maxBytes {
				return [][]byte{text}
			}

			return hardSplit(text, maxBytes)
		}

		if !bytes.Contains(text, []byte(sep)) {
			continue
		}

		var pieces [][]byte

		for _, piece := range splitKeepingPrefix(text, sep) {
			if len(piece) <= maxBytes {
				pieces = append(pieces, piece)
				continue
			}

			pieces = append(pieces, splitRecursive(piece, separators[idx+1:], maxBytes)...)
		}

		return pieces
	}

	// Should never reach here due to empty-string floor
	return [][]byte{text}
}

// splitKeepingPrefix splits text on sep, keeping sep as the prefix of every
// piece after the first. Empty pieces are dropped.
func splitKeepingPrefix(text []byte, sep string) [][]byte {
	sepBytes := []byte(sep)
	parts := bytes.Split(text, sepBytes)
	pieces := make([][]byte, 0, len(parts))

	for idx, part := range parts {
		var piece []byte

		if idx == 0 {
			piece = part
		} else {
			piece = make([]byte, 0, len(sepBytes)+len(part))
			piece = append(piece, sepBytes...)
			piece = append(piece, part...)
		}

		if len(piece) > 0 {
			pieces = append(pieces, piece)
		}
	}

	return pieces
}

// hardSplit cuts text into fixed-size byte windows. Last resort when no
// markdown separator occurs in text.
func hardSplit(text []byte, maxBytes int) [][]byte {
	var out [][]byte

	for offset := 0; offset < len(text); offset += maxBytes {
		end := offset + maxBytes

		if end > len(text) {
			end = len(text)
		}

		out = append(out, text[offset:end])
	}

	return out
}

// packPieces greedily concatenates pieces up to target bytes, then emits a
// chunk and seeds the next chunk with the previous chunk's tail (OverlapBytes).
// A single piece larger than target becomes its own chunk. maxBytes is a hard
// cap: pieces will force an emit if accumulating would exceed it, and the
// seeded tail is shrunk so tail+piece never exceeds maxBytes.
func packPieces(pieces [][]byte, target, overlap, maxBytes int) [][]byte {
	var (
		chunks [][]byte
		cur    []byte
	)

	for _, piece := range pieces {
		overTarget := len(cur) > 0 && len(cur)+len(piece) > target
		overMax := len(cur) > 0 && len(cur)+len(piece) > maxBytes

		if overTarget || overMax {
			chunks = append(chunks, cur)

			if overlap > 0 && len(cur) > overlap {
				tail := cur[len(cur)-overlap:]

				room := maxBytes - len(piece)
				if room < 0 {
					room = 0
				}

				if len(tail) > room {
					tail = tail[len(tail)-room:]
				}

				cur = make([]byte, 0, len(tail)+len(piece))
				cur = append(cur, tail...)
			} else {
				cur = nil
			}
		}

		cur = append(cur, piece...)
	}

	if len(cur) > 0 {
		chunks = append(chunks, cur)
	}

	return chunks
}
