package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
)

// These tests pin issue #681: the CLI watcher must run the full reindex pass on
// every filesystem event and drain the embedding queue, matching `tusk reindex`
// and the MCP watcher (internal/mcp/watch.go). Before the fix, cmd_watch.go
// hand-rolled a handler that (1) never wired an embedder, (2) swallowed
// directory-level and rename-out events behind a stat/IsDir gate, (3) deleted
// node rows on delete without tombstoning file_state, and (4) never re-healed
// ref drift on deletes.

// syncBuffer is a goroutine-safe io.Writer used to capture the watch command's
// stdout while it runs on a background goroutine (the test reads it to learn
// when the watcher is live). The mutex keeps `go test -race` clean.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (sb *syncBuffer) Write(p []byte) (int, error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	return sb.buf.Write(p)
}

func (sb *syncBuffer) String() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	return sb.buf.String()
}

// runWatchBackground starts `tusk watch` in wsDir on a background goroutine and
// returns a stop func (cancel + wait) that the caller must defer. It blocks
// until the command prints its "Watching for changes" banner (initial reindex
// done) plus a short grace window so the fsnotify watch set is registered before
// the caller drives events.
func runWatchBackground(test *testing.T, wsDir string) func() {
	test.Helper()

	chdir(test, wsDir)

	ctx, cancel := context.WithCancel(context.Background())

	out := &syncBuffer{}
	done := make(chan struct{})

	rootCmd := newRootCmd()
	rootCmd.SetOut(out)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetContext(ctx)
	rootCmd.SetArgs([]string{"watch"})

	go func() {
		_ = rootCmd.Execute()

		close(done)
	}()

	deadline := time.Now().Add(15 * time.Second)

	for !strings.Contains(out.String(), "Watching for changes") {
		if time.Now().After(deadline) {
			cancel()
			<-done
			test.Fatalf("watch did not reach the watching banner; output:\n%s", out.String())
		}

		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(500 * time.Millisecond) // let fsnotify register the watch set

	var once sync.Once

	return func() {
		once.Do(func() {
			cancel()

			select {
			case <-done:
			case <-time.After(10 * time.Second):
				test.Errorf("watch did not exit within 10s of cancel")
			}
		})
	}
}

// watchTestNodeExists opens a short-lived read handle on the index and reports
// whether nodeID has a row. WAL + busy_timeout make this safe to poll while the
// watch goroutine holds its own handle open.
func watchTestNodeExists(idxPath, nodeID string) bool {
	idx, openErr := index.Open(idxPath)

	if openErr != nil {
		return false
	}

	defer idx.Close()

	_, getErr := index.NewNodeRepo(idx).Get(nodeID)

	return getErr == nil
}

// waitForWatch polls cond every 100ms until it returns true or the deadline
// elapses, failing the test on timeout with msg.
func waitForWatch(test *testing.T, timeout time.Duration, msg string, cond func() bool) {
	test.Helper()

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	test.Fatalf("timed out after %s: %s", timeout, msg)
}

const watchNoteManifest = `[workspace]
name = "t"

[node-types.note]
properties = []
`

// TestWatchCmd_DrainsEmbedQueueOnInitialReindex pins finding #1: `tusk watch`
// must drain the embedding queue (its --help promises it, and workers=0 refuses
// to start "because the watcher needs a drainer"). Before the fix, cmd_watch.go
// built a reindex config without an Embedder, so reindex.Run's drain never fired
// and semantic search stayed blind to watched content.
func TestWatchCmd_DrainsEmbedQueueOnInitialReindex(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{"embedding": []float64{1, 0, 0}})
	}))

	defer server.Close()

	wsDir := test.TempDir()

	manifestBody := `[workspace]
name = "t"

[embeddings]
provider = "ollama"
model = "test-model"
endpoint = "` + server.URL + `"
dim = 3

[node-types.note]
properties = []
`

	writeWatchTestFile(test, wsDir, "tusk.toml", manifestBody)
	writeWatchTestFile(test, wsDir, "notes/base.md", "---\ntype: note\n---\n# Base\n\nA plain note about calendars and scheduling meetings.\n")

	runWatchOnce(test, wsDir)

	idx, idxErr := index.Open(filepath.Join(wsDir, ".tusk", "index.db"))

	if idxErr != nil {
		test.Fatalf("open index: %v", idxErr)
	}

	defer idx.Close()

	embeddings, getErr := index.NewEmbeddingRepo(idx).GetByNodeID("notes/base")

	if getErr != nil {
		test.Fatalf("GetByNodeID: %v", getErr)
	}

	if len(embeddings) == 0 {
		test.Errorf("expected notes/base to have embeddings after the watch initial reindex; the embed queue was not drained")
	}

	depth, depthErr := index.NewEmbedQueueRepo(idx).DepthByKind("embed")

	if depthErr != nil {
		test.Fatalf("DepthByKind: %v", depthErr)
	}

	if depth != 0 {
		test.Errorf("embed queue depth = %d, want 0 (watch must drain the embedding queue)", depth)
	}
}

