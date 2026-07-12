package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestE2E_NodeGetIncludeEdges reproduces issue #706: `node get <id> --include
// edges` must hydrate the node's edges from the index (as `edge list` /
// `query --include edges` do) instead of always returning null.
func TestE2E_NodeGetIncludeEdges(test *testing.T) {
	tmpDir := initWorkspaceWithManifest(test, edgeManifestBody())

	if mkErr := os.MkdirAll(filepath.Join(tmpDir, "tickets"), 0o755); mkErr != nil {
		test.Fatalf("mkdir tickets: %v", mkErr)
	}

	// Epic is the parent target; child declares a `parent` frontmatter edge.
	if writeErr := os.WriteFile(filepath.Join(tmpDir, "tickets/epic.md"), []byte(
		"---\ntype: ticket\ntitle: Epic\n---\n\nEpic body.\n"), 0o644); writeErr != nil {
		test.Fatalf("write epic: %v", writeErr)
	}

	if writeErr := os.WriteFile(filepath.Join(tmpDir, "tickets/child.md"), []byte(
		"---\ntype: ticket\ntitle: Child\nparent: tickets/epic\n---\n\nChild body.\n"), 0o644); writeErr != nil {
		test.Fatalf("write child: %v", writeErr)
	}

	{
		cmd := newRootCmd()
		cmd.SetArgs([]string{"reindex"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("reindex: %v", execErr)
		}
	}

	out := &bytes.Buffer{}
	cmd := newRootCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"node", "get", "tickets/child", "--include", "edges", "--format", "json"})

	if execErr := cmd.Execute(); execErr != nil {
		test.Fatalf("node get: %v\n%s", execErr, out.String())
	}

	var payload map[string]any

	if unmarshalErr := json.Unmarshal(out.Bytes(), &payload); unmarshalErr != nil {
		test.Fatalf("unmarshal node get json: %v\n%s", unmarshalErr, out.String())
	}

	edgesRaw, present := payload["edges"]

	if !present {
		test.Fatalf("node get did not emit an edges key:\n%s", out.String())
	}

	if edgesRaw == nil {
		test.Fatalf("node get returned edges:null despite an existing parent edge (issue #706):\n%s", out.String())
	}

	edges, ok := edgesRaw.([]any)

	if !ok {
		test.Fatalf("edges is %T, want a JSON array of edge refs:\n%s", edgesRaw, out.String())
	}

	var foundParent bool

	for _, entry := range edges {
		edge, _ := entry.(map[string]any)

		if edge["type"] == "parent" && edge["direction"] == "out" && edge["target_id"] == "tickets/epic" {
			foundParent = true
		}
	}

	if !foundParent {
		test.Errorf("expected an out parent edge to tickets/epic, got:\n%s", out.String())
	}
}
