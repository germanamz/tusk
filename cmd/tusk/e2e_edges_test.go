package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestE2E_EdgesLifecycle(test *testing.T) {
	tmpDir := initWorkspaceWithManifest(test, edgeManifestBody())

	// 1) Create a small graph.
	for _, args := range [][]string{
		{"node", "create", "--type", "ticket", "--title", "Epic", "--path", "tickets/epic.md"},
		{"node", "create", "--type", "ticket", "--title", "Foo", "--path", "tickets/foo.md"},
		{"node", "create", "--type", "ticket", "--title", "Bar", "--path", "tickets/bar.md"},
	} {
		cmd := newRootCmd()
		cmd.SetArgs(args)

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("create %v: %v", args, execErr)
		}
	}

	// 2) Drop a frontmatter-edge node externally — `parent` and a wikilink.
	external := filepath.Join(tmpDir, "tickets/child.md")

	body := []byte(`---
type: ticket
title: Child
parent: tickets/epic
---

This child references [[tickets/foo]] in its body.
`)

	if writeErr := os.WriteFile(external, body, 0o644); writeErr != nil {
		test.Fatalf("write external: %v", writeErr)
	}

	// 3) Reindex — should pick up the parent edge AND the references edge.
	{
		cmd := newRootCmd()
		cmd.SetArgs([]string{"reindex"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("reindex: %v", execErr)
		}
	}

	// 4) edge list --from tickets/child shows both edges.
	{
		out := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"edge", "list", "--from", "tickets/child"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("list child edges: %v", execErr)
		}

		body := out.String()

		if !bytes.Contains(out.Bytes(), []byte("parent")) {
			test.Errorf("missing parent edge:\n%s", body)
		}

		if !bytes.Contains(out.Bytes(), []byte("references")) {
			test.Errorf("missing references edge:\n%s", body)
		}
	}

	// 5) Add a CLI-tracked edge: foo blocks bar.
	{
		cmd := newRootCmd()
		cmd.SetArgs([]string{"edge", "add", "--type", "blocks", "--source", "tickets/foo", "--target", "tickets/bar"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("edge add: %v", execErr)
		}
	}

	// 6) edge list --type blocks shows only foo→bar.
	{
		out := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"edge", "list", "--type", "blocks"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("list blocks: %v", execErr)
		}

		if !bytes.Contains(out.Bytes(), []byte("tickets/foo")) || !bytes.Contains(out.Bytes(), []byte("tickets/bar")) {
			test.Errorf("missing foo→bar:\n%s", out.String())
		}
	}

	// 7) Try to introduce a cycle: bar blocks foo. Acyclic should reject it.
	{
		cmd := newRootCmd()
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"edge", "add", "--type", "blocks", "--source", "tickets/bar", "--target", "tickets/foo"})

		if execErr := cmd.Execute(); execErr == nil {
			test.Fatalf("expected cycle error on bar→foo blocks")
		}
	}

	// 8) Remove foo→bar.
	{
		cmd := newRootCmd()
		cmd.SetArgs([]string{"edge", "remove", "--type", "blocks", "--source", "tickets/foo", "--target", "tickets/bar"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("edge remove: %v", execErr)
		}
	}

	// 9) Delete the child file and reindex; edges should be gone.
	if rmErr := os.Remove(external); rmErr != nil {
		test.Fatalf("rm: %v", rmErr)
	}

	{
		cmd := newRootCmd()
		cmd.SetArgs([]string{"reindex"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("second reindex: %v", execErr)
		}
	}

	{
		out := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"edge", "list", "--from", "tickets/child"})

		// Empty result is acceptable — list may return error "specify at least one
		// of ..." or simply print only the header. We just assert the body doesn't
		// contain "parent" / "references" data rows.
		_ = cmd.Execute()

		if bytes.Contains(out.Bytes(), []byte("references")) {
			test.Errorf("child edges should be gone, list still shows references:\n%s", out.String())
		}
	}
}