// runWatchOnceErr is runWatchOnce but surfaces the command's exit error, so a
// test can assert `tusk watch` started (returned nil on the pre-cancelled ctx)
// rather than aborting during the initial reindex.
func runWatchOnceErr(test *testing.T, wsDir string) error {
	test.Helper()

	chdir(test, wsDir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rootCmd := newRootCmd()
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetContext(ctx)
	rootCmd.SetArgs([]string{"watch"})

	return rootCmd.Execute()
}

// TestWatchCmd_StartsWhenEmbedBackendUnreachable guards the resilience fix that
// accompanies finding #1: wiring the embedder into `tusk watch` must NOT turn a
// down embedding backend into a hard startup dependency. The initial reindex
// still builds the node/edge index; only the embedding drain fails (transport
// error), and the watcher must start anyway so it keeps the index live and
// drains embeddings once the backend returns.
func TestWatchCmd_StartsWhenEmbedBackendUnreachable(test *testing.T) {
	// A server we immediately close: its port is now refused, so Embed() gets a
	// connection error (a *TransportError), fast and deterministically.
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := server.URL
	server.Close()

	wsDir := test.TempDir()

	manifestBody := `[workspace]
name = "t"

[embeddings]
provider = "ollama"
model = "test-model"
endpoint = "` + endpoint + `"
dim = 3

[node-types.note]
properties = []
`

	writeWatchTestFile(test, wsDir, "tusk.toml", manifestBody)
	writeWatchTestFile(test, wsDir, "notes/base.md", "---\ntype: note\n---\n# Base\n\nbody\n")

	if startErr := runWatchOnceErr(test, wsDir); startErr != nil {
		test.Fatalf("watch must start despite an unreachable embed backend; got: %v", startErr)
	}

	// The node index must still have been built — only embeddings were skipped.
	idx, idxErr := index.Open(filepath.Join(wsDir, ".tusk", "index.db"))

	if idxErr != nil {
		test.Fatalf("open index: %v", idxErr)
	}

	defer idx.Close()

	if _, getErr := index.NewNodeRepo(idx).Get("notes/base"); getErr != nil {
		test.Errorf("node notes/base must be indexed even when the embed backend is down: %v", getErr)
	}
}

// TestWatchCmd_IndexesDirMoveInAndReapsMoveOut pins finding #2: moving a
// directory of notes into the workspace must index its files, and moving a file
// out must reap its node. Before the fix, the stat/IsDir gate swallowed the
// single directory-create event (children get no fsnotify events) and the
// rename-out event (os.Stat fails), so neither converged.
func TestWatchCmd_IndexesDirMoveInAndReapsMoveOut(test *testing.T) {
	wsDir := test.TempDir()
	stagingDir := test.TempDir() // outside the workspace: a whole dir to move in
	outboxDir := test.TempDir()  // outside the workspace: where a file moves out to

	writeWatchTestFile(test, wsDir, "tusk.toml", watchNoteManifest)
	writeWatchTestFile(test, wsDir, "notes/a.md", "---\ntype: note\n---\n# A\n\nbody\n")

	if writeErr := os.WriteFile(filepath.Join(stagingDir, "inbound1.md"), []byte("---\ntype: note\n---\n# One\n\nbody\n"), 0o644); writeErr != nil {
		test.Fatalf("write staging file: %v", writeErr)
	}

	stop := runWatchBackground(test, wsDir)
	defer stop()

	// (a) move a directory of notes INTO the workspace (single CREATE for the dir)
	if renameErr := os.Rename(stagingDir, filepath.Join(wsDir, "notes2")); renameErr != nil {
		test.Fatalf("move dir in: %v", renameErr)
	}

	// (b) move a file OUT of the workspace (RENAME on the vanished source path)
	if renameErr := os.Rename(filepath.Join(wsDir, "notes/a.md"), filepath.Join(outboxDir, "a.md")); renameErr != nil {
		test.Fatalf("move file out: %v", renameErr)
	}

	idxPath := filepath.Join(wsDir, ".tusk", "index.db")

	waitForWatch(test, 15*time.Second, "moved-in file must index and moved-out node must be reaped", func() bool {
		return watchTestNodeExists(idxPath, "notes2/inbound1") && !watchTestNodeExists(idxPath, "notes/a")
	})
}

// TestWatchCmd_ReindexesRestoredFileAfterDelete pins finding #3: a delete must
// tombstone the file_state row (as reindex.Run's reap does) so a
// delete-then-restore that preserves mtime+size re-indexes. Before the fix, the
// delete handler only deleted the node row and left file_state "live", so the
// restore hit reindex.Run's incremental mtime+size skip forever.
func TestWatchCmd_ReindexesRestoredFileAfterDelete(test *testing.T) {
	wsDir := test.TempDir()

	body := []byte("---\ntype: note\n---\n# Keeper\n\nBody.\n")

	writeWatchTestFile(test, wsDir, "tusk.toml", watchNoteManifest)
	writeWatchTestFile(test, wsDir, "notes/keeper.md", string(body))

	keeperPath := filepath.Join(wsDir, "notes/keeper.md")

	// Pin mtime so the initial reindex and the restore record byte-identical
	// mtime+size — the exact condition that armed the permanent-loss skip.
	fixed := time.Unix(1_600_000_000, 0)

	if chErr := os.Chtimes(keeperPath, fixed, fixed); chErr != nil {
		test.Fatalf("chtimes: %v", chErr)
	}

	stop := runWatchBackground(test, wsDir)
	defer stop()

	idxPath := filepath.Join(wsDir, ".tusk", "index.db")

	if removeErr := os.Remove(keeperPath); removeErr != nil {
		test.Fatalf("remove keeper: %v", removeErr)
	}

	waitForWatch(test, 15*time.Second, "delete must reap the node (and tombstone file_state)", func() bool {
		return !watchTestNodeExists(idxPath, "notes/keeper")
	})

	// Restore with byte-identical content and mtime (cp -p / mv from backup).
	if writeErr := os.WriteFile(keeperPath, body, 0o644); writeErr != nil {
		test.Fatalf("restore keeper: %v", writeErr)
	}

	if chErr := os.Chtimes(keeperPath, fixed, fixed); chErr != nil {
		test.Fatalf("chtimes restore: %v", chErr)
	}

	waitForWatch(test, 15*time.Second, "restored file with preserved mtime+size must re-index", func() bool {
		return watchTestNodeExists(idxPath, "notes/keeper")
	})
}

const watchRefManifest = `[workspace]
name = "t"

[node-types.person]
properties = []

[node-types.ticket]
properties = [
    { name = "assignee", type = "ref", to = "person" },
]
`

// TestWatchCmd_HealsRefDriftAfterDelete pins finding #4 (#677 parity): deleting
// one of two ambiguous ref candidates must let the recorded ref_ambiguous drift
// heal — drift cleared and the now-unambiguous edge written. Before the fix the
// delete handler returned right after deleting the node row, so the ref-drift
// heal pass never ran on delete-only event streams.
func TestWatchCmd_HealsRefDriftAfterDelete(test *testing.T) {
	wsDir := test.TempDir()

	writeWatchTestFile(test, wsDir, "tusk.toml", watchRefManifest)
	writeWatchTestFile(test, wsDir, "people/alex1.md", "---\ntype: person\ntitle: Alex\n---\nA.\n")
	writeWatchTestFile(test, wsDir, "people/alex2.md", "---\ntype: person\ntitle: Alex\n---\nB.\n")
	writeWatchTestFile(test, wsDir, "tickets/t1.md", "---\ntype: ticket\ntitle: Fix login\nassignee: Alex\n---\nT.\n")

	stop := runWatchBackground(test, wsDir)
	defer stop()

	idxPath := filepath.Join(wsDir, ".tusk", "index.db")

	// Deleting one candidate leaves a single "Alex", so the ambiguity resolves.
	if removeErr := os.Remove(filepath.Join(wsDir, "people/alex2.md")); removeErr != nil {
		test.Fatalf("remove alex2: %v", removeErr)
	}

	waitForWatch(test, 20*time.Second, "ref drift must clear and the assignee edge must resolve after the delete", func() bool {
		idx, openErr := index.Open(idxPath)

		if openErr != nil {
			return false
		}

		defer idx.Close()

		driftRows, driftErr := index.NewPropertyDriftRepo(idx).ListAll()

		if driftErr != nil {
			return false
		}

		for _, row := range driftRows {
			if row.NodeID == "tickets/t1" {
				return false // drift still standing
			}
		}

		edges, edgeErr := index.NewEdgeRepo(idx).ListBySource("tickets/t1")

		if edgeErr != nil {
			return false
		}

		for _, edge := range edges {
			if edge.Type == "assignee" && edge.TargetID == "people/alex1" {
				return true
			}
		}

		return false
	})
}
