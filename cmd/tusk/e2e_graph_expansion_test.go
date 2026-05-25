package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2E_GraphExpansion is the Phase 3 integration smoke: exercises
// the whole feature through the CLI, including the doctor pane. The
// fixture vault is structured so a referentially-popular note ("auth-flow")
// is multiply referenced by notes whose embeddings outrank it on the
// query "auth"; with graph expansion off the hub stays buried, and with
// expansion on the hub is promoted into the top results. The doctor
// surface is then asserted in a separate invocation so the pane wiring
// (CLI renderer + Issue-emission rules) is covered end-to-end.
func TestE2E_GraphExpansion(test *testing.T) {
	tmpDir := initWorkspace(test)

	// Stub embedder: maps marker keywords found anywhere in the prompt
	// to a fixed 3-dim vector. The reindex pipeline prepends a frontmatter
	// header to each chunk before embedding (see internal/embed/payload.go),
	// so first-byte heuristics don't work — the header always starts with
	// "[type]". Instead we look for distinctive body tokens.
	//
	// Vector layout (3 axes):
	//   * axis 0: the "auth" semantic — query and aligned notes use it.
	//   * axis 2: the orthogonal axis — hub and noise sit here.
	//
	// Query prompt "auth" embeds to {1,0,0}. Note bodies that contain
	// "alpha-token" embed to {1,0,0} (cosine 1 against the query). The
	// hub body contains "hub-token" → {0,0,1} (cosine 0). The noise body
	// contains "noise-token" → {0.1, 0, 0.99} (small positive cosine).
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Prompt string `json:"prompt"`
		}

		_ = json.NewDecoder(request.Body).Decode(&payload)

		vector := []float64{0, 0, 1} // default = orthogonal to "auth"

		switch {
		case strings.Contains(payload.Prompt, "alpha-token"):
			vector = []float64{1, 0, 0}
		case strings.Contains(payload.Prompt, "hub-token"):
			vector = []float64{0, 0, 1}
		case strings.Contains(payload.Prompt, "noise-token"):
			vector = []float64{0.1, 0, 0.99}
		case payload.Prompt == "auth":
			// Bare query string from the CLI — align with the alpha notes.
			vector = []float64{1, 0, 0}
		}

		_ = json.NewEncoder(writer).Encode(map[string]any{"embedding": vector})
	}))

	defer server.Close()

	manifestBody := `[workspace]
name = "test"
sub-units = false

[edge-types.references]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"

[embeddings]
provider = "ollama"
model = "test-model"
endpoint = "` + server.URL + `"
dim = 3

[query.graph-expansion]
enabled = false
weight = 0.3
hops = 1
edge-types = ["references"]
`

	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	if writeErr := os.WriteFile(manifestPath, []byte(manifestBody), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	// Six notes:
	//   - "hub" has an orthogonal embedding (the embedder maps 'h' to the
	//     third axis) so cosine alone never surfaces it.
	//   - Three "alpha-*" notes are referrers that align with the query
	//     vector; each links to hub via the `references` edge.
	//   - "noise" is a low-cosine baseline (starts with 'x') that has no
	//     edges. Without graph expansion noise outranks hub on cosine
	//     (small positive vs zero). With expansion enabled hub picks up
	//     graph score from its three referrers and overtakes noise.
	for _, args := range [][]string{
		{"node", "create", "--type", "note", "--title", "Hub Note", "--path", "hub.md"},
		{"node", "create", "--type", "note", "--title", "Alpha One", "--path", "alpha-one.md"},
		{"node", "create", "--type", "note", "--title", "Alpha Two", "--path", "alpha-two.md"},
		{"node", "create", "--type", "note", "--title", "Alpha Three", "--path", "alpha-three.md"},
		{"node", "create", "--type", "note", "--title", "Noise", "--path", "noise.md"},
	} {
		cmd := newRootCmd()
		cmd.SetArgs(args)

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("setup %v: %v", args, execErr)
		}
	}

	// Body content + manual frontmatter rewrite to add references to hub.
	// `hub.md` has an orthogonal-friendly body so its file-level chunk
	// has cosine 0 against the query. `noise.md`'s body starts with 'x',
	// also orthogonal, so it ties with hub on cosine but has no edges
	// for the walker to leverage.
	nodeBodies := map[string]string{
		"hub.md":         "---\ntype: note\ntitle: Hub Note\n---\n\nhub-token body content\n",
		"alpha-one.md":   "---\ntype: note\ntitle: Alpha One\nreferences:\n  - hub\n---\n\nalpha-token body one\n",
		"alpha-two.md":   "---\ntype: note\ntitle: Alpha Two\nreferences:\n  - hub\n---\n\nalpha-token body two\n",
		"alpha-three.md": "---\ntype: note\ntitle: Alpha Three\nreferences:\n  - hub\n---\n\nalpha-token body three\n",
		"noise.md":       "---\ntype: note\ntitle: Noise\n---\n\nnoise-token body content\n",
	}

	for filename, body := range nodeBodies {
		fullPath := filepath.Join(tmpDir, filename)

		if writeErr := os.WriteFile(fullPath, []byte(body), 0o644); writeErr != nil {
			test.Fatalf("write %s: %v", filename, writeErr)
		}
	}

	{
		reindexCmd := newRootCmd()
		reindexCmd.SetArgs([]string{"reindex"})

		if execErr := reindexCmd.Execute(); execErr != nil {
			test.Fatalf("reindex: %v", execErr)
		}
	}

	// Pass 1: graph-expansion DISABLED. hub must not be in the top-2 by
	// cosine alone. The four alpha notes share an identical embedding,
	// so they fill the top results.
	// Note: --take is applied at the SQL pre-filter (before semantic
	// ranking), so we omit it here and assert on the full ranked order:
	// with expansion off, hub must rank last (cosine 0); with expansion
	// on, hub must climb above at least one alpha because the blended
	// final score picks up graph contribution from its three referrers.
	baseline := runQueryJSON(test, "auth", []string{"type=note", "--semantic", "auth"})

	test.Logf("baseline rows ids: %v", idsOf(baseline))

	if len(baseline) == 0 {
		test.Fatalf("baseline returned no rows; check fixture")
	}

	baselineIDs := idsOf(baseline)
	hubPosBaseline := indexOf(baselineIDs, "hub")

	if hubPosBaseline != len(baselineIDs)-1 {
		test.Errorf("baseline (expansion off): hub at position %d (of %d); expected LAST because its cosine is orthogonal to the query: %v", hubPosBaseline, len(baselineIDs), baselineIDs)
	}

	// Pass 2: per-call override --graph-expand. hub should now climb
	// above at least one alpha because the blended final score picks up
	// graph contribution from its three referrers.
	expanded := runQueryJSON(test, "auth", []string{
		"type=note", "--semantic", "auth",
		"--graph-expand",
	})

	expandedIDs := idsOf(expanded)
	hubPosExpanded := indexOf(expandedIDs, "hub")

	test.Logf("expanded rows ids: %v", expandedIDs)

	if hubPosExpanded < 0 {
		test.Fatalf("with --graph-expand, hub absent from results: %v", expandedIDs)
	}

	if hubPosExpanded >= hubPosBaseline {
		test.Errorf("with --graph-expand, hub did not climb the ranking: baseline pos %d, expanded pos %d (full order: %v)", hubPosBaseline, hubPosExpanded, expandedIDs)
	}

	// Pass 3: doctor must surface the pane with the configured values.
	doctorOut := &bytes.Buffer{}
	doctorCmd := newRootCmd()
	doctorCmd.SetOut(doctorOut)
	doctorCmd.SetErr(doctorOut)
	doctorCmd.SetArgs([]string{"doctor"})

	if execErr := doctorCmd.Execute(); execErr != nil {
		test.Fatalf("doctor: %v\n%s", execErr, doctorOut.String())
	}

	output := doctorOut.String()

	if !strings.Contains(output, "graph expansion:") {
		test.Errorf("doctor output missing graph-expansion pane:\n%s", output)
	}

	if !strings.Contains(output, "enabled               false") {
		test.Errorf("doctor output missing 'enabled false' row:\n%s", output)
	}

	if !strings.Contains(output, "weight                0.30") {
		test.Errorf("doctor output missing 'weight 0.30' row:\n%s", output)
	}

	if !strings.Contains(output, "references") {
		test.Errorf("doctor output missing 'references' edge type in pane:\n%s", output)
	}

	if !strings.Contains(output, "hint:") {
		test.Errorf("doctor output missing the --explain hint:\n%s", output)
	}

	// Pass 4: toggle the manifest to enabled=true and re-run doctor to
	// confirm the renderer updates accordingly. This also covers the
	// "enabled with all known edge types → no warning Issues" path.
	enabledManifest := strings.Replace(manifestBody, "enabled = false", "enabled = true", 1)

	if writeErr := os.WriteFile(manifestPath, []byte(enabledManifest), 0o644); writeErr != nil {
		test.Fatalf("rewrite manifest: %v", writeErr)
	}

	doctorOut2 := &bytes.Buffer{}
	doctorCmd2 := newRootCmd()
	doctorCmd2.SetOut(doctorOut2)
	doctorCmd2.SetErr(doctorOut2)
	doctorCmd2.SetArgs([]string{"doctor"})

	if execErr := doctorCmd2.Execute(); execErr != nil {
		test.Fatalf("doctor (enabled): %v\n%s", execErr, doctorOut2.String())
	}

	output2 := doctorOut2.String()

	if !strings.Contains(output2, "enabled               true") {
		test.Errorf("doctor output (enabled toggled on) missing 'enabled true':\n%s", output2)
	}

	if strings.Contains(output2, "graph-expansion-unknown-edge") {
		test.Errorf("doctor output (enabled toggled on) reported unknown edge — `references` is declared in the manifest:\n%s", output2)
	}
}

// runQueryJSON executes `tusk query` with --json and returns the
// decoded result list. Fails the test on non-zero exit or invalid JSON.
func runQueryJSON(test *testing.T, label string, args []string) []map[string]any {
	test.Helper()

	out := &bytes.Buffer{}
	cmd := newRootCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)

	full := append([]string{"query"}, args...)
	full = append(full, "--json")
	cmd.SetArgs(full)

	if execErr := cmd.Execute(); execErr != nil {
		test.Fatalf("query %s: %v\n%s", label, execErr, out.String())
	}

	var results []map[string]any

	if jsonErr := json.Unmarshal(out.Bytes(), &results); jsonErr != nil {
		test.Fatalf("query %s: json unmarshal: %v\n%s", label, jsonErr, out.String())
	}

	return results
}

// idsOf extracts the "id" string field from each row for test diagnostics.
func idsOf(rows []map[string]any) []string {
	ids := make([]string, 0, len(rows))

	for _, row := range rows {
		if id, ok := row["id"].(string); ok {
			ids = append(ids, id)
		}
	}

	return ids
}

// indexOf returns the position of want in ids, or -1 when absent.
func indexOf(ids []string, want string) int {
	for index, id := range ids {
		if id == want {
			return index
		}
	}

	return -1
}
