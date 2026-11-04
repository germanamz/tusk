package subunit_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/subunit"
)

// contextManifest is referencesManifest with an embedding provider and the
// given sub-unit context settings.
func contextManifest(test *testing.T, mode string, properties ...string) *manifest.Manifest {
	test.Helper()

	loaded := referencesManifest(test)
	loaded.Embeddings = manifest.EmbeddingsSection{
		Provider:          "ollama",
		Model:             "nomic-embed-text",
		SubUnitContext:    mode,
		SubUnitProperties: properties,
	}

	return loaded
}

// applyAndDrain applies source's units for fileRow and empties the queue, so
// the next apply's enqueues are all a test sees.
func applyAndDrain(test *testing.T, sync *subunit.Sync, queue *index.EmbedQueueRepo, fileRow index.NodeRow, source string) {
	test.Helper()

	applySource(test, sync, fileRow, source)
	drainEmbedQueue(test, queue)
}

func applySource(test *testing.T, sync *subunit.Sync, fileRow index.NodeRow, source string) {
	test.Helper()

	units, parseErr := subunit.Parse([]byte(source))

	if parseErr != nil {
		test.Fatalf("Parse: %v", parseErr)
	}

	if _, applyErr := sync.ApplyFile(context.Background(), fileRow, units); applyErr != nil {
		test.Fatalf("ApplyFile: %v", applyErr)
	}
}

func queuedIDs(test *testing.T, queue *index.EmbedQueueRepo) []string {
	test.Helper()

	ids, listErr := queue.ListNodeIDs()

	if listErr != nil {
		test.Fatalf("ListNodeIDs: %v", listErr)
	}

	slices.Sort(ids)

	return ids
}

const contextSource = "# Service\n\nIntro paragraph.\n\n## Async\n\nConsumes the settled topic.\n"

// TestSync_TitleRenameReenqueuesLeaves pins #862: a leaf embeds its file's
// title, so renaming the file re-embeds every leaf even though no passage
// changed.
func TestSync_TitleRenameReenqueuesLeaves(test *testing.T) {
	store := openSyncTestIndex(test)
	sync, nodes, _, queue := newSync(store, contextManifest(test, ""))
	fileRow := seedFileRow(test, nodes, "svc", "svc.md")

	applyAndDrain(test, sync, queue, fileRow, contextSource)

	applySource(test, sync, fileRow, contextSource)

	if ids := queuedIDs(test, queue); len(ids) != 0 {
		test.Fatalf("unchanged re-apply enqueued %v, want nothing", ids)
	}

	fileRow.Title = "ledger-sync"
	applySource(test, sync, fileRow, contextSource)

	want := []string{"svc#S1.1P1", "svc#S1P1"}

	if ids := queuedIDs(test, queue); !slices.Equal(ids, want) {
		test.Errorf("title rename enqueued %v, want every leaf %v", ids, want)
	}
}

// TestSync_HeadingRenameReenqueuesOnlyUnderPath: a heading is in a leaf's
// context only under "path", so renaming one re-embeds the leaves beneath it
// there and nothing under "title".
func TestSync_HeadingRenameReenqueuesOnlyUnderPath(test *testing.T) {
	renamed := "# Service\n\nIntro paragraph.\n\n## Messaging\n\nConsumes the settled topic.\n"

	cases := []struct {
		mode string
		want []string
	}{
		{mode: manifest.SubUnitContextPath, want: []string{"svc#S1.1P1"}},
		{mode: manifest.SubUnitContextTitle, want: nil},
	}

	for _, testCase := range cases {
		test.Run(testCase.mode, func(test *testing.T) {
			store := openSyncTestIndex(test)
			sync, nodes, _, queue := newSync(store, contextManifest(test, testCase.mode))
			fileRow := seedFileRow(test, nodes, "svc", "svc.md")

			applyAndDrain(test, sync, queue, fileRow, contextSource)
			applySource(test, sync, fileRow, renamed)

			if ids := queuedIDs(test, queue); !slices.Equal(ids, testCase.want) {
				test.Errorf("heading rename enqueued %v, want %v", ids, testCase.want)
			}
		})
	}
}

// TestSync_NamedPropertyEditReenqueuesLeaves: a property named in
// sub-unit-properties is part of every leaf's context; another property isn't.
func TestSync_NamedPropertyEditReenqueuesLeaves(test *testing.T) {
	store := openSyncTestIndex(test)
	sync, nodes, _, queue := newSync(store, contextManifest(test, manifest.SubUnitContextNone, "aliases"))
	fileRow := seedFileRow(test, nodes, "svc", "svc.md")
	fileRow.PropertiesJSON = `{"aliases":["ls"],"owner":"payments"}`

	applyAndDrain(test, sync, queue, fileRow, contextSource)

	fileRow.PropertiesJSON = `{"aliases":["ls"],"owner":"billing"}`
	applySource(test, sync, fileRow, contextSource)

	if ids := queuedIDs(test, queue); len(ids) != 0 {
		test.Fatalf("editing an unnamed property enqueued %v, want nothing", ids)
	}

	fileRow.PropertiesJSON = `{"aliases":["ls","ledger"],"owner":"billing"}`
	applySource(test, sync, fileRow, contextSource)

	if ids := queuedIDs(test, queue); len(ids) != 2 {
		test.Errorf("editing a named property enqueued %v, want both leaves", ids)
	}
}

// TestSync_LeafHashIsBareWithoutContext: with no embedding provider, or with
// sub-unit-context = "none" and no properties, a leaf's content hash is
// sha256 of its payload, as before #862, so an existing index keeps matching.
func TestSync_LeafHashIsBareWithoutContext(test *testing.T) {
	noProvider := referencesManifest(test)

	cases := map[string]*manifest.Manifest{
		"no provider": noProvider,
		"none":        contextManifest(test, manifest.SubUnitContextNone),
	}

	for name, loaded := range cases {
		test.Run(name, func(test *testing.T) {
			store := openSyncTestIndex(test)
			sync, nodes, _, _ := newSync(store, loaded)
			fileRow := seedFileRow(test, nodes, "svc", "svc.md")

			applySource(test, sync, fileRow, contextSource)

			row, getErr := nodes.Get("svc#S1.1P1")

			if getErr != nil {
				test.Fatalf("Get: %v", getErr)
			}

			sum := sha256.Sum256([]byte("Consumes the settled topic."))
			want := hex.EncodeToString(sum[:])

			if row.ContentHash.String != want {
				test.Errorf("content_hash = %s, want the bare payload hash %s", row.ContentHash.String, want)
			}
		})
	}
}
