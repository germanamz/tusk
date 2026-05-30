package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/mcp"
	"github.com/germanamz/tusk/internal/node"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// seedNode inserts a minimal node row directly into the runtime's index — for
// MCP cases that need pre-existing nodes (status counts, node_get/list of
// existing nodes) without driving a full reindex or writing a file.
func seedNode(test *testing.T, rt *mcp.Runtime, nodeID, nodeType string) {
	test.Helper()

	if upErr := rt.Nodes.Upsert(index.NodeRow{
		ID:             nodeID,
		Type:           nodeType,
		Path:           nodeID + ".md",
		PropertiesJSON: "{}",
		LastChecksum:   "x",
	}); upErr != nil {
		test.Fatalf("seed node %s: %v", nodeID, upErr)
	}
}

// createRealNode writes a node through the runtime's NodeService — file plus
// index row — for tools that mutate the file on disk (modify/move/delete).
func createRealNode(test *testing.T, rt *mcp.Runtime, relPath, nodeType, title string) {
	test.Helper()

	if _, createErr := rt.NodeService.Create(node.CreateInput{
		RelPath: relPath,
		Type:    nodeType,
		Title:   title,
	}); createErr != nil {
		test.Fatalf("create node %s: %v", relPath, createErr)
	}
}

// Shared harness for the MCP golden suite (Initiative 1). Pins the MCP wire
// surface byte-for-byte so Initiative 2 refactors validate against a frozen
// baseline. Holds the determinism scrubber, the runtime builder, the table
// runner, the raw tool caller, and a dependency-free diff. Cases live in
// per-area files (golden_node_test.go, golden_edge_test.go, ...), each a thin
// TestGoldenMCP_<Area> that calls runGoldenMCPCases.

var (
	reReindexTimestamp = regexp.MustCompile(`last reindex \(unix ns\): \d+`)
	reLastReindexAt    = regexp.MustCompile(`"last_reindex_at":\s*"?[^",}]*"?`)
	rePackAddDate      = regexp.MustCompile(`on \d{4}-\d{2}-\d{2}`)
	reMillis           = regexp.MustCompile(`\b\d+ms\b`)
)

// scrub neutralizes non-deterministic substrings in tool output so a golden
// literal stays stable across runs. See the determinism plan (spec §3.3).
// Passing wsRoot == "" skips the workspace-path rewrite.
func scrub(text, wsRoot string) string {
	if wsRoot != "" {
		text = strings.ReplaceAll(text, wsRoot, "<WS>")
	}

	text = reReindexTimestamp.ReplaceAllString(text, "last reindex (unix ns): <TS>")
	text = reLastReindexAt.ReplaceAllString(text, `"last_reindex_at":"<TS>"`)
	text = rePackAddDate.ReplaceAllString(text, "on <DATE>")
	text = reMillis.ReplaceAllString(text, "<MS>")

	return text
}

// goldenRuntime builds a deterministic MCP runtime for a golden case. It pins
// the embed-worker count and lease TTL via env knobs, writes manifestBody to
// tusk.toml (an empty body uses a minimal default), and opens the runtime. The
// runtime is closed automatically on test cleanup.
func goldenRuntime(test *testing.T, manifestBody string) *mcp.Runtime {
	test.Helper()

	test.Setenv("TUSK_EMBED_WORKERS", "1")
	test.Setenv("TUSK_LEASE_TTL_SECONDS", "3600")

	root := test.TempDir()

	if manifestBody == "" {
		manifestBody = "[workspace]\nname = \"test\"\n"
	}

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifestBody), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	test.Cleanup(func() { _ = rt.Close() })

	return rt
}

// goldenMCPCase is one row of an MCP golden table. It pins the error encoding
// (plain toolError text vs structured toolJSONError JSON body) and the exact
// text/body, so a refactor that swaps one encoding for the other — or drifts a
// field — is caught.
type goldenMCPCase struct {
	name        string
	manifest    string // tusk.toml body; "" uses a minimal default
	setup       func(test *testing.T, rt *mcp.Runtime)
	tool        string
	args        map[string]any
	wantIsError bool
	wantJSONObj bool   // body must parse as a JSON object (toolJSONError) vs plain text (toolError)
	wantText    string // exact result text after scrubbing
}

// runGoldenMCPCases drives each case through the in-process fast tier
// (HandleToolCall via rawToolResult) and asserts the error flag, the encoding
// kind (plain vs JSON object), and the byte-stable result text.
func runGoldenMCPCases(test *testing.T, cases []goldenMCPCase) {
	test.Helper()

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			rt := goldenRuntime(test, testCase.manifest)

			if testCase.setup != nil {
				testCase.setup(test, rt)
			}

			srv := mcp.NewServer(rt)

			text, isError := rawToolResult(test, srv, testCase.tool, testCase.args)

			if isError != testCase.wantIsError {
				test.Errorf("IsError = %v, want %v\ntext:\n%s", isError, testCase.wantIsError, text)
			}

			if gotJSONObj := looksLikeJSONObject(text); gotJSONObj != testCase.wantJSONObj {
				test.Errorf("JSON-object body = %v, want %v\ntext:\n%s", gotJSONObj, testCase.wantJSONObj, text)
			}

			// Thread rt.Root so any absolute workspace path a tool body or
			// error leaks collapses to <WS> (e.g. node_get's file-not-found
			// error), keeping goldens machine-independent.
			if diff := goldenDiff(scrub(testCase.wantText, rt.Root), scrub(text, rt.Root)); diff != "" {
				test.Errorf("%s", diff)
			}
		})
	}
}

// rawToolResult invokes the tool handler directly and returns its raw result
// text and IsError flag, without the JSON-decoding that callTool applies. This
// is what lets a golden case observe the two error encodings — callTool reduces
// both to an opaque Go error.
func rawToolResult(test *testing.T, srv *mcp.Server, name string, args map[string]any) (string, bool) {
	test.Helper()

	request := mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Name:      name,
			Arguments: args,
		},
	}

	result, callErr := srv.HandleToolCall(context.Background(), request)

	if callErr != nil {
		test.Fatalf("HandleToolCall %s: %v", name, callErr)
	}

	if len(result.Content) == 0 {
		return "", result.IsError
	}

	text, ok := result.Content[0].(mcpgo.TextContent)

	if !ok {
		test.Fatalf("expected TextContent, got %T", result.Content[0])
	}

	return text.Text, result.IsError
}

// looksLikeJSONObject reports whether text parses as a JSON object — the
// observable difference between a structured toolJSONError body and a plain
// toolError message.
func looksLikeJSONObject(text string) bool {
	var obj map[string]any

	return json.Unmarshal([]byte(text), &obj) == nil
}

// goldenDiff returns a human-readable description of the first per-line
// differences between want and got, or "" when they are byte-identical. It is a
// dependency-free stand-in for cmp.Diff.
func goldenDiff(want, got string) string {
	if want == got {
		return ""
	}

	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")

	maxLines := max(len(wantLines), len(gotLines))

	var builder strings.Builder

	builder.WriteString("golden mismatch (-want +got):\n")

	for idx := range maxLines {
		wantLine := ""

		if idx < len(wantLines) {
			wantLine = wantLines[idx]
		}

		gotLine := ""

		if idx < len(gotLines) {
			gotLine = gotLines[idx]
		}

		if wantLine == gotLine {
			continue
		}

		fmt.Fprintf(&builder, "  line %d:\n    -want: %q\n    +got:  %q\n", idx+1, wantLine, gotLine)
	}

	return builder.String()
}
