package reindex_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
)

const embedFingerprintKey = embed.DocumentFingerprintKey

// settingsEmbedder stands in for the Ollama embedder NewFromManifest builds:
// its vector key and format come from an [embeddings] section, and it counts
// Embed calls so a test can tell "re-embedded" from "skip-acked".
type settingsEmbedder struct {
	section manifest.EmbeddingsSection
	calls   atomic.Int64
}

func (stub *settingsEmbedder) Embed(_ context.Context, _ []byte) ([]float32, error) {
	stub.calls.Add(1)

	return []float32{0.1, 0.2, 0.3}, nil
}

func (stub *settingsEmbedder) Model() string     { return stub.section.Model }
func (stub *settingsEmbedder) VectorKey() string { return embed.VectorKeyFor(stub.section) }
func (stub *settingsEmbedder) Dim() int          { return 3 }

func (stub *settingsEmbedder) Format() embed.Format {
	return embed.Format{
		QueryPrefix:    stub.section.QueryPrefix,
		DocumentPrefix: stub.section.DocumentPrefix,
		HeaderMode:     embed.HeaderMode(stub.section.ResolvedDocumentHeader()),
	}
}

// settingsVault is a one-file vault (a section heading and one paragraph, so
// two embeddable nodes: the file and its paragraph leaf) plus the repos a
// reindex pass with embeddings needs.
type settingsVault struct {
	root       string
	store      *index.Index
	queue      *index.EmbedQueueRepo
	embeddings *index.EmbeddingRepo
	meta       *index.MetaRepo
}

// embeddableNodes is the number of nodes in the settings vault the drain
// embeds: the file row and its one paragraph leaf (the section is not embedded).
const embeddableNodes = 2

func newSettingsVault(test *testing.T) settingsVault {
	test.Helper()

	root := test.TempDir()

	writeNode(test, root, "a.md", "type: note\ntitle: A\n", "# Heading\n\nFirst paragraph.\n")

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	test.Cleanup(func() { _ = store.Close() })

	return settingsVault{
		root:       root,
		store:      store,
		queue:      index.NewEmbedQueueRepo(store),
		embeddings: index.NewEmbeddingRepo(store),
		meta:       index.NewMetaRepo(store),
	}
}

// run writes section to tusk.toml (a process normally runs under the manifest
// on disk), reindexes the vault under it, and returns how many texts were
// embedded. async mirrors the MCP daemon's walk: no in-process drain of
// either queue (the daemon leaves Workers unset; -1 survives withGen's
// default).
func (vault settingsVault) run(test *testing.T, section manifest.EmbeddingsSection, async bool) int64 {
	test.Helper()

	vault.writeManifest(test, section)

	return vault.runInMemory(test, section, async).calls
}

// writeManifest writes a tusk.toml carrying section.
func (vault settingsVault) writeManifest(test *testing.T, section manifest.EmbeddingsSection) {
	test.Helper()

	var buffer bytes.Buffer

	encodeErr := toml.NewEncoder(&buffer).Encode(map[string]any{
		"workspace":  map[string]any{"name": "settings"},
		"embeddings": section,
	})

	if encodeErr != nil {
		test.Fatalf("encode tusk.toml: %v", encodeErr)
	}

	if writeErr := os.WriteFile(filepath.Join(vault.root, "tusk.toml"), buffer.Bytes(), 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}
}

type passResult struct {
	calls  int64
	report *reindex.Report
}

// runInMemory reindexes under section without touching tusk.toml, modeling a
// long-lived process (an MCP daemon) whose loaded manifest may lag the file.
func (vault settingsVault) runInMemory(test *testing.T, section manifest.EmbeddingsSection, async bool) passResult {
	test.Helper()

	embedder := &settingsEmbedder{section: section}
	target, maxBytes, overlap := section.ChunkSizes()
	workers := 0

	if async {
		workers = -1
	}

	report, runErr := reindex.Run(withGen(vault.store, reindex.Config{
		Root:          vault.root,
		Repo:          index.NewNodeRepo(vault.store),
		Edges:         index.NewEdgeRepo(vault.store),
		Manifest:      &manifest.Manifest{Embeddings: section},
		EmbedQueue:    vault.queue,
		EmbeddingRepo: vault.embeddings,
		Embedder:      embedder,
		Chunker:       embed.MarkdownRecursive{TargetBytes: target, MaxBytes: maxBytes, OverlapBytes: overlap},
		Meta:          vault.meta,
		Workers:       workers,
		Async:         async,
	}))

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	return passResult{calls: embedder.calls.Load(), report: report}
}

