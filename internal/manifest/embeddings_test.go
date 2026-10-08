package manifest_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

const embeddingsBase = `[workspace]
name = "x"

[embeddings]
provider = "ollama"
model    = "nomic-embed-text"
endpoint = "http://localhost:11434"
dim      = 768
`

func TestLoad_ParsesEmbeddingsModelSettings(test *testing.T) {
	loaded := loadTOMLFromString(test, embeddingsBase+`query-prefix        = "search_query: "
document-prefix     = "title: {title} | text: "
document-header     = "title"
chunk-target-bytes  = 800
chunk-max-bytes     = 1000
chunk-overlap-bytes = 100
num-ctx             = 512
`)

	section := loaded.Embeddings

	if section.QueryPrefix != "search_query: " {
		test.Errorf("QueryPrefix = %q", section.QueryPrefix)
	}

	if section.DocumentPrefix != "title: {title} | text: " {
		test.Errorf("DocumentPrefix = %q", section.DocumentPrefix)
	}

	if section.DocumentHeader != "title" {
		test.Errorf("DocumentHeader = %q", section.DocumentHeader)
	}

	if section.ChunkTargetBytes != 800 || section.ChunkMaxBytes != 1000 || section.ChunkOverlapBytes != 100 {
		test.Errorf("chunk sizes = %d/%d/%d, want 800/1000/100", section.ChunkTargetBytes, section.ChunkMaxBytes, section.ChunkOverlapBytes)
	}

	if section.NumCtx != 512 {
		test.Errorf("NumCtx = %d, want 512", section.NumCtx)
	}
}

func TestEmbeddingsSection_ResolvedDefaults(test *testing.T) {
	cases := []struct {
		name                             string
		section                          manifest.EmbeddingsSection
		wantTarget, wantMax, wantOverlap int
		wantHeader                       string
	}{
		{
			name:        "all absent",
			section:     manifest.EmbeddingsSection{},
			wantTarget:  1600,
			wantMax:     4000,
			wantOverlap: 200,
			wantHeader:  "full",
		},
		{
			name:        "partial override keeps the other defaults",
			section:     manifest.EmbeddingsSection{ChunkMaxBytes: 1000, ChunkTargetBytes: 800, DocumentHeader: "none"},
			wantTarget:  800,
			wantMax:     1000,
			wantOverlap: 200,
			wantHeader:  "none",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			target, maxBytes, overlap := testCase.section.ChunkSizes()

			if target != testCase.wantTarget || maxBytes != testCase.wantMax || overlap != testCase.wantOverlap {
				test.Errorf("ChunkSizes() = %d/%d/%d, want %d/%d/%d", target, maxBytes, overlap, testCase.wantTarget, testCase.wantMax, testCase.wantOverlap)
			}

			if header := testCase.section.ResolvedDocumentHeader(); header != testCase.wantHeader {
				test.Errorf("ResolvedDocumentHeader() = %q, want %q", header, testCase.wantHeader)
			}
		})
	}
}

func TestLoad_RejectsInvalidEmbeddingsModelSettings(test *testing.T) {
	cases := []struct {
		name      string
		extra     string
		wantInErr string
	}{
		{name: "negative target", extra: "chunk-target-bytes = -1\n", wantInErr: "chunk-target-bytes"},
		{name: "negative max", extra: "chunk-max-bytes = -1\n", wantInErr: "chunk-max-bytes"},
		{name: "negative overlap", extra: "chunk-overlap-bytes = -1\n", wantInErr: "chunk-overlap-bytes"},
		{name: "negative num-ctx", extra: "num-ctx = -1\n", wantInErr: "num-ctx"},
		{name: "unknown header mode", extra: "document-header = \"short\"\n", wantInErr: "document-header"},
		{name: "title placeholder in query prefix", extra: "query-prefix = \"q {title}: \"\n", wantInErr: "query-prefix"},
		// Lowering only the cap below the default target (1600) is the easy
		// mistake for a 512-token model; the error must name the default.
		{name: "max below default target", extra: "chunk-max-bytes = 1000\n", wantInErr: "1600"},
		{name: "target above max", extra: "chunk-target-bytes = 900\nchunk-max-bytes = 800\n", wantInErr: "chunk-target-bytes"},
		{name: "overlap equal to target", extra: "chunk-target-bytes = 300\nchunk-overlap-bytes = 300\n", wantInErr: "chunk-overlap-bytes"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			_, loadErr := loadTOMLString(test, embeddingsBase+testCase.extra)

			if loadErr == nil {
				test.Fatalf("expected a load error for %q", testCase.extra)
			}

			if !strings.Contains(loadErr.Error(), testCase.wantInErr) {
				test.Errorf("error should mention %q: %v", testCase.wantInErr, loadErr)
			}
		})
	}
}

func TestLoad_AcceptsEmbeddingsModelSettingsAtBoundaries(test *testing.T) {
	// target == max is legal (the cap and the packing goal coincide), and a
	// document-prefix may use {title} freely.
	loadTOMLFromString(test, embeddingsBase+`chunk-target-bytes  = 1000
chunk-max-bytes     = 1000
chunk-overlap-bytes = 999
document-prefix     = "title: {title} | text: "
`)
}

// TestLoad_RejectsHashInEmbeddingsModel: '#' joins a model to its options in
// the stored vector key, so a model name containing it would be ambiguous.
func TestLoad_RejectsHashInEmbeddingsModel(test *testing.T) {
	_, loadErr := loadTOMLString(test, `[workspace]
name = "x"

[embeddings]
provider = "ollama"
model    = "nomic-embed-text#v2"
endpoint = "http://localhost:11434"
dim      = 768
`)

	if loadErr == nil {
		test.Fatalf("expected a load error for '#' in embeddings.model")
	}

	if !strings.Contains(loadErr.Error(), "embeddings.model") || !strings.Contains(loadErr.Error(), "#") {
		test.Errorf("error should name embeddings.model and '#': %v", loadErr)
	}
}

// TestManifest_EmbeddingsKeyDefined tells an explicit empty prefix (a user
// opting out) from an absent one; doctor's prefix hint relies on it.
func TestManifest_EmbeddingsKeyDefined(test *testing.T) {
	loaded := loadTOMLFromString(test, embeddingsBase+"query-prefix = \"\"\n")

	if !loaded.EmbeddingsKeyDefined("query-prefix") {
		test.Errorf("explicit query-prefix = \"\" reported as undefined")
	}

	if loaded.EmbeddingsKeyDefined("document-prefix") {
		test.Errorf("absent document-prefix reported as defined")
	}

	if (&manifest.Manifest{}).EmbeddingsKeyDefined("query-prefix") {
		test.Errorf("a hand-built manifest has no decode metadata, so nothing is defined")
	}
}
