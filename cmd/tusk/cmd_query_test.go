package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

// manifestDefaultGraphExpansion is a tiny test-only shim so the assertions
// can read like "from default" without typing the package name three times.
func manifestDefaultGraphExpansion() manifest.GraphExpansion {
	return manifest.DefaultGraphExpansion()
}

func TestQueryCmd_FiltersByType(test *testing.T) {
	initWorkspace(test)

	for _, args := range [][]string{
		{"node", "create", "--type", "ticket", "--title", "T1", "--path", "tickets/t1.md"},
		{"node", "create", "--type", "ticket", "--title", "T2", "--path", "tickets/t2.md"},
		{"node", "create", "--type", "note", "--title", "N1", "--path", "notes/n1.md"},
	} {
		cmd := newRootCmd()
		cmd.SetArgs(args)

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("setup %v: %v", args, execErr)
		}
	}

	out := &bytes.Buffer{}

	queryCmd := newRootCmd()
	queryCmd.SetOut(out)
	queryCmd.SetErr(out)
	queryCmd.SetArgs([]string{"query", "type=ticket"})

	if execErr := queryCmd.Execute(); execErr != nil {
		test.Fatalf("query: %v", execErr)
	}

	body := out.String()

	if strings.Contains(body, "notes/n1") {
		test.Errorf("note should be excluded: %s", body)
	}

	if !strings.Contains(body, "tickets/t1") || !strings.Contains(body, "tickets/t2") {
		test.Errorf("missing tickets: %s", body)
	}
}

func TestQueryCmd_TakeAndSkip(test *testing.T) {
	initWorkspace(test)

	for index := 0; index < 5; index++ {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"node", "create", "--type", "note", "--title", "n", "--path", "notes/n" + string(rune('0'+index)) + ".md"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("create: %v", execErr)
		}
	}

	out := &bytes.Buffer{}

	cmd := newRootCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"query", "type=note", "--take", "2", "--skip", "1"})

	if execErr := cmd.Execute(); execErr != nil {
		test.Fatalf("query: %v", execErr)
	}

	body := out.String()

	dataLines := strings.Count(strings.TrimSpace(body), "\n")

	if dataLines != 2 {
		test.Errorf("expected 2 data rows (got %d):\n%s", dataLines, body)
	}
}

func TestQueryCmd_TraversalSortsByOrderedByProperty(test *testing.T) {
	manifestBody := `
[workspace]
name = "test"

[node-types.wbs-node]
properties = [
    { name = "order", type = "int" },
]

[edge-types.wbs-parent]
from        = ["wbs-node"]
to          = ["wbs-node"]
cardinality = "many-to-one"
ordered     = "order"
hierarchy   = "wbs"
`

	tmpDir := initWorkspaceWithManifest(test, manifestBody)

	// Parent node.
	writeNodeFile(test, tmpDir, "wbs/proj.md", `---
type: wbs-node
title: Project
---

Root of the project.
`)

	// Three children, with order 3, 1, 2 (intentionally not in creation order).
	writeNodeFile(test, tmpDir, "wbs/c-three.md", `---
type: wbs-node
title: Three
wbs-parent: wbs/proj
order: 3
---

Third child.
`)

	writeNodeFile(test, tmpDir, "wbs/c-one.md", `---
type: wbs-node
title: One
wbs-parent: wbs/proj
order: 1
---

First child.
`)

	writeNodeFile(test, tmpDir, "wbs/c-two.md", `---
type: wbs-node
title: Two
wbs-parent: wbs/proj
order: 2
---

Second child.
`)

	// Reindex so frontmatter edges and properties are visible to the query.
	{
		cmd := newRootCmd()
		cmd.SetArgs([]string{"reindex"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("reindex: %v", execErr)
		}
	}

	out := &bytes.Buffer{}

	queryCmd := newRootCmd()
	queryCmd.SetOut(out)
	queryCmd.SetErr(out)
	queryCmd.SetArgs([]string{"query", "parent=wbs/proj"})

	if execErr := queryCmd.Execute(); execErr != nil {
		test.Fatalf("query: %v\n%s", execErr, out.String())
	}

	body := out.String()

	indexOne := strings.Index(body, "wbs/c-one")
	indexTwo := strings.Index(body, "wbs/c-two")
	indexThree := strings.Index(body, "wbs/c-three")

	if indexOne < 0 || indexTwo < 0 || indexThree < 0 {
		test.Fatalf("missing one of the children in output:\n%s", body)
	}

	if indexOne >= indexTwo || indexTwo >= indexThree {
		test.Errorf("expected children in order c-one, c-two, c-three; got:\n%s", body)
	}
}

func writeNodeFile(test *testing.T, root, relPath, body string) {
	test.Helper()

	absPath := filepath.Join(root, relPath)

	if mkErr := os.MkdirAll(filepath.Dir(absPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir %s: %v", filepath.Dir(absPath), mkErr)
	}

	if writeErr := os.WriteFile(absPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write %s: %v", absPath, writeErr)
	}
}

func TestQueryCmd_ErrorsWithoutFilter(test *testing.T) {
	initWorkspace(test)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"query"})

	if execErr := cmd.Execute(); execErr == nil {
		test.Fatalf("expected error when filter argument is missing")
	}
}

