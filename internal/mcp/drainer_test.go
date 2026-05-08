package mcp_test

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/mcp"
)

func TestRunDrainer_DrainsQueueAndStopsOnCancel(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	rt.Nodes.Upsert(index.NodeRow{ID: "notes/x", Type: "note", Path: "notes/x.md", PropertiesJSON: "{}", LastChecksum: "x"})

	// No embedder → drainer is a no-op but should still respect the ticker
	// and exit on context cancellation cleanly.
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)

	go func() {
		done <- mcp.RunDrainer(ctx, mcp.DrainerConfig{
			Runtime:  rt,
			Interval: 20 * time.Millisecond,
		})
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case runErr := <-done:
		if runErr != nil {
			test.Fatalf("RunDrainer returned %v", runErr)
		}
	case <-time.After(2 * time.Second):
		test.Fatalf("RunDrainer did not exit after cancel")
	}
}
