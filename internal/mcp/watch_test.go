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

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)

	go func() {
		done <- mcp.RunWatcher(ctx, mcp.WatchConfig{Runtime: rt})
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

			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	cancel()
	<-done

	test.Fatalf("expected node notes/external to be indexed after watcher saw the write")
}