// TestQueryCmd_GraphExpandFlagsRegistered confirms the new Phase 3 plumbing
// flags exist on the command so subsequent tasks can rely on them.
func TestQueryCmd_GraphExpandFlagsRegistered(test *testing.T) {
	cmd := newQueryCmd()

	required := []string{"graph-expand", "no-graph-expand", "hops", "graph-weight", "graph-edges", "explain"}

	for _, name := range required {
		if cmd.Flags().Lookup(name) == nil {
			test.Errorf("--%s flag not registered on tusk query", name)
		}
	}
}

// TestQueryCmd_RejectsInvalidHops confirms the per-call --hops override is
// validated before the query runs.
func TestQueryCmd_RejectsInvalidHops(test *testing.T) {
	initWorkspace(test)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"query", "type=note", "--hops", "5"})

	execErr := cmd.Execute()

	if execErr == nil {
		test.Fatalf("expected error for --hops 5")
	}

	if !strings.Contains(execErr.Error(), "hops") {
		test.Errorf("error %q should mention hops", execErr.Error())
	}
}

// TestQueryCmd_RejectsInvalidGraphWeight confirms --graph-weight outside
// [0,1] surfaces a usage error.
func TestQueryCmd_RejectsInvalidGraphWeight(test *testing.T) {
	initWorkspace(test)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"query", "type=note", "--graph-weight", "1.5"})

	execErr := cmd.Execute()

	if execErr == nil {
		test.Fatalf("expected error for --graph-weight 1.5")
	}

	if !strings.Contains(execErr.Error(), "graph-weight") {
		test.Errorf("error %q should mention graph-weight", execErr.Error())
	}
}

// TestMergeGraphExpansion_NoOverridesPreservesBase confirms the merger
// returns the manifest defaults verbatim when no per-call flags are set.
func TestMergeGraphExpansion_NoOverridesPreservesBase(test *testing.T) {
	base := manifestDefaultGraphExpansion()

	got, mergeErr := mergeGraphExpansion(base, graphExpansionOverrides{})

	if mergeErr != nil {
		test.Fatalf("mergeGraphExpansion: %v", mergeErr)
	}

	if got == nil {
		test.Fatalf("mergeGraphExpansion: nil result")
	}

	if got.Enabled != base.Enabled {
		test.Errorf("Enabled = %v, want %v", got.Enabled, base.Enabled)
	}

	if got.Hops != base.Hops {
		test.Errorf("Hops = %d, want %d", got.Hops, base.Hops)
	}
}

// TestMergeGraphExpansion_NoExpandBeatsExpand asserts the tri-state
// precedence: --no-graph-expand overrides --graph-expand and the workspace
// default.
func TestMergeGraphExpansion_NoExpandBeatsExpand(test *testing.T) {
	base := manifestDefaultGraphExpansion()
	base.Enabled = true

	got, mergeErr := mergeGraphExpansion(base, graphExpansionOverrides{
		ExpandSet:   true,
		ExpandValue: true,
		NoExpandSet: true,
		NoExpand:    true,
	})

	if mergeErr != nil {
		test.Fatalf("mergeGraphExpansion: %v", mergeErr)
	}

	if got.Enabled {
		test.Errorf("Enabled = true, want false (--no-graph-expand must beat --graph-expand)")
	}
}

// TestMergeGraphExpansion_ExpandFlagBeatsWorkspaceDisabled confirms that
// --graph-expand turns the feature on even when the workspace ships with
// enabled=false.
func TestMergeGraphExpansion_ExpandFlagBeatsWorkspaceDisabled(test *testing.T) {
	base := manifestDefaultGraphExpansion() // Enabled = false

	got, mergeErr := mergeGraphExpansion(base, graphExpansionOverrides{
		ExpandSet:   true,
		ExpandValue: true,
	})

	if mergeErr != nil {
		test.Fatalf("mergeGraphExpansion: %v", mergeErr)
	}

	if !got.Enabled {
		test.Errorf("Enabled = false, want true (--graph-expand should enable)")
	}
}

// TestMergeGraphExpansion_ExpandFalseBeatsWorkspaceEnabled confirms an
// explicit --graph-expand=false disables the feature even when the workspace
// manifest enables it. The previous switch arm only ran when ExpandValue was
// true, silently dropping the user's explicit false.
func TestMergeGraphExpansion_ExpandFalseBeatsWorkspaceEnabled(test *testing.T) {
	base := manifestDefaultGraphExpansion()
	base.Enabled = true // Workspace ships with enabled = true.

	got, mergeErr := mergeGraphExpansion(base, graphExpansionOverrides{
		ExpandSet:   true,
		ExpandValue: false,
	})

	if mergeErr != nil {
		test.Fatalf("mergeGraphExpansion: %v", mergeErr)
	}

	if got.Enabled {
		test.Errorf("Enabled = true, want false (--graph-expand=false must beat workspace enabled=true)")
	}
}

// TestMergeGraphExpansion_EdgeTypesNotAliased confirms the resolved
// GraphExpansion does not share the backing array of its base.EdgeTypes
// slice. The MCP server fans requests out across goroutines, so an aliased
// slice would race once a future caller mutates it.
func TestMergeGraphExpansion_EdgeTypesNotAliased(test *testing.T) {
	base := manifestDefaultGraphExpansion()

	if len(base.EdgeTypes) == 0 {
		test.Fatalf("default EdgeTypes unexpectedly empty")
	}

	got, mergeErr := mergeGraphExpansion(base, graphExpansionOverrides{})

	if mergeErr != nil {
		test.Fatalf("mergeGraphExpansion: %v", mergeErr)
	}

	if &got.EdgeTypes[0] == &base.EdgeTypes[0] {
		test.Errorf("resolved EdgeTypes shares backing array with base; want clone")
	}
}
