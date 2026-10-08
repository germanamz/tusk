package doctor_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

func runDoctorWith(test *testing.T, store *index.Index, section manifest.EmbeddingsSection) *doctor.Report {
	test.Helper()

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: index.NewEmbedQueueRepo(store),
		Embeddings: index.NewEmbeddingRepo(store),
		Manifest:   &manifest.Manifest{Embeddings: section},
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	return report
}

func issuesOfKind(report *doctor.Report, kind string) []doctor.Issue {
	var matched []doctor.Issue

	for _, issue := range report.Issues {
		if issue.Kind == kind {
			matched = append(matched, issue)
		}
	}

	return matched
}

func openDoctorStore(test *testing.T) *index.Index {
	test.Helper()

	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("open: %v", openErr)
	}

	test.Cleanup(func() { _ = store.Close() })

	return store
}

// TestRun_EmbeddingDriftComparesVectorKey: a num-ctx change keys new vectors
// as model#num-ctx=N, so vectors under the bare model are drift until the
// drain re-embeds them, and vectors under the matching key are not.
func TestRun_EmbeddingDriftComparesVectorKey(test *testing.T) {
	section := manifest.EmbeddingsSection{Provider: "ollama", Model: "nomic-embed-text", Dim: 768, NumCtx: 8192}

	stale := openDoctorStore(test)
	seedDriftNode(test, index.NewNodeRepo(stale), index.NewEmbeddingRepo(stale), "nomic-embed-text", 768)

	drift := issuesOfKind(runDoctorWith(test, stale, section), doctor.IssueEmbeddingDrift)

	if len(drift) != 1 {
		test.Fatalf("got %d embedding-drift issues, want 1", len(drift))
	}

	if !strings.Contains(drift[0].Message, "tusk reindex") {
		test.Errorf("drift advice should point at `tusk reindex`: %s", drift[0].Message)
	}

	current := openDoctorStore(test)
	seedDriftNode(test, index.NewNodeRepo(current), index.NewEmbeddingRepo(current), "nomic-embed-text#num-ctx=8192", 768)

	if drift := issuesOfKind(runDoctorWith(test, current, section), doctor.IssueEmbeddingDrift); len(drift) != 0 {
		test.Errorf("vectors under the configured vector key reported as drift: %+v", drift)
	}
}

