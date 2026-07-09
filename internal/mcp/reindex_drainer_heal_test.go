package mcp_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/mcp"
	"github.com/germanamz/tusk/internal/reindex"
)

// TestRunReindexDrainer_HealsRefDriftAfterProductiveTick pins the async half
// of issue #677: a recorded ref_dangling row must be retried by the
// background drainer once a tick indexes something (here, the just-created
// target), without the referencing file ever changing.
func TestRunReindexDrainer_HealsRefDriftAfterProductiveTick(test *testing.T) {
	root := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(`[workspace]
name = "x"

[node-types.person]
properties = [
    { name = "name", type = "string", required = true },
]

[node-types.ticket]
properties = [
    { name = "assignee", type = "ref", to = "person" },
]
`), 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}

	if mkErr := os.MkdirAll(filepath.Join(root, "aref"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "aref/auth.md"),
		[]byte("---\ntype: ticket\ntitle: Auth\nassignee: alice\n---\n\nbody\n"), 0o644); writeErr != nil {
		test.Fatalf("write ticket: %v", writeErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer rt.Close()

	// Sync pass wedges the workspace: assignee dangles (alice missing) and
	// aref/auth.md will be walk-skipped on every later pass.
	if _, runErr := reindex.Run(reindex.Config{
		Root:          rt.Root,
		Repo:          rt.Nodes,
		Edges:         rt.Edges,
		EdgeTypes:     rt.Manifest.EdgeTypes,
		NodeTypes:     rt.Manifest.NodeTypes,
		PropertyDrift: rt.PropertyDrift,
		Meta:          rt.Meta,
		FileStates:    rt.FileState,
		EmbedQueue:    rt.EmbedQueue,
		Workers:       rt.Workers,
	}); runErr != nil {
		test.Fatalf("wedge Run: %v", runErr)
	}

	if edges, _ := rt.Edges.ListBySource("aref/auth"); len(edges) != 0 {
		test.Fatalf("premise broken: edge exists before target does: %+v", edges)
	}

	// The target appears and lands in the queue (as a watch walk would put
	// it); the referencing file stays untouched.
	if mkErr := os.MkdirAll(filepath.Join(root, "zref"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "zref/alice.md"),
		[]byte("---\ntype: person\ntitle: alice\nname: Alice\n---\n\nbio\n"), 0o644); writeErr != nil {
		test.Fatalf("write person: %v", writeErr)
	}

	if enqErr := rt.EmbedQueue.EnqueueReindex("zref/alice.md"); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	srv := mcp.NewServer(rt)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)

	logBuf := &strings.Builder{}

	go func() {
		done <- mcp.RunReindexDrainer(ctx, mcp.ReindexDrainerConfig{
			Server:   srv,
			Interval: 50 * time.Millisecond,
			Logger:   slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})),
		})
	}()

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		edges, _ := rt.Edges.ListBySource("aref/auth")

		// The heal drain runs with the full config, so sub-unit sync may add
		// structural edges alongside the healed ref edge; match specifically.
		for _, edge := range edges {
			if edge.Type == "assignee" && edge.TargetID == "zref/alice" {
				cancel()
				<-done

				return
			}
		}

		time.Sleep(50 * time.Millisecond)
	}

	cancel()
	<-done

	test.Fatalf("drainer never healed aref/auth -> zref/alice after the target was indexed\ndrainer log:\n%s", logBuf.String())
}
