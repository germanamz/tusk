package mcp_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/mcp"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func helpToolText(test *testing.T, srv *mcp.Server, args map[string]any) string {
	test.Helper()

	result, callErr := srv.HandleToolCall(context.Background(), mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Name:      "tusk_help",
			Arguments: args,
		},
	})

	if callErr != nil {
		test.Fatalf("HandleToolCall(tusk_help): %v", callErr)
	}

	if result.IsError {
		test.Fatalf("tusk_help returned error result: %+v", result.Content)
	}

	if len(result.Content) == 0 {
		test.Fatalf("tusk_help returned no content")
	}

	text, ok := result.Content[0].(mcpgo.TextContent)
	if !ok {
		test.Fatalf("expected TextContent, got %T", result.Content[0])
	}

	return text.Text
}

func newHelpTestServer(test *testing.T) *mcp.Server {
	test.Helper()

	root := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(`[workspace]
name = "x"
`), 0o644); writeErr != nil {
		test.Fatalf("seed manifest: %v", writeErr)
	}

	rt, openErr := mcp.Open(root)
	if openErr != nil {
		test.Fatalf("mcp.Open: %v", openErr)
	}
	test.Cleanup(func() { rt.Close() })

	return mcp.NewServer(rt)
}

func TestHelpTool_NoArgReturnsOverviewAndIndex(test *testing.T) {
	test.Parallel()

	srv := newHelpTestServer(test)
	out := helpToolText(test, srv, map[string]any{})

	// Overview content must be present.
	if !strings.Contains(out, "Tusk overview") {
		test.Errorf("expected overview heading, got:\n%s", out)
	}

	// Topic index must enumerate every known topic.
	for _, topic := range []string{
		"overview", "workflow", "node-types", "edge-types",
		"manifest", "filter", "query", "packs",
	} {
		if !strings.Contains(out, "- "+topic) {
			test.Errorf("topic %q missing from index, got:\n%s", topic, out)
		}
	}
}

func TestHelpTool_KnownTopicReturnsContent(test *testing.T) {
	test.Parallel()

	srv := newHelpTestServer(test)

	cases := map[string]string{
		"workflow":   "Typical agent workflow",
		"node-types": "Node types",
		"edge-types": "Edge types",
		"manifest":   "Manifest (tusk.toml)",
		"filter":     "Filter grammar",
		"query":      "Three modes",
		"packs":      "Type packs",
	}

	for topic, marker := range cases {
		test.Run(topic, func(test *testing.T) {
			test.Parallel()
			out := helpToolText(test, srv, map[string]any{"topic": topic})

			if !strings.Contains(out, marker) {
				test.Errorf("topic %q output missing marker %q, got:\n%s", topic, marker, out)
			}
		})
	}
}

func TestHelpTool_UnknownTopicReturnsIndexWithHint(test *testing.T) {
	test.Parallel()

	srv := newHelpTestServer(test)
	out := helpToolText(test, srv, map[string]any{"topic": "nonsense"})

	if !strings.Contains(out, `Unknown topic "nonsense"`) {
		test.Errorf("expected unknown-topic hint, got:\n%s", out)
	}

	if !strings.Contains(out, "Available tusk_help topics") {
		test.Errorf("expected topic index in unknown-topic response, got:\n%s", out)
	}
}

func TestHelpTool_BlankTopicTreatedAsNoArg(test *testing.T) {
	test.Parallel()

	srv := newHelpTestServer(test)
	out := helpToolText(test, srv, map[string]any{"topic": "   "})

	if !strings.Contains(out, "Tusk overview") {
		test.Errorf("expected overview for whitespace-only topic, got:\n%s", out)
	}
}

func TestHelpTool_RegisteredAlongsideOtherTools(test *testing.T) {
	test.Parallel()

	srv := newHelpTestServer(test)

	names := srv.RegisteredToolNames()

	if !slices.Contains(names, "tusk_help") {
		test.Errorf("tusk_help not in registered tools: %v", names)
	}
}
