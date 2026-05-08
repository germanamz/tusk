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
