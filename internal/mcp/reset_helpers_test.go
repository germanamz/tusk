package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/reindex"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// setupServerWorkspace creates a temp workspace with a manifest and one seeded
// node file ON DISK (disk presence is mandatory: tusk_reset / siblingReopen
// rebuild from disk, so a row-only seed would vanish after a reset). Returns the
// workspace root.
//
//nolint:unused // becomes live in Task 5 (swap_test.go) and Phases 6-8 reset/epoch tests.
func setupServerWorkspace(test *testing.T) string {
	test.Helper()

	root := test.TempDir()

	if err := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte("[workspace]\nname = \"test\"\n"), 0o644); err != nil {
		test.Fatalf("write tusk.toml: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		test.Fatalf("mkdir notes: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "notes", "hi.md"), []byte("---\ntype: note\ntitle: Hi\n---\nhello\n"), 0o644); err != nil {
		test.Fatalf("write node: %v", err)
	}

	return root
}

// newServerForRoot opens the workspace at root, populates the index with a
// blocking reindex (Open does NOT index a fresh empty DB), and returns a Server.
// Two daemons can share one root by calling this twice with the same root.
//
//nolint:unused // becomes live in Task 5 (swap_test.go) and Phases 6-8 reset/epoch tests.
func newServerForRoot(test *testing.T, root string) *Server {
	test.Helper()

	rt, err := Open(root) // unqualified: this file is package mcp

	if err != nil {
		test.Fatalf("Open(%s): %v", root, err)
	}

	test.Cleanup(func() { _ = rt.Close() })

	// Populate node rows: Open/OpenOrRebuild only rebuilds on schema mismatch, so
	// a fresh index needs an explicit blocking reindex (Workers >= 1, Async:false).
	if _, runErr := reindex.Run(reindex.Config{
		Root:       rt.Root,
		Repo:       rt.Nodes,
		Edges:      rt.Edges,
		EdgeTypes:  rt.Manifest.EdgeTypes,
		EmbedQueue: rt.EmbedQueue,
		Meta:       rt.Meta,
		FileStates: rt.FileState,
		NodeTypes:  rt.Manifest.NodeTypes,
		Manifest:   rt.Manifest,
		Workers:    1,
		Async:      false,
	}); runErr != nil {
		test.Fatalf("seed reindex: %v", runErr)
	}

	return NewServer(rt)
}

// buildTestServer is the common single-daemon fixture.
//
//nolint:unused // becomes live in Task 5 (swap_test.go) and Phases 6-8 reset/epoch tests.
func buildTestServer(test *testing.T) *Server {
	test.Helper()

	return newServerForRoot(test, setupServerWorkspace(test))
}

// seededNodeID is the id of the node setupServerWorkspace writes to disk.
//
//nolint:unused // shared fixture surface consumed by Phases 6-8 reset/epoch tests.
func seededNodeID(test *testing.T) string {
	test.Helper()

	return "notes/hi"
}

// nodeListRequest builds a tusk_node_list call.
//
//nolint:unused // becomes live in Task 5 (swap_test.go) and Phases 6-8 reset/epoch tests.
func nodeListRequest() mcpgo.CallToolRequest {
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "tusk_node_list"

	return req
}

// textOf extracts the text content of a tool result (mirrors the idiom in the
// package mcp_test golden harness, duplicated here for package mcp).
//
//nolint:unused // becomes live in Task 5 (swap_test.go) and Phases 6-8 reset/epoch tests.
func textOf(result *mcpgo.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}

	if text, ok := result.Content[0].(mcpgo.TextContent); ok {
		return text.Text
	}

	return ""
}

// rebuildIndex synchronously walks + indexes the workspace into srv's CURRENT
// runtime handle (Workers:1, blocking), so structural node/edge rows materialize
// for assertions. Use after a reset/reopen, which only kick an Async walk (it
// enqueues reindex jobs but does NOT write node rows without a running drainer).
//
//nolint:unused // shared fixture surface consumed by Phases 6-8 reset/epoch tests.
func rebuildIndex(test *testing.T, srv *Server) {
	test.Helper()

	rt := srv.snapshotRuntime()

	if _, err := reindex.Run(reindex.Config{
		Root:            rt.Root,
		Repo:            rt.Nodes,
		Edges:           rt.Edges,
		EdgeTypes:       rt.Manifest.EdgeTypes,
		WorkspaceIgnore: rt.Manifest.Workspace.Ignore,
		EmbedQueue:      rt.EmbedQueue,
		Meta:            rt.Meta,
		FileStates:      rt.FileState,
		NodeTypes:       rt.Manifest.NodeTypes,
		Manifest:        rt.Manifest,
		Workers:         1,
		Async:           false,
	}); err != nil {
		test.Fatalf("rebuildIndex: %v", err)
	}
}
