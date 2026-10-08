package embed_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
)

// formatFixture is one index with a single file node "notes/a" (type note,
// title "x", body "hi") queued for embedding.
type formatFixture struct {
	root       string
	nodes      *index.NodeRepo
	queue      *index.EmbedQueueRepo
	embeddings *index.EmbeddingRepo
}

func newFormatFixture(test *testing.T) formatFixture {
	test.Helper()

	root := test.TempDir()
	store := openIndex(test, root)

	test.Cleanup(func() { _ = store.Close() })

	fixture := formatFixture{
		root:       root,
		nodes:      index.NewNodeRepo(store),
		queue:      index.NewEmbedQueueRepo(store),
		embeddings: index.NewEmbeddingRepo(store),
	}

	createNodeFile(test, root, "notes/a.md", "hi")

	if upsertErr := fixture.nodes.Upsert(index.NodeRow{ID: "notes/a", Type: "note", Path: "notes/a.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert: %v", upsertErr)
	}

	fixture.enqueue(test, "notes/a")

	return fixture
}

func (fixture formatFixture) enqueue(test *testing.T, nodeID string) {
	test.Helper()

	if enqErr := fixture.queue.Enqueue(nodeID); enqErr != nil {
		test.Fatalf("enqueue %s: %v", nodeID, enqErr)
	}
}

func (fixture formatFixture) drain(test *testing.T, embedder embed.Embedder) {
	test.Helper()

	if _, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       fixture.root,
		Nodes:      fixture.nodes,
		Queue:      fixture.queue,
		Embeddings: fixture.embeddings,
		Embedder:   embedder,
		Chunker:    embed.WholeDocument{},
		BatchSize:  50,
	}); drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}
}

func (fixture formatFixture) onlyRow(test *testing.T, nodeID string) index.EmbeddingRow {
	test.Helper()

	rows, getErr := fixture.embeddings.GetByNodeID(nodeID)

	if getErr != nil {
		test.Fatalf("GetByNodeID: %v", getErr)
	}

	if len(rows) != 1 {
		test.Fatalf("%s has %d embedding rows, want 1", nodeID, len(rows))
	}

	return rows[0]
}

func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))

	return hex.EncodeToString(sum[:])
}

func assertSent(test *testing.T, recorder *recordingEmbedder, want ...string) {
	test.Helper()

	if len(recorder.payloads) != len(want) {
		test.Fatalf("embedder received %d texts %q, want %d %q", len(recorder.payloads), recorder.payloads, len(want), want)
	}

	for position, payload := range recorder.payloads {
		if string(payload) != want[position] {
			test.Errorf("text %d = %q, want %q", position, payload, want[position])
		}
	}
}

// TestDrainQueue_DefaultFormatKeepsStoredHashes pins the upgrade guarantee:
// with every new [embeddings] key at its default, the drain sends and hashes
// exactly the header+chunk bytes it always has, so hashes already stored in an
// existing index still match and nothing re-embeds.
func TestDrainQueue_DefaultFormatKeepsStoredHashes(test *testing.T) {
	fixture := newFormatFixture(test)
	recorder := &recordingEmbedder{dim: 3, model: "stub"}

	fixture.drain(test, recorder)

	const sent = "[type] note\n[title] x\n---\nhi\n"

	assertSent(test, recorder, sent)

	row := fixture.onlyRow(test, "notes/a")

	if row.ContentHash != sha256Hex(sent) {
		test.Errorf("ContentHash = %s, want sha256 of the header+chunk bytes", row.ContentHash)
	}

	if row.Body != "hi\n" {
		test.Errorf("Body = %q, want the chunk only", row.Body)
	}
}

// TestDrainQueue_DefaultFormatSubUnitHashMatchesNodeHash pins the upgrade
// guarantee for sub-units, which hold most stored vectors: with default
// settings a leaf's vector hash is sha256 of its payload, the same value as
// its nodes.content_hash and as the hash stored before these settings existed.
func TestDrainQueue_DefaultFormatSubUnitHashMatchesNodeHash(test *testing.T) {
	fixture := newFormatFixture(test)

	if upsertErr := fixture.nodes.BulkUpsert([]index.NodeRow{{
		ID: "notes/a#p1", Type: "paragraph", Path: "notes/a.md", PropertiesJSON: "{}", LastChecksum: "x",
		ParentID:     sql.NullString{String: "notes/a", Valid: true},
		Ordinal:      sql.NullInt64{Int64: 0, Valid: true},
		EmbedPayload: sql.NullString{String: "leaf text", Valid: true},
	}}, "markdown"); upsertErr != nil {
		test.Fatalf("upsert sub-unit: %v", upsertErr)
	}

	fixture.enqueue(test, "notes/a#p1")
	fixture.drain(test, &recordingEmbedder{dim: 3, model: "stub"})

	if row := fixture.onlyRow(test, "notes/a#p1"); row.ContentHash != sha256Hex("leaf text") {
		test.Errorf("sub-unit ContentHash = %s, want sha256 of the payload", row.ContentHash)
	}
}

