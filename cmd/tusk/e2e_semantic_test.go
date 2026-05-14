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

func TestE2E_SemanticRetrieval(test *testing.T) {
	tmpDir := initWorkspace(test)

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

	defer server.Close()

	manifestBody := `[workspace]
name = "test"

[embeddings]
provider = "ollama"
model = "test-model"
endpoint = "` + server.URL + `"
dim = 3
`

	if writeErr := os.WriteFile(filepath.Join(tmpDir, "tusk.toml"), []byte(manifestBody), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	for _, args := range [][]string{
		{"node", "create", "--type", "note", "--title", "Apples", "--path", "a.md"},
		{"node", "create", "--type", "note", "--title", "Bananas", "--path", "b.md"},
		{"node", "create", "--type", "note", "--title", "Cherries", "--path", "c.md"},
	} {
		cmd := newRootCmd()
		cmd.SetArgs(args)

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("setup %v: %v", args, execErr)
		}
	}

	// Each fixture's body is overwritten with quotable content so the snippet
	// assertion downstream has substance to verify.
	nodeBodies := map[string]string{
		"a.md": "Apples come in many varieties such as Fuji and Gala.",
		"b.md": "Bananas ripen quickly when stored at room temperature.",
		"c.md": "Cherries are stone fruits with a short summer season.",
	}

	for filename, bodyText := range nodeBodies {
		fullPath := filepath.Join(tmpDir, filename)

		existing, readErr := os.ReadFile(fullPath)

		if readErr != nil {
			test.Fatalf("read %s: %v", filename, readErr)
		}

		if writeErr := os.WriteFile(fullPath, []byte(string(existing)+"\n"+bodyText+"\n"), 0o644); writeErr != nil {
			test.Fatalf("write body %s: %v", filename, writeErr)
		}
	}

	reindexCmd := newRootCmd()
	reindexCmd.SetArgs([]string{"reindex"})

	if execErr := reindexCmd.Execute(); execErr != nil {
		test.Fatalf("reindex: %v", execErr)
	}

	out := &bytes.Buffer{}

	queryCmd := newRootCmd()
	queryCmd.SetOut(out)
	queryCmd.SetErr(out)
	queryCmd.SetArgs([]string{"query", "type=note", "--semantic", "apple varieties"})

	if execErr := queryCmd.Execute(); execErr != nil {
		test.Fatalf("query --semantic: %v", execErr)
	}

	body := out.String()

	firstResult := firstNonHeaderLine(body)

	if !strings.HasPrefix(firstResult, "a") {
		test.Errorf("expected 'a' to rank first, got body:\n%s", body)
	}

	if !strings.Contains(body, "SNIPPET") {
		test.Errorf("tabwriter missing SNIPPET column:\n%s", body)
	}

	// Re-run with --json and assert snippet key is present and non-empty.
	jsonOut := &bytes.Buffer{}

	jsonCmd := newRootCmd()
	jsonCmd.SetOut(jsonOut)
	jsonCmd.SetErr(jsonOut)
	jsonCmd.SetArgs([]string{"query", "type=note", "--semantic", "apple varieties", "--json"})

	if execErr := jsonCmd.Execute(); execErr != nil {
		test.Fatalf("json query: %v\n%s", execErr, jsonOut.String())
	}

	var results []map[string]any

	if jsonErr := json.Unmarshal(jsonOut.Bytes(), &results); jsonErr != nil {
		test.Fatalf("json unmarshal: %v\n%s", jsonErr, jsonOut.String())
	}

	if len(results) == 0 {
		test.Fatalf("empty json results:\n%s", jsonOut.String())
	}

	snippet, ok := results[0]["snippet"].(string)

	if !ok {
		test.Errorf("result[0] missing snippet key: %v", results[0])
	} else if snippet == "" {
		test.Errorf("result[0] snippet is empty; ensure fixture node bodies are non-empty")
	} else if !strings.Contains(snippet, "Apples") && !strings.Contains(snippet, "varieties") {
		test.Errorf("result[0] snippet content unexpected: %q", snippet)
	}
}

func firstNonHeaderLine(body string) string {
	lines := strings.Split(body, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "ID") {
			continue
		}

		return trimmed
	}

	return ""
}