// forgetFingerprint simulates an index written by a binary that predates the
// fingerprint: the meta key is absent (Get returns "").
func (vault settingsVault) forgetFingerprint(test *testing.T) {
	test.Helper()

	if setErr := vault.meta.Set(embedFingerprintKey, ""); setErr != nil {
		test.Fatalf("clear fingerprint: %v", setErr)
	}
}

func nomic() manifest.EmbeddingsSection {
	return manifest.EmbeddingsSection{Provider: "ollama", Model: "nomic-embed-text", Endpoint: "http://localhost:11434", Dim: 3}
}

func TestRun_DocumentSettingChangeReembedsUnchangedFiles(test *testing.T) {
	vault := newSettingsVault(test)

	if calls := vault.run(test, nomic(), false); calls != embeddableNodes {
		test.Fatalf("first pass embedded %d texts, want %d", calls, embeddableNodes)
	}

	if calls := vault.run(test, nomic(), false); calls != 0 {
		test.Fatalf("unchanged settings re-embedded %d texts, want 0", calls)
	}

	prefixed := nomic()
	prefixed.DocumentPrefix = "search_document: "

	// No file changed, so only the settings change can enqueue this work.
	if calls := vault.run(test, prefixed, false); calls != embeddableNodes {
		test.Errorf("document-prefix change re-embedded %d texts, want %d", calls, embeddableNodes)
	}

	stored, _ := vault.meta.Get(embedFingerprintKey)

	if stored != embed.DocumentFingerprint(prefixed) {
		test.Errorf("stored fingerprint = %q, want the new settings' fingerprint", stored)
	}

	if calls := vault.run(test, prefixed, false); calls != 0 {
		test.Errorf("the pass after the change re-embedded %d texts, want 0 (the change is recorded)", calls)
	}
}

func TestRun_QueryPrefixChangeReembedsNothing(test *testing.T) {
	vault := newSettingsVault(test)

	vault.run(test, nomic(), false)

	queryPrefixed := nomic()
	queryPrefixed.QueryPrefix = "search_query: "

	if calls := vault.run(test, queryPrefixed, false); calls != 0 {
		test.Errorf("query-prefix change re-embedded %d texts, want 0", calls)
	}
}

// TestRun_UpgradeWithDefaultsReembedsNothing: a vault indexed before the
// fingerprint existed, upgraded with every new key at its default, records
// the fingerprint without re-embedding.
func TestRun_UpgradeWithDefaultsReembedsNothing(test *testing.T) {
	vault := newSettingsVault(test)

	vault.run(test, nomic(), false)
	vault.forgetFingerprint(test)

	if depth := enqueueAfterWalk(test, vault, nomic()); depth != 0 {
		test.Errorf("upgrade with default settings enqueued %d embed jobs, want 0", depth)
	}

	if stored, _ := vault.meta.Get(embedFingerprintKey); stored != embed.DocumentFingerprint(nomic()) {
		test.Errorf("upgrade pass did not record the fingerprint (got %q)", stored)
	}
}

// TestRun_UpgradeWithPrefixReembeds: upgrading and setting a prefix in the
// same edit must re-embed, even though no fingerprint was stored to compare.
func TestRun_UpgradeWithPrefixReembeds(test *testing.T) {
	vault := newSettingsVault(test)

	vault.run(test, nomic(), false)
	vault.forgetFingerprint(test)

	prefixed := nomic()
	prefixed.DocumentPrefix = "search_document: "

	if calls := vault.run(test, prefixed, false); calls != embeddableNodes {
		test.Errorf("upgrade with a new prefix re-embedded %d texts, want %d", calls, embeddableNodes)
	}
}

// TestRun_UpgradeWithStaleModelReembeds: with no fingerprint stored and
// vectors left under an older model, the pass converges them even though
// every new key is at its default.
func TestRun_UpgradeWithStaleModelReembeds(test *testing.T) {
	vault := newSettingsVault(test)

	older := nomic()
	older.Model = "all-minilm"

	vault.run(test, older, false)
	vault.forgetFingerprint(test)

	if calls := vault.run(test, nomic(), false); calls != embeddableNodes {
		test.Errorf("upgrade over stale-model vectors re-embedded %d texts, want %d", calls, embeddableNodes)
	}
}

