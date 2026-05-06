package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// e2eClient is a tiny JSON-RPC client driving stdin/stdout of `tusk mcp`.
type e2eClient struct {
	stdin  io.Writer
	stdout *bufio.Reader
}

func (client *e2eClient) call(method string, params map[string]any) (map[string]any, error) {
	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      time.Now().UnixNano(),
		"method":  method,
		"params":  params,
	}

	body, _ := json.Marshal(request)

	if _, writeErr := client.stdin.Write(append(body, '\n')); writeErr != nil {
		return nil, writeErr
	}

	line, readErr := client.stdout.ReadBytes('\n')

	if readErr != nil {
		return nil, readErr
	}

	var response map[string]any

	if unmarshalErr := json.Unmarshal(line, &response); unmarshalErr != nil {
		return nil, unmarshalErr
	}

	return response, nil
}

func TestE2E_MCPStdioSession(test *testing.T) {
	if testing.Short() {
		test.Skip("e2e: skip in short mode")
	}

	root := setupTempWorkspace(test)
	repo := repoRoot(test) // capture before any chdir

	// Build the binary into a temp dir so Dir = workspace root works cleanly.
	binDir := test.TempDir()
	binPath := filepath.Join(binDir, "tusk")

	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/tusk")
	buildCmd.Dir = repo

	if buildOut, buildErr := buildCmd.CombinedOutput(); buildErr != nil {
		test.Fatalf("build: %v\n%s", buildErr, buildOut)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mcpCmd := exec.CommandContext(ctx, binPath, "mcp")
	mcpCmd.Dir = root

	stdin, stdinErr := mcpCmd.StdinPipe()

	if stdinErr != nil {
		test.Fatalf("StdinPipe: %v", stdinErr)
	}

	stdout, stdoutErr := mcpCmd.StdoutPipe()

	if stdoutErr != nil {
		test.Fatalf("StdoutPipe: %v", stdoutErr)
	}

	if startErr := mcpCmd.Start(); startErr != nil {
		test.Fatalf("start: %v", startErr)
	}

	defer func() {
		stdin.Close()
		_ = mcpCmd.Wait()
	}()

	client := &e2eClient{stdin: stdin, stdout: bufio.NewReader(stdout)}

	// Step 1: initialize
	if _, callErr := client.call("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "tusk-e2e", "version": "0.0.1"},
	}); callErr != nil {
		test.Fatalf("initialize: %v", callErr)
	}

	// Step 2: list tools — expect at least 10 registered
	listResponse, listErr := client.call("tools/list", map[string]any{})

	if listErr != nil {
		test.Fatalf("tools/list: %v", listErr)
	}

	resultMap, _ := listResponse["result"].(map[string]any)
	tools, _ := resultMap["tools"].([]any)

	if len(tools) < 10 {
		test.Errorf("expected >=10 tools registered, got %d", len(tools))
	}

	// Step 3: tusk_node_create — create a note
	if _, callErr := client.call("tools/call", map[string]any{
		"name": "tusk_node_create",
		"arguments": map[string]any{
			"path":  "notes/e2e.md",
			"type":  "note",
			"title": "E2E",
		},
	}); callErr != nil {
		test.Fatalf("tusk_node_create: %v", callErr)
	}

	// Step 4: tusk_query — expect 1 result
	queryResponse, queryErr := client.call("tools/call", map[string]any{
		"name": "tusk_query",
		"arguments": map[string]any{
			"filter": "type=note",
		},
	})

	if queryErr != nil {
		test.Fatalf("tusk_query: %v", queryErr)
	}

	queryResult, _ := queryResponse["result"].(map[string]any)
	contents, _ := queryResult["content"].([]any)

	if len(contents) == 0 {
		test.Fatalf("query returned no content")
	}

	textBody := contents[0].(map[string]any)["text"].(string)

	var queryBody map[string]any

	if unmarshalErr := json.Unmarshal([]byte(textBody), &queryBody); unmarshalErr != nil {
		test.Fatalf("unmarshal query body: %v", unmarshalErr)
	}

	if int(queryBody["count"].(float64)) != 1 {
		test.Errorf("count = %v, want 1", queryBody["count"])
	}

	// Step 5: ensure the file is on disk (verifies workspace lock didn't deadlock)
	if _, statErr := os.Stat(filepath.Join(root, "notes", "e2e.md")); statErr != nil {
		test.Errorf("notes/e2e.md not on disk: %v", statErr)
	}
}