func TestDrainQueue_DocumentPrefixIsSentAndHashed(test *testing.T) {
	fixture := newFormatFixture(test)
	recorder := &recordingEmbedder{dim: 3, model: "stub", format: embed.Format{DocumentPrefix: "search_document: "}}

	fixture.drain(test, recorder)

	const sent = "search_document: [type] note\n[title] x\n---\nhi\n"

	assertSent(test, recorder, sent)

	row := fixture.onlyRow(test, "notes/a")

	if row.ContentHash != sha256Hex(sent) {
		test.Errorf("ContentHash = %s, want sha256 of the prefixed text", row.ContentHash)
	}

	if row.Body != "hi\n" {
		test.Errorf("Body = %q, want the chunk without prefix or header", row.Body)
	}
}

// TestDrainQueue_PrefixChangeReembeds is the no-mixing guarantee: vectors
// embedded without a prefix are not reused once one is configured.
func TestDrainQueue_PrefixChangeReembeds(test *testing.T) {
	fixture := newFormatFixture(test)

	fixture.drain(test, &recordingEmbedder{dim: 3, model: "stub"})
	fixture.enqueue(test, "notes/a")

	prefixed := &recordingEmbedder{dim: 3, model: "stub", format: embed.Format{DocumentPrefix: "search_document: "}}

	fixture.drain(test, prefixed)

	assertSent(test, prefixed, "search_document: [type] note\n[title] x\n---\nhi\n")
}

func TestDrainQueue_HeaderModeNoneWithTitlePrefix(test *testing.T) {
	fixture := newFormatFixture(test)
	recorder := &recordingEmbedder{dim: 3, model: "stub", format: embed.Format{DocumentPrefix: "title: {title} | text: ", HeaderMode: embed.HeaderNone}}

	fixture.drain(test, recorder)

	assertSent(test, recorder, "title: x | text: hi\n")
}

// TestDrainQueue_TitleEditReembedsUnderTitlePrefix: with no header, the title
// reaches the model only through {title}. Editing it must still re-embed,
// which works because the hash covers the rendered prefix.
func TestDrainQueue_TitleEditReembedsUnderTitlePrefix(test *testing.T) {
	fixture := newFormatFixture(test)
	recorder := &recordingEmbedder{dim: 3, model: "stub", format: embed.Format{DocumentPrefix: "title: {title} | text: ", HeaderMode: embed.HeaderNone}}

	fixture.drain(test, recorder)

	if writeErr := os.WriteFile(filepath.Join(fixture.root, "notes/a.md"), []byte("---\ntype: note\ntitle: y\n---\n\nhi\n"), 0o644); writeErr != nil {
		test.Fatalf("rewrite: %v", writeErr)
	}

	fixture.enqueue(test, "notes/a")
	fixture.drain(test, recorder)

	assertSent(test, recorder, "title: x | text: hi\n", "title: y | text: hi\n")
}

func TestDrainQueue_SubUnitTitlePlaceholderRendersNone(test *testing.T) {
	fixture := newFormatFixture(test)

	if upsertErr := fixture.nodes.BulkUpsert([]index.NodeRow{{
		ID: "notes/a#p1", Type: "paragraph", Path: "notes/a.md", PropertiesJSON: "{}", LastChecksum: "x",
		ParentID:     sql.NullString{String: "notes/a", Valid: true},
		Ordinal:      sql.NullInt64{Int64: 0, Valid: true},
		EmbedPayload: sql.NullString{String: "leaf text", Valid: true},
	}}, "markdown"); upsertErr != nil {
		test.Fatalf("upsert sub-unit: %v", upsertErr)
	}

	// Drain the file first so the recorder sees only the sub-unit below.
	fixture.drain(test, &recordingEmbedder{dim: 3, model: "stub"})
	fixture.enqueue(test, "notes/a#p1")

	recorder := &recordingEmbedder{dim: 3, model: "stub", format: embed.Format{DocumentPrefix: "title: {title} | text: "}}

	fixture.drain(test, recorder)

	assertSent(test, recorder, "title: none | text: leaf text")

	if row := fixture.onlyRow(test, "notes/a#p1"); row.Body != "leaf text" {
		test.Errorf("Body = %q, want the payload without the prefix", row.Body)
	}
}

// TestDrainQueue_StoresAndReusesUnderVectorKey: vectors are keyed on the
// vector key, not the display model, so a num-ctx change (same model, same
// text) neither reuses the old vector nor stores the new one under the bare
// model name.
func TestDrainQueue_StoresAndReusesUnderVectorKey(test *testing.T) {
	fixture := newFormatFixture(test)

	fixture.drain(test, &recordingEmbedder{dim: 3, model: "m"})
	fixture.enqueue(test, "notes/a")

	keyed := &recordingEmbedder{dim: 3, model: "m", vectorKey: "m#num-ctx=512"}

	fixture.drain(test, keyed)

	assertSent(test, keyed, "[type] note\n[title] x\n---\nhi\n")

	if row := fixture.onlyRow(test, "notes/a"); row.Model != "m#num-ctx=512" {
		test.Errorf("stored Model = %q, want the vector key", row.Model)
	}
}

