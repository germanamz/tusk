package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestE2E_PathRefFollowUps runs the #780 follow-ups through the CLI: a page
// names paths in a paths-only frontmatter value, a markdown link and a bare
// `Makefile`; node get --include paths lists them with lines and existence;
// and a qualified names-path matches one edge type's refs only.
func TestE2E_PathRefFollowUps(test *testing.T) {
	root := initWorkspaceWithManifest(test, `[workspace]
name = "x"

[edge-types.describes]
from  = ["technical"]
paths = true

[edge-types.mentions]
from  = ["technical"]
paths = true
`)

	files := map[string]string{
		"server/ledger/core/service.go": "package core\n",
		"Makefile":                      "all:\n",
		"technical/ledger.md": "---\n" + // 1
			"type: technical\n" + // 2
			"title: Ledger\n" + // 3
			"describes: [server/ledger/core/service.go]\n" + // 4
			"---\n" + // 5
			"\n" + // 6
			"Build with `Makefile`; see [the old service](../server/ledger/old.go).\n", // 7
	}

	for relPath, content := range files {
		if mkErr := os.MkdirAll(filepath.Dir(filepath.Join(root, relPath)), 0o755); mkErr != nil {
			test.Fatalf("mkdir: %v", mkErr)
		}

		if writeErr := os.WriteFile(filepath.Join(root, relPath), []byte(content), 0o644); writeErr != nil {
			test.Fatalf("write %s: %v", relPath, writeErr)
		}
	}

	stdoutOf(test, "reindex")

	var payload struct {
		Paths []struct {
			Path     string `json:"path"`
			EdgeType string `json:"edge_type"`
			Exists   bool   `json:"exists"`
			Mentions []struct {
				Line int `json:"line"`
			} `json:"mentions"`
		} `json:"paths"`
	}

	nodeGet := stdoutOf(test, "node", "get", "technical/ledger", "--include", "paths", "--format", "json")

	if unmarshalErr := json.Unmarshal([]byte(nodeGet), &payload); unmarshalErr != nil {
		test.Fatalf("unmarshal node get: %v\n%s", unmarshalErr, nodeGet)
	}

	var got []string

	for _, ref := range payload.Paths {
		lines := make([]string, 0, len(ref.Mentions))

		for _, mention := range ref.Mentions {
			lines = append(lines, strconv.Itoa(mention.Line))
		}

		got = append(got, ref.EdgeType+" "+ref.Path+" exists="+strconv.FormatBool(ref.Exists)+" lines="+strings.Join(lines, ","))
	}

	slices.Sort(got)

	want := []string{
		"describes Makefile exists=true lines=7",
		"describes server/ledger/core/service.go exists=true lines=4",
		"describes server/ledger/old.go exists=false lines=7",
		"mentions Makefile exists=true lines=7",
		"mentions server/ledger/old.go exists=false lines=7",
	}

	if !slices.Equal(got, want) {
		test.Errorf("node get --include paths =\n%s\nwant\n%s\nraw:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"), nodeGet)
	}

	// The frontmatter value is a describes path only; mentions scans the body.
	for filterText, wantIDs := range map[string][]string{
		"names-path=server/ledger/core/service.go":           {"technical/ledger"},
		"names-path:describes=server/ledger/core/service.go": {"technical/ledger"},
		"names-path:mentions=server/ledger/core/service.go":  nil,
		"names-path:mentions=Makefile":                       {"technical/ledger"},
	} {
		var rows []map[string]any

		queried := stdoutOf(test, "query", filterText, "--json")

		if unmarshalErr := json.Unmarshal([]byte(queried), &rows); unmarshalErr != nil {
			test.Fatalf("unmarshal query %q: %v\n%s", filterText, unmarshalErr, queried)
		}

		var ids []string

		for _, row := range rows {
			ids = append(ids, row["id"].(string))
		}

		if !slices.Equal(ids, wantIDs) {
			test.Errorf("query %q = %v, want %v", filterText, ids, wantIDs)
		}
	}
}

// stdoutOf executes the root command with args and returns what it wrote to
// stdout, failing the test with stderr when it errors.
func stdoutOf(test *testing.T, args ...string) string {
	test.Helper()

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd := newRootCmd()
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs(args)

	if execErr := cmd.Execute(); execErr != nil {
		test.Fatalf("tusk %s: %v\n%s%s", strings.Join(args, " "), execErr, out.String(), errOut.String())
	}

	return out.String()
}
