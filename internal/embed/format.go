package embed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

// HeaderMode selects the block Format.Header prepends to each file chunk.
// The zero value behaves as HeaderFull.
type HeaderMode string

const (
	HeaderFull  HeaderMode = manifest.DocumentHeaderFull
	HeaderTitle HeaderMode = manifest.DocumentHeaderTitle
	HeaderNone  HeaderMode = manifest.DocumentHeaderNone
)

// untitled is what {title} renders as when there is no title: a file without
// one, and every sub-unit. EmbeddingGemma's documented document template
// (`title: {title | "none"} | text: `) uses the same fallback.
const untitled = "none"

// Format shapes the text an Embedder is handed: the instruction prefixes
// asymmetric models are trained with, and the header prepended to file
// chunks. Its zero value is no prefixes and the full header, the shape tusk
// sent before these settings existed.
//
// The drain builds the full document text with Format and hashes exactly
// those bytes, so every setting that changes the text also changes the
// content hash and no vector embedded under other settings is reused.
type Format struct {
	QueryPrefix    string
	DocumentPrefix string
	HeaderMode     HeaderMode
}

// Query returns the semantic query text: QueryPrefix followed by query.
func (format Format) Query(query []byte) []byte {
	return prepend(format.QueryPrefix, query)
}

// Document returns the document text: DocumentPrefix, with {title} replaced
// by title (or "none" when title is empty), followed by payload.
func (format Format) Document(title string, payload []byte) []byte {
	if title == "" {
		title = untitled
	}

	return prepend(strings.ReplaceAll(format.DocumentPrefix, manifest.TitlePlaceholder, title), payload)
}

// Header renders the block prepended to every chunk of a file node. HeaderFull
// is the node's type, title, and sorted remaining properties followed by a
// `---` separator; HeaderTitle keeps only the title line and the separator
// (nothing for an untitled node); HeaderNone renders nothing.
func (format Format) Header(parsedNode *node.Node) []byte {
	switch format.HeaderMode {
	case HeaderNone:
		return nil
	case HeaderTitle:
		if parsedNode.Title == "" {
			return nil
		}

		return fmt.Appendf(nil, "[title] %s\n---\n", parsedNode.Title)
	default:
		return BuildHeader(parsedNode)
	}
}

func prepend(prefix string, payload []byte) []byte {
	if prefix == "" {
		return payload
	}

	out := make([]byte, 0, len(prefix)+len(payload))
	out = append(out, prefix...)
	out = append(out, payload...)

	return out
}

// EmbedQuery embeds a semantic query string, applying the embedder's query
// prefix. Query call sites use this rather than Embed so the prefix can't be
// forgotten.
func EmbedQuery(ctx context.Context, embedder Embedder, query []byte) ([]float32, error) {
	return embedder.Embed(ctx, embedder.Format().Query(query))
}

// vectorKeySeparator joins a model name to the request options that change
// its output. Ollama model names cannot contain '#'.
const vectorKeySeparator = "#"

// VectorKey is the identity stored vectors are keyed on (the model column of
// the embeddings tables): the model name, plus the request options that make
// the same text embed differently. With numCtx unset it is the bare model
// name, so vectors stored before num-ctx existed keep matching.
func VectorKey(model string, numCtx int) string {
	if numCtx <= 0 {
		return model
	}

	return model + vectorKeySeparator + "num-ctx=" + strconv.Itoa(numCtx)
}

// VectorKeyFor returns the vector key a workspace's [embeddings] section
// produces.
func VectorKeyFor(section manifest.EmbeddingsSection) string {
	return VectorKey(section.Model, section.NumCtx)
}

// DisplayModel strips the option suffix from a stored vector key, leaving the
// model name for display.
func DisplayModel(key string) string {
	model, _, _ := strings.Cut(key, vectorKeySeparator)

	return model
}

// DocumentFingerprintKey is the meta key holding the DocumentFingerprint the
// index's embed work must be done under. reindex records it before queueing a
// re-embed; a drainer whose own fingerprint differs stops (see
// DrainConfig.Fingerprint).
const DocumentFingerprintKey = "embed_document_fingerprint"

// documentFingerprintVersion is folded into DocumentFingerprint so a future
// change to its encoding reads as a mismatch instead of a false match.
const documentFingerprintVersion = "v1"

// DocumentFingerprint identifies every [embeddings] setting a stored document
// vector depends on: the vector key (model and num-ctx), document-prefix,
// document-header, and the file chunk sizes, with defaults filled in so an
// explicit default and an absent key fingerprint the same. query-prefix is
// excluded because no stored vector depends on it. reindex compares this
// against the value recorded under DocumentFingerprintKey and re-embeds every
// node when they differ.
func DocumentFingerprint(section manifest.EmbeddingsSection) string {
	target, maxBytes, overlap := section.ChunkSizes()

	fields := []string{
		documentFingerprintVersion,
		VectorKeyFor(section),
		section.DocumentPrefix,
		section.ResolvedDocumentHeader(),
		strconv.Itoa(target),
		strconv.Itoa(maxBytes),
		strconv.Itoa(overlap),
	}

	sum := sha256.Sum256([]byte(strings.Join(fields, "\x00")))

	return hex.EncodeToString(sum[:])
}