// hookEmbedder runs onEmbed during each Embed call, so a test can change
// state while an embed is in flight (a reload landing mid-request).
type hookEmbedder struct {
	recordingEmbedder
	onEmbed func()
}

func (stub *hookEmbedder) Embed(ctx context.Context, text []byte) ([]float32, error) {
	if stub.onEmbed != nil {
		stub.onEmbed()
	}

	return stub.recordingEmbedder.Embed(ctx, text)
}

// drainGuarded drains with the settings guard on and returns the drained
// count plus whether the drain stopped on superseded settings.
func (fixture formatFixture) drainGuarded(test *testing.T, embedder embed.Embedder, meta *index.MetaRepo, fingerprint string) (int, bool) {
	test.Helper()

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:        fixture.root,
		Nodes:       fixture.nodes,
		Queue:       fixture.queue,
		Embeddings:  fixture.embeddings,
		Embedder:    embedder,
		Chunker:     embed.WholeDocument{},
		BatchSize:   50,
		Meta:        meta,
		Fingerprint: fingerprint,
	})

	superseded := errors.Is(drainErr, embed.ErrSettingsSuperseded)

	if drainErr != nil && !superseded {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	return drained, superseded
}

func (fixture formatFixture) metaRepo(test *testing.T) *index.MetaRepo {
	test.Helper()

	store := openIndex(test, fixture.root)

	test.Cleanup(func() { _ = store.Close() })

	return index.NewMetaRepo(store)
}

func (fixture formatFixture) claimableBy(test *testing.T, workerID string) int {
	test.Helper()

	claimed, claimErr := fixture.queue.DrainEmbed(workerID, 50, time.Minute)

	if claimErr != nil {
		test.Fatalf("DrainEmbed: %v", claimErr)
	}

	return len(claimed)
}

// TestDrainQueue_StaleSettingsLeaveRowsQueued: once reindex records new
// settings, a drainer still holding the old ones must neither embed nor
// skip-ack anything. Here its old-format hash matches the stored vector, so
// without the guard it would ack the row and the node would never re-embed.
func TestDrainQueue_StaleSettingsLeaveRowsQueued(test *testing.T) {
	fixture := newFormatFixture(test)
	meta := fixture.metaRepo(test)

	fixture.drain(test, &recordingEmbedder{dim: 3, model: "stub"})
	fixture.enqueue(test, "notes/a")

	if setErr := meta.Set(embed.DocumentFingerprintKey, "new-settings"); setErr != nil {
		test.Fatalf("stamp: %v", setErr)
	}

	stale := &recordingEmbedder{dim: 3, model: "stub"}

	drained, superseded := fixture.drainGuarded(test, stale, meta, "old-settings")

	if drained != 0 {
		test.Errorf("stale drainer drained %d rows, want 0", drained)
	}

	if !superseded {
		test.Errorf("stale drain did not report ErrSettingsSuperseded")
	}

	assertSent(test, stale)

	if claimable := fixture.claimableBy(test, "up-to-date"); claimable != 1 {
		test.Errorf("an up-to-date drainer can claim %d rows, want 1 (the stale drainer must release, not ack)", claimable)
	}
}

// TestDrainQueue_SettingsChangeMidEmbedWritesNothing: settings recorded while
// an embed call is in flight must stop that node's write; the row goes back
// to the queue unleased.
func TestDrainQueue_SettingsChangeMidEmbedWritesNothing(test *testing.T) {
	fixture := newFormatFixture(test)
	meta := fixture.metaRepo(test)

	if setErr := meta.Set(embed.DocumentFingerprintKey, "old-settings"); setErr != nil {
		test.Fatalf("stamp: %v", setErr)
	}

	slow := &hookEmbedder{
		recordingEmbedder: recordingEmbedder{dim: 3, model: "stub"},
		onEmbed: func() {
			_ = meta.Set(embed.DocumentFingerprintKey, "new-settings")
		},
	}

	if _, superseded := fixture.drainGuarded(test, slow, meta, "old-settings"); !superseded {
		test.Errorf("drain did not report ErrSettingsSuperseded after the mid-embed change")
	}

	if rows, _ := fixture.embeddings.GetByNodeID("notes/a"); len(rows) != 0 {
		test.Errorf("stored %d vectors embedded under superseded settings, want 0", len(rows))
	}

	if claimable := fixture.claimableBy(test, "up-to-date"); claimable != 1 {
		test.Errorf("an up-to-date drainer can claim %d rows, want 1", claimable)
	}
}

func TestDrainQueue_MatchingSettingsDrain(test *testing.T) {
	fixture := newFormatFixture(test)
	meta := fixture.metaRepo(test)

	if setErr := meta.Set(embed.DocumentFingerprintKey, "current"); setErr != nil {
		test.Fatalf("stamp: %v", setErr)
	}

	recorder := &recordingEmbedder{dim: 3, model: "stub"}

	if drained, superseded := fixture.drainGuarded(test, recorder, meta, "current"); drained != 1 || superseded {
		test.Errorf("drained = %d, superseded = %v; want 1, false", drained, superseded)
	}

	assertSent(test, recorder, "[type] note\n[title] x\n---\nhi\n")
}