// TestRun_AsyncSettingChangeEnqueuesEveryEmbeddableNode covers the MCP
// daemon: its walk is async and its background drainer never forces, so the
// settings change must enqueue the embed work directly, sub-unit leaves
// included.
func TestRun_AsyncSettingChangeEnqueuesEveryEmbeddableNode(test *testing.T) {
	vault := newSettingsVault(test)

	vault.run(test, nomic(), false)

	prefixed := nomic()
	prefixed.DocumentPrefix = "search_document: "

	vault.run(test, prefixed, true)

	queued, listErr := vault.queue.ListNodeIDs()

	if listErr != nil {
		test.Fatalf("ListNodeIDs: %v", listErr)
	}

	sort.Strings(queued)

	leaves, _ := index.NewNodeRepo(vault.store).ListSubUnitsForFile("a")

	var want []string

	want = append(want, "a")

	for _, leaf := range leaves {
		if leaf.Type != "section" {
			want = append(want, leaf.ID)
		}
	}

	sort.Strings(want)

	if len(want) != embeddableNodes {
		test.Fatalf("setup: expected %d embeddable nodes, found %v", embeddableNodes, want)
	}

	if len(queued) != len(want) {
		test.Fatalf("queued = %v, want %v", queued, want)
	}

	for position, nodeID := range want {
		if queued[position] != nodeID {
			test.Errorf("queued = %v, want %v", queued, want)

			break
		}
	}
}

// enqueueAfterWalk runs an async pass (no drain) and returns how many embed
// jobs it left queued.
func enqueueAfterWalk(test *testing.T, vault settingsVault, section manifest.EmbeddingsSection) int {
	test.Helper()

	vault.run(test, section, true)

	depth, depthErr := vault.queue.DepthByKind("embed")

	if depthErr != nil {
		test.Fatalf("DepthByKind: %v", depthErr)
	}

	return depth
}

// TestRun_StaleProcessDoesNotRevertSettings: a daemon still holding the old
// [embeddings] settings, after a CLI reindex recorded the new ones from the
// edited tusk.toml, must neither re-queue the vault under its stale settings
// nor overwrite the recorded fingerprint.
func TestRun_StaleProcessDoesNotRevertSettings(test *testing.T) {
	vault := newSettingsVault(test)

	vault.run(test, nomic(), false)

	prefixed := nomic()
	prefixed.DocumentPrefix = "search_document: "

	vault.run(test, prefixed, false)

	stale := vault.runInMemory(test, nomic(), true)

	if depth, _ := vault.queue.DepthByKind("embed"); depth != 0 {
		test.Errorf("stale process queued %d embed jobs, want 0", depth)
	}

	if stored, _ := vault.meta.Get(embedFingerprintKey); stored != embed.DocumentFingerprint(prefixed) {
		test.Errorf("stale process overwrote the recorded fingerprint")
	}

	if stale.report.EmbedSettingsRequeued != 0 {
		test.Errorf("EmbedSettingsRequeued = %d for a stale process, want 0", stale.report.EmbedSettingsRequeued)
	}
}

func TestRun_ReportsSettingsRequeue(test *testing.T) {
	vault := newSettingsVault(test)

	vault.run(test, nomic(), false)

	prefixed := nomic()
	prefixed.DocumentPrefix = "search_document: "
	vault.writeManifest(test, prefixed)

	if result := vault.runInMemory(test, prefixed, false); result.report.EmbedSettingsRequeued != embeddableNodes {
		test.Errorf("EmbedSettingsRequeued = %d, want %d", result.report.EmbedSettingsRequeued, embeddableNodes)
	}
}

// TestRun_StaleProcessSyncPassSucceedsAndLeavesQueue: a stale process's own
// end-of-pass drain stops on the settings guard. That is not a reindex
// failure, and the queued row stays for an up-to-date process.
func TestRun_StaleProcessSyncPassSucceedsAndLeavesQueue(test *testing.T) {
	vault := newSettingsVault(test)

	vault.run(test, nomic(), false)

	prefixed := nomic()
	prefixed.DocumentPrefix = "search_document: "

	vault.run(test, prefixed, false)

	if enqErr := vault.queue.Enqueue("a"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	if stale := vault.runInMemory(test, nomic(), false); stale.calls != 0 {
		test.Errorf("stale process embedded %d texts, want 0", stale.calls)
	}

	if depth, _ := vault.queue.DepthByKind("embed"); depth != 1 {
		test.Errorf("embed queue depth = %d, want 1 (left for an up-to-date process)", depth)
	}
}