// TestRun_LargeChunkUsesConfiguredMax: the large-chunk warning fires at 90%
// of the configured chunk-max-bytes, not the 4000-byte default.
func TestRun_LargeChunkUsesConfiguredMax(test *testing.T) {
	store := openDoctorStore(test)
	nodes := index.NewNodeRepo(store)
	embeddings := index.NewEmbeddingRepo(store)

	if upsertErr := nodes.Upsert(index.NodeRow{ID: "a", Type: "note", Title: "A", Path: "a.md", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert node: %v", upsertErr)
	}

	if upsertErr := embeddings.Upsert(index.EmbeddingRow{
		NodeID: "a", ChunkIdx: 0, Model: "mxbai-embed-large", ContentHash: "h",
		Vector: make([]float32, 3), Dim: 3, Body: strings.Repeat("x", 950),
	}); upsertErr != nil {
		test.Fatalf("upsert embedding: %v", upsertErr)
	}

	defaults := manifest.EmbeddingsSection{Provider: "ollama", Model: "mxbai-embed-large", Dim: 3}

	if large := issuesOfKind(runDoctorWith(test, store, defaults), doctor.IssueEmbedLargeChunk); len(large) != 0 {
		test.Errorf("950-byte chunk flagged under the 4000-byte default cap: %+v", large)
	}

	lowered := defaults
	lowered.ChunkTargetBytes = 800
	lowered.ChunkMaxBytes = 1000

	if large := issuesOfKind(runDoctorWith(test, store, lowered), doctor.IssueEmbedLargeChunk); len(large) != 1 {
		test.Errorf("950-byte chunk under a 1000-byte cap: got %d large-chunk issues, want 1", len(large))
	}
}

// TestRun_OversizeSubUnitPayloadUsesConfiguredMax: the sub-unit pane counts
// leaves over the configured chunk-max-bytes.
func TestRun_OversizeSubUnitPayloadUsesConfiguredMax(test *testing.T) {
	store := openDoctorStore(test)
	nodes := index.NewNodeRepo(store)

	if upsertErr := nodes.Upsert(index.NodeRow{ID: "notes/n", Type: "note", Path: "notes/n.md", Title: "N", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert parent: %v", upsertErr)
	}

	if bulkErr := nodes.BulkUpsert([]index.NodeRow{{
		ID: "notes/n#p", Type: "paragraph", Path: "notes/n.md", PropertiesJSON: "{}", LastChecksum: "x",
		ParentID:     sql.NullString{String: "notes/n", Valid: true},
		Ordinal:      sql.NullInt64{Int64: 0, Valid: true},
		EmbedPayload: sql.NullString{String: strings.Repeat("y", 1500), Valid: true},
	}}, "markdown"); bulkErr != nil {
		test.Fatalf("upsert sub-unit: %v", bulkErr)
	}

	defaults := manifest.EmbeddingsSection{Provider: "ollama", Model: "mxbai-embed-large", Dim: 3}

	if pane := runDoctorWith(test, store, defaults).SubUnitPane; pane == nil || pane.OversizeEmbedPayloads != 0 {
		test.Errorf("1500-byte leaf counted oversize under the default cap: %+v", pane)
	}

	lowered := defaults
	lowered.ChunkTargetBytes = 800
	lowered.ChunkMaxBytes = 1000

	if pane := runDoctorWith(test, store, lowered).SubUnitPane; pane == nil || pane.OversizeEmbedPayloads != 1 {
		test.Errorf("1500-byte leaf under a 1000-byte cap: pane = %+v, want OversizeEmbedPayloads 1", pane)
	}
}

func TestRun_EmbeddingPrefixHint(test *testing.T) {
	const represent = "Represent this sentence for searching relevant passages: "

	cases := []struct {
		name           string
		model          string
		queryPrefix    string
		documentPrefix string
		wantHint       bool
		wantStrings    []string
		rejectStrings  []string
	}{
		{name: "nomic without prefixes", model: "nomic-embed-text:latest", wantHint: true, wantStrings: []string{`"search_query: "`, `"search_document: "`}},
		{name: "nomic under an hf.co path", model: "hf.co/nomic-ai/nomic-embed-text-v1.5-GGUF:Q8_0", wantHint: true, wantStrings: []string{`"search_query: "`}},
		{name: "nomic with both prefixes", model: "nomic-embed-text", queryPrefix: "search_query: ", documentPrefix: "search_document: "},
		{name: "nomic with only a query prefix", model: "nomic-embed-text", queryPrefix: "search_query: "},
		{name: "embeddinggemma without prefixes", model: "embeddinggemma-2:270m-bf16-text", wantHint: true, wantStrings: []string{`"task: search result | query: "`, `"title: {title} | text: "`}},
		{name: "mxbai without a query prefix", model: "mxbai-embed-large:335m", wantHint: true, wantStrings: []string{represent}},
		{name: "mxbai with a query prefix", model: "mxbai-embed-large", queryPrefix: represent},
		{name: "snowflake v1", model: "snowflake-arctic-embed:110m", wantHint: true, wantStrings: []string{represent}},
		{name: "snowflake v2 is not v1", model: "snowflake-arctic-embed2:568m", wantHint: true, wantStrings: []string{`"query: "`}, rejectStrings: []string{represent}},
		{name: "snowflake v2.0 hf.co build", model: "hf.co/Snowflake/snowflake-arctic-embed-l-v2.0-GGUF:Q8_0", wantHint: true, wantStrings: []string{`"query: "`}, rejectStrings: []string{represent}},
		{name: "case-insensitive match", model: "Nomic-Embed-Text", wantHint: true, wantStrings: []string{`"search_query: "`}},
		{name: "unknown model", model: "all-minilm"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			report := runDoctorWith(test, openDoctorStore(test), manifest.EmbeddingsSection{
				Provider: "ollama", Model: testCase.model, Dim: 768,
				QueryPrefix: testCase.queryPrefix, DocumentPrefix: testCase.documentPrefix,
			})

			hints := issuesOfKind(report, doctor.IssueEmbeddingPrefixHint)

			if !testCase.wantHint {
				if len(hints) != 0 {
					test.Errorf("unexpected hint: %+v", hints)
				}

				return
			}

			if len(hints) != 1 {
				test.Fatalf("got %d prefix hints, want 1: %+v", len(hints), report.Issues)
			}

			for _, want := range testCase.wantStrings {
				if !strings.Contains(hints[0].Message, want) {
					test.Errorf("hint should name %q: %s", want, hints[0].Message)
				}
			}

			for _, reject := range testCase.rejectStrings {
				if strings.Contains(hints[0].Message, reject) {
					test.Errorf("hint should not name %q: %s", reject, hints[0].Message)
				}
			}
		})
	}
}

func TestRun_NoEmbeddingPrefixHintWithoutProvider(test *testing.T) {
	report := runDoctorWith(test, openDoctorStore(test), manifest.EmbeddingsSection{Model: "nomic-embed-text"})

	if hints := issuesOfKind(report, doctor.IssueEmbeddingPrefixHint); len(hints) != 0 {
		test.Errorf("hint fired with [embeddings] unconfigured: %+v", hints)
	}
}
