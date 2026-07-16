package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

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

// TestGraphExpandOverride_NoExpandBeatsExpand asserts the tri-state precedence:
// --no-graph-expand overrides --graph-expand and the workspace default. The
// merge itself now lives in manifest.MergeGraphExpansion; this flag precedence
// is what stayed behind in the CLI, so it is tested where it lives.
func TestGraphExpandOverride_NoExpandBeatsExpand(test *testing.T) {
	cases := []struct {
		name string
		args []string
		want *bool
	}{
		{
			name: "no flags inherits the workspace",
			args: nil,
			want: nil,
		},
		{
			name: "no-graph-expand beats graph-expand",
			args: []string{"--graph-expand", "--no-graph-expand"},
			want: boolPtr(false),
		},
		{
			// Passing --no-graph-expand at all disables expansion, whatever
			// value it carries.
			name: "no-graph-expand=false still disables",
			args: []string{"--no-graph-expand=false"},
			want: boolPtr(false),
		},
		{
			name: "graph-expand enables",
			args: []string{"--graph-expand"},
			want: boolPtr(true),
		},
		{
			name: "graph-expand=false disables",
			args: []string{"--graph-expand=false"},
			want: boolPtr(false),
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			var (
				graphExpand   bool
				graphNoExpand bool
			)

			flags := pflag.NewFlagSet("query", pflag.ContinueOnError)
			flags.BoolVar(&graphExpand, "graph-expand", false, "")
			flags.BoolVar(&graphNoExpand, "no-graph-expand", false, "")

			if parseErr := flags.Parse(testCase.args); parseErr != nil {
				test.Fatalf("parse %v: %v", testCase.args, parseErr)
			}

			got := graphExpandOverride(flags, graphExpand)

			switch {
			case testCase.want == nil && got != nil:
				test.Fatalf("override = %v, want nil (the manifest must survive)", *got)
			case testCase.want != nil && got == nil:
				test.Fatalf("override = nil, want %v", *testCase.want)
			case testCase.want != nil && *got != *testCase.want:
				test.Errorf("override = %v, want %v", *got, *testCase.want)
			}
		})
	}
}

// boolPtr returns a pointer to value, so the table above can express an
// expected optional.
func boolPtr(value bool) *bool {
	return &value
}
