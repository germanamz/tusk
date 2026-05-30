package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// queryManifest disables sub-units so structural rows aren't padded with
// paragraph sub-units — keeps the query goldens focused on the matched nodes.
const queryManifest = "[workspace]\nname = \"test\"\nsub-units = false\n"

// startStubEmbedder serves a deterministic 3-dim embedder: the vector is chosen
// by the prompt's first byte (a/A→x, b/B→y, else z), so orthogonal fixtures
// yield exact 0.0/1.0 cosine scores. Closed on test cleanup.
func startStubEmbedder(test *testing.T) *httptest.Server {
	test.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Prompt string `json:"prompt"`
		}

		_ = json.NewDecoder(request.Body).Decode(&payload)

		first := byte(0)

		if len(payload.Prompt) > 0 {
			first = payload.Prompt[0]
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

	test.Cleanup(server.Close)

	return server
}

// semanticFixture wires the stub embedder into [embeddings], writes two
// orthogonal-bodied notes, and reindexes (which embeds them via the stub).
func semanticFixture(test *testing.T, root string) {
	test.Helper()

	server := startStubEmbedder(test)

	writeFile(test, root, "tusk.toml", "[workspace]\nname = \"test\"\nsub-units = false\n\n"+
		"[embeddings]\nprovider = \"ollama\"\nmodel = \"test-model\"\nendpoint = \""+server.URL+"\"\ndim = 3\n")
	writeFile(test, root, "notes/apple.md", "---\ntype: note\ntitle: Apple\n---\n\nApples are great.\n")
	writeFile(test, root, "notes/banana.md", "---\ntype: note\ntitle: Banana\n---\n\nBananas are yellow.\n")
	reindexWorkspace(test, root)
}

// TestGoldenCLI_Query pins structural query output (the [] sentinel, the --json
// envelope, the legacy table) and the semantic/filter/graph-expansion error paths.
func TestGoldenCLI_Query(test *testing.T) {
	runGoldenCLICases(test, []goldenCLICase{
		{
			name:       "structural query with no matches emits the [] sentinel",
			manifest:   queryManifest,
			args:       []string{"query", "type=ghost", "--json"},
			wantStdout: "[]\n",
		},
		{
			// Legacy contract: --json emits the [] sentinel even WITH matches
			// when no rows carry expansions and no fields are projected.
			name:       "structural --json emits the sentinel without expansions",
			manifest:   queryManifest,
			setup:      queryFixture,
			args:       []string{"query", "type=note", "--json"},
			wantStdout: "[]\n",
		},
		{
			name:     "structural --include body --json returns rows",
			manifest: queryManifest,
			setup:    queryFixture,
			args:     []string{"query", "type=note", "--include", "body", "--json"},
			wantStdout: `[
  {
    "id": "notes/a",
    "type": "note",
    "path": "notes/a.md",
    "title": "A",
    "body": "---\ntype: note\ntitle: A\n---\n\nAlpha.\n"
  },
  {
    "id": "notes/b",
    "type": "note",
    "path": "notes/b.md",
    "title": "B",
    "body": "---\ntype: note\ntitle: B\n---\n\nBeta.\n"
  }
]
`,
		},
		{
			name:     "structural query renders the legacy table",
			manifest: queryManifest,
			setup:    queryFixture,
			args:     []string{"query", "type=note"},
			wantStdout: "ID       TYPE  TITLE  PATH\n" +
				"notes/a  note  A      notes/a.md\n" +
				"notes/b  note  B      notes/b.md\n",
		},
		{
			name:       "semantic without [embeddings] is rejected",
			manifest:   queryManifest,
			args:       []string{"query", "type=note", "--semantic", "foo"},
			wantErr:    true,
			wantStderr: "--semantic requires [embeddings] block in tusk.toml\n",
		},
		{
			name:               "a malformed filter is rejected",
			manifest:           queryManifest,
			args:               []string{"query", "type="},
			wantErr:            true,
			wantStderrContains: "filter parse:",
		},
		{
			name:       "graph-expand hops out of range is rejected",
			manifest:   queryManifest,
			args:       []string{"query", "type=note", "--graph-expand", "--hops", "3"},
			wantErr:    true,
			wantStderr: "--hops must be 1 or 2 (got 3)\n",
		},
		{
			// Pins the format contract: semantic --json is a single-line
			// Encoder array with score + snippet keys, distinct from the
			// structural MarshalIndent shape above. (Ranking correctness is
			// covered structurally by e2e_semantic_test.go.) Tie-broken by
			// NodeID, so apple precedes banana.
			name:       "semantic query emits single-line JSON with score and snippet",
			setup:      semanticFixture,
			args:       []string{"query", "type=note", "--semantic", "apple", "--json"},
			wantStdout: `[{"id":"notes/apple","type":"note","path":"notes/apple.md","title":"Apple","score":0,"snippet":"Apples are great."},{"id":"notes/banana","type":"note","path":"notes/banana.md","title":"Banana","score":0,"snippet":"Bananas are yellow."}]` + "\n",
		},
	})
}

// queryFixture writes two note nodes and indexes them.
func queryFixture(test *testing.T, root string) {
	test.Helper()

	writeFile(test, root, "notes/a.md", goldenNoteA)
	writeFile(test, root, "notes/b.md", goldenNoteB)
	reindexWorkspace(test, root)
}
