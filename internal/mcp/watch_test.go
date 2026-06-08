package mcp_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/mcp"
)

func TestRunWatcher_PicksUpExternalEdit(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	srv := mcp.NewServer(rt)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	drainerDone := make(chan error, 1)

	go func() {
		done <- mcp.RunWatcher(ctx, mcp.WatchConfig{Server: srv})
	}()

	go func() {
		drainerDone <- mcp.RunReindexDrainer(ctx, mcp.ReindexDrainerConfig{
			Server:   srv,
			Interval: 100 * time.Millisecond,
		})
	}()

	time.Sleep(100 * time.Millisecond) // let watcher boot

	if mkErr := os.MkdirAll(filepath.Join(rt.Root, "notes"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	body := []byte("---\ntype: note\ntitle: external\n---\n\nbody\n")

	if writeErr := os.WriteFile(filepath.Join(rt.Root, "notes/external.md"), body, 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		if _, getErr := rt.Nodes.Get("notes/external"); getErr == nil {
			cancel()
			<-done
			<-drainerDone

			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	cancel()
	<-done
	<-drainerDone

	test.Fatalf("expected node notes/external to be indexed after watcher saw the write")
}

// TestRunWatcher_DoesNotLoopOnIndexWrites is the end-to-end regression guard for
// the runaway reindex loop: writes under the gitignored .tusk/ index dir must
// NOT trip the watcher into a reindex (every reindex bumps reindex_gen, which
// rewrites .tusk/, which would re-trip the watcher — forever). A real markdown
// edit must still reindex, proving the watcher is live and not over-filtering.
func TestRunWatcher_DoesNotLoopOnIndexWrites(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	srv := mcp.NewServer(rt)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)

	go func() {
		done <- mcp.RunWatcher(ctx, mcp.WatchConfig{Server: srv})
	}()

	time.Sleep(200 * time.Millisecond) // let the watcher boot off the snapshot

	tuskDir := filepath.Join(rt.Root, ".tusk")

	if mkErr := os.MkdirAll(tuskDir, 0o755); mkErr != nil {
		cancel()
		<-done
		test.Fatalf("mkdir .tusk: %v", mkErr)
	}

	genBefore, getErr := rt.Meta.Get("reindex_gen")

	if getErr != nil {
		cancel()
		<-done
		test.Fatalf("read reindex_gen: %v", getErr)
	}

	// Mimic the index churn that fueled the loop: repeated writes under .tusk/.
	for range [5]struct{}{} {
		if writeErr := os.WriteFile(filepath.Join(tuskDir, "index.db-wal"), []byte("x"), 0o644); writeErr != nil {
			cancel()
			<-done
			test.Fatalf("write .tusk/index.db-wal: %v", writeErr)
		}

		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(1 * time.Second) // > debounce; let any (wrongly) triggered reindex run

	genAfter, getErr := rt.Meta.Get("reindex_gen")

	if getErr != nil {
		cancel()
		<-done
		test.Fatalf("read reindex_gen: %v", getErr)
	}

	if genAfter != genBefore {
		cancel()
		<-done
		test.Fatalf("reindex_gen climbed %q → %q on .tusk/ writes (watcher self-triggered reindex)", genBefore, genAfter)
	}

	// Positive control: a real markdown edit MUST still trigger a reindex.
	if mkErr := os.MkdirAll(filepath.Join(rt.Root, "notes"), 0o755); mkErr != nil {
		cancel()
		<-done
		test.Fatalf("mkdir notes: %v", mkErr)
	}

	body := []byte("---\ntype: note\ntitle: real\n---\n\nbody\n")

	if writeErr := os.WriteFile(filepath.Join(rt.Root, "notes/real.md"), body, 0o644); writeErr != nil {
		cancel()
		<-done
		test.Fatalf("write notes/real.md: %v", writeErr)
	}

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		gen, genErr := rt.Meta.Get("reindex_gen")

		if genErr == nil && gen != genAfter {
			cancel()
			<-done

			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	cancel()
	<-done

	test.Fatalf("expected reindex_gen to climb past %q after a real markdown edit", genAfter)
}
