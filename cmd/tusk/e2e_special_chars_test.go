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

	"github.com/germanamz/tusk/internal/index"
)

// TestE2E_SpecialCharacterFileIDs drives the whole CLI over a workspace whose
// filenames exercise the id-alphabet edges from #683: a `[wip]` bracket name, a
// `#` in the name, a `reindex:` prefix, and an emoji. It pins the four findings
// end-to-end:
//
//   - Bracket ids ('[' is a SQLite GLOB character class): the bracket file must
//     stay visible to semantic search, and deleting a paragraph must remove its
//     sub-unit row rather than leaving an immortal ghost (findings 1 and 2).
//   - '#' ids collide with the sub-unit separator: the file is skipped, never
//     indexed, and does not wedge the pass (finding 3).
//   - a 'reindex:' prefix collides with the embed-queue key namespace: the file
//     is skipped and neither `reindex` nor `reindex --force` errors (finding 4).
//   - an emoji name is legal and indexes normally (the id alphabet is not
//     over-restricted).
func TestE2E_SpecialCharacterFileIDs(test *testing.T) {
	tmpDir := initWorkspace(test)

	// Deterministic embedder: the vector is chosen by the first letter of the
	// text, so 'a' content and an 'a' query align while 'b' content does not.
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Prompt string `json:"prompt"`
		}

		_ = json.NewDecoder(request.Body).Decode(&payload)

		first := byte(0)

		for i := 0; i < len(payload.Prompt); i++ {
			c := payload.Prompt[i]
			if c > ' ' {
				first = c
				break
			}
		}

		vector := []float64{0, 0, 0}

		switch first {
		case 'a', 'A':
			vector[0] = 1
		case 'b', 'B':
			vector[1] = 1
		default:
			vector[2] = 1
		}

		_ = json.NewEncoder(writer).Encode(map[string]any{"embedding": vector})
	}))

	defer server.Close()

	manifestBody := `[workspace]
name = "test"

[node-types.note]
properties = []

[embeddings]
provider = "ollama"
model = "test-model"
endpoint = "` + server.URL + `"
dim = 3
`

	if writeErr := os.WriteFile(filepath.Join(tmpDir, "tusk.toml"), []byte(manifestBody), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	// Written directly (not via `node create`, which now refuses reserved ids)
	// so the reindex walk sees the reserved files on disk.
	files := map[string]string{
		"notes/[wip] alpha.md": "---\ntype: note\ntitle: Alpha\n---\n\n# Alpha\n\napple orchard planting notes for the season.\n\nautumn harvest schedule and cold storage plans.\n",
		"notes/garden.md":      "---\ntype: note\ntitle: Beta\n---\n\n# Beta\n\nbananas ripen quickly in the greenhouse.\n\nbeetroot beds need weekly weeding.\n",
		"notes/y#S1.md":        "---\ntype: note\ntitle: Hash\n---\n\nA standalone file whose name contains a hash.\n",
		"reindex:notes.md":     "---\ntype: note\ntitle: Reserved\n---\n\nA file whose name uses the reserved reindex prefix.\n",
		"notes/🚀 emoji.md":     "---\ntype: note\ntitle: Emoji\n---\n\ncosmic rocket launch checklist.\n",
	}

	for relPath, content := range files {
		abs := filepath.Join(tmpDir, relPath)

		if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
			test.Fatalf("mkdir %s: %v", relPath, mkErr)
		}

		if writeErr := os.WriteFile(abs, []byte(content), 0o644); writeErr != nil {
			test.Fatalf("write %s: %v", relPath, writeErr)
		}
	}

	runReindex := func(args ...string) {
		test.Helper()

		out := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs(append([]string{"reindex"}, args...))

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("reindex %v wedged: %v\n%s", args, execErr, out.String())
		}
	}

	dbPath := filepath.Join(tmpDir, ".tusk", "index.db")

	// countSubUnits returns the number of sub-unit rows whose id belongs to
	// fileID, matched in Go so the test itself never hits the `[`-as-metachar
	// trap the fix removes from the SQL layer.
	countSubUnits := func(fileID string) int {
		store, openErr := index.Open(dbPath)

		if openErr != nil {
			test.Fatalf("open index: %v", openErr)
		}

		defer store.Close()

		rows, queryErr := store.DB().Query(`SELECT id FROM nodes WHERE kind = 'subunit'`)

		if queryErr != nil {
			test.Fatalf("query sub-units: %v", queryErr)
		}

		defer rows.Close()

		prefix := fileID + "#"
		count := 0

		for rows.Next() {
			var id string

			if scanErr := rows.Scan(&id); scanErr != nil {
				test.Fatalf("scan: %v", scanErr)
			}

			if strings.HasPrefix(id, prefix) {
				count++
			}
		}

		return count
	}

	nodeExists := func(id string) bool {
		store, openErr := index.Open(dbPath)

		if openErr != nil {
			test.Fatalf("open index: %v", openErr)
		}

		defer store.Close()

		if _, getErr := index.NewNodeRepo(store).Get(id); getErr != nil {
			return false
		}

		return true
	}

	runReindex()

	// The legal names index; the reserved names do not.
	for _, id := range []string{"notes/[wip] alpha", "notes/garden", "notes/🚀 emoji"} {
		if !nodeExists(id) {
			test.Errorf("expected %q to be indexed", id)
		}
	}

	for _, id := range []string{"notes/y#S1", "reindex:notes"} {
		if nodeExists(id) {
			test.Errorf("reserved-id file %q must not be indexed", id)
		}
	}

	// The bracket file's second paragraph is a live sub-unit.
	before := countSubUnits("notes/[wip] alpha")

	if before < 2 {
		test.Fatalf("bracket file sub-units before edit = %d, want >= 2 (a section + two paragraphs)", before)
	}

	// Semantic visibility: the bracket file's own sub-unit must rank for an
	// 'a' query. Under the old GLOB path its leaves never loaded and it was
	// absent from every semantic result.
	assertSemanticHit := func(prompt, wantPathSubstr string) {
		test.Helper()

		out := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"query", "type=note", "--semantic", prompt, "--json"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("semantic query %q: %v\n%s", prompt, execErr, out.String())
		}

		var results []map[string]any

		if jsonErr := json.Unmarshal(out.Bytes(), &results); jsonErr != nil {
			test.Fatalf("unmarshal semantic results: %v\n%s", jsonErr, out.String())
		}

		for _, result := range results {
			if pathVal, ok := result["path"].(string); ok && strings.Contains(pathVal, wantPathSubstr) {
				return
			}
		}

		test.Errorf("semantic query %q did not surface a result whose path contains %q; got %s", prompt, wantPathSubstr, out.String())
	}

	assertSemanticHit("apple orchard planting", "[wip] alpha.md")

	// Drop the bracket file's second paragraph and reindex. Its sub-unit must
	// be deleted, not left as an immortal ghost.
	if writeErr := os.WriteFile(
		filepath.Join(tmpDir, "notes/[wip] alpha.md"),
		[]byte("---\ntype: note\ntitle: Alpha\n---\n\n# Alpha\n\napple orchard planting notes for the season.\n"),
		0o644,
	); writeErr != nil {
		test.Fatalf("rewrite bracket file: %v", writeErr)
	}

	runReindex()

	after := countSubUnits("notes/[wip] alpha")

	if after >= before {
		test.Errorf("bracket file sub-units after deleting a paragraph = %d, want < %d (deleted sub-unit must not be immortal)", after, before)
	}

	// The bracket file still ranks after the edit (its remaining leaf is 'a').
	assertSemanticHit("apple orchard planting", "[wip] alpha.md")

	// --force re-enqueues everything; the reserved files must still not wedge it.
	runReindex("--force")
}
