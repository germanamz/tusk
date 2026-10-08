package embed_test

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

func TestFormat_Query(test *testing.T) {
	cases := []struct {
		name   string
		format embed.Format
		want   string
	}{
		{name: "zero format sends the query unchanged", format: embed.Format{}, want: "what is tusk"},
		{name: "prefix is prepended verbatim", format: embed.Format{QueryPrefix: "search_query: "}, want: "search_query: what is tusk"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			if got := string(testCase.format.Query([]byte("what is tusk"))); got != testCase.want {
				test.Errorf("Query = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestFormat_Document(test *testing.T) {
	cases := []struct {
		name   string
		prefix string
		title  string
		want   string
	}{
		{name: "no prefix", prefix: "", title: "Ignored", want: "body"},
		{name: "plain prefix", prefix: "search_document: ", title: "Ignored", want: "search_document: body"},
		{name: "title placeholder", prefix: "title: {title} | text: ", title: "My Note", want: "title: My Note | text: body"},
		{name: "empty title renders none", prefix: "title: {title} | text: ", title: "", want: "title: none | text: body"},
		{name: "other braces pass through", prefix: "{kind} {title}: ", title: "T", want: "{kind} T: body"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			format := embed.Format{DocumentPrefix: testCase.prefix}

			if got := string(format.Document(testCase.title, []byte("body"))); got != testCase.want {
				test.Errorf("Document = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestFormat_Header(test *testing.T) {
	titled := &node.Node{
		Type:       "ticket",
		Title:      "Fix login",
		Properties: map[string]any{"type": "ticket", "title": "Fix login", "priority": 3},
	}

	untitled := &node.Node{
		Type:       "note",
		Properties: map[string]any{"type": "note"},
	}

	cases := []struct {
		name   string
		mode   embed.HeaderMode
		parsed *node.Node
		want   string
	}{
		{name: "zero mode is the full header", mode: "", parsed: titled, want: "[type] ticket\n[title] Fix login\npriority=3\n---\n"},
		{name: "full", mode: embed.HeaderFull, parsed: titled, want: "[type] ticket\n[title] Fix login\npriority=3\n---\n"},
		{name: "title", mode: embed.HeaderTitle, parsed: titled, want: "[title] Fix login\n---\n"},
		{name: "title without a title writes nothing", mode: embed.HeaderTitle, parsed: untitled, want: ""},
		{name: "none", mode: embed.HeaderNone, parsed: titled, want: ""},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			format := embed.Format{HeaderMode: testCase.mode}

			if got := string(format.Header(testCase.parsed)); got != testCase.want {
				test.Errorf("Header = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestVectorKey(test *testing.T) {
	if got := embed.VectorKey("nomic-embed-text", 0); got != "nomic-embed-text" {
		test.Errorf("VectorKey without num-ctx = %q, want the bare model", got)
	}

	if got := embed.VectorKey("nomic-embed-text", 8192); got != "nomic-embed-text#num-ctx=8192" {
		test.Errorf("VectorKey with num-ctx = %q", got)
	}
}

func TestDisplayModel(test *testing.T) {
	if got := embed.DisplayModel("nomic-embed-text#num-ctx=8192"); got != "nomic-embed-text" {
		test.Errorf("DisplayModel = %q, want nomic-embed-text", got)
	}

	if got := embed.DisplayModel("hf.co/org/model:q8"); got != "hf.co/org/model:q8" {
		test.Errorf("DisplayModel of a bare key = %q, want it unchanged", got)
	}
}

func TestDocumentFingerprint(test *testing.T) {
	base := manifest.EmbeddingsSection{Provider: "ollama", Model: "nomic-embed-text", Endpoint: "http://localhost:11434", Dim: 768}
	baseline := embed.DocumentFingerprint(base)

	same := map[string]manifest.EmbeddingsSection{
		"explicit defaults": withSection(base, func(section *manifest.EmbeddingsSection) {
			section.DocumentHeader = "full"
			section.ChunkTargetBytes = 1600
			section.ChunkMaxBytes = 4000
			section.ChunkOverlapBytes = 200
		}),
		"query-prefix":    withSection(base, func(section *manifest.EmbeddingsSection) { section.QueryPrefix = "search_query: " }),
		"endpoint":        withSection(base, func(section *manifest.EmbeddingsSection) { section.Endpoint = "http://gpu-box:11434" }),
		"timeout-seconds": withSection(base, func(section *manifest.EmbeddingsSection) { section.TimeoutSeconds = 300 }),
	}

	for name, section := range same {
		if embed.DocumentFingerprint(section) != baseline {
			test.Errorf("changing %s must not change the document fingerprint", name)
		}
	}

	different := map[string]manifest.EmbeddingsSection{
		"model":           withSection(base, func(section *manifest.EmbeddingsSection) { section.Model = "mxbai-embed-large" }),
		"num-ctx":         withSection(base, func(section *manifest.EmbeddingsSection) { section.NumCtx = 2048 }),
		"document-prefix": withSection(base, func(section *manifest.EmbeddingsSection) { section.DocumentPrefix = "search_document: " }),
		"document-header": withSection(base, func(section *manifest.EmbeddingsSection) { section.DocumentHeader = "none" }),
		"chunk target":    withSection(base, func(section *manifest.EmbeddingsSection) { section.ChunkTargetBytes = 1200 }),
		"chunk max":       withSection(base, func(section *manifest.EmbeddingsSection) { section.ChunkMaxBytes = 3000 }),
		"chunk overlap":   withSection(base, func(section *manifest.EmbeddingsSection) { section.ChunkOverlapBytes = 100 }),
	}

	for name, section := range different {
		if embed.DocumentFingerprint(section) == baseline {
			test.Errorf("changing %s must change the document fingerprint", name)
		}
	}
}

func TestNewFromManifest_AppliesModelSettings(test *testing.T) {
	embedder, chunker := embed.NewFromManifest(manifest.EmbeddingsSection{
		Provider:          "ollama",
		Endpoint:          "http://localhost:11434",
		Model:             "mxbai-embed-large",
		Dim:               1024,
		QueryPrefix:       "q: ",
		DocumentPrefix:    "d: ",
		DocumentHeader:    "none",
		ChunkTargetBytes:  80,
		ChunkMaxBytes:     100,
		ChunkOverlapBytes: 10,
		NumCtx:            512,
	}, nil)

	want := embed.Format{QueryPrefix: "q: ", DocumentPrefix: "d: ", HeaderMode: embed.HeaderNone}

	if embedder.Format() != want {
		test.Errorf("Format = %+v, want %+v", embedder.Format(), want)
	}

	if embedder.VectorKey() != "mxbai-embed-large#num-ctx=512" {
		test.Errorf("VectorKey = %q", embedder.VectorKey())
	}

	// 600 bytes is one chunk under the default 4000-byte cap; the configured
	// 100-byte cap must split it.
	body := make([]byte, 0, 600)

	for len(body) < 600 {
		body = append(body, "word "...)
	}

	chunks := chunker.Chunk(body)

	if len(chunks) < 2 {
		test.Fatalf("got %d chunk(s), want the configured cap to split the body", len(chunks))
	}

	for index, chunk := range chunks {
		if len(chunk) > 100 {
			test.Errorf("chunk %d is %d bytes, over the configured 100-byte cap", index, len(chunk))
		}
	}
}

func TestEmbedQuery_AppliesQueryPrefix(test *testing.T) {
	recorder := &recordingEmbedder{
		model:  "nomic-embed-text",
		dim:    3,
		format: embed.Format{QueryPrefix: "search_query: ", DocumentPrefix: "search_document: "},
	}

	if _, embedErr := embed.EmbedQuery(context.Background(), recorder, []byte("graph expansion")); embedErr != nil {
		test.Fatalf("EmbedQuery: %v", embedErr)
	}

	if len(recorder.payloads) != 1 || string(recorder.payloads[0]) != "search_query: graph expansion" {
		test.Errorf("embedder received %q, want [\"search_query: graph expansion\"]", recorder.payloads)
	}
}

func withSection(base manifest.EmbeddingsSection, mutate func(*manifest.EmbeddingsSection)) manifest.EmbeddingsSection {
	mutate(&base)

	return base
}
