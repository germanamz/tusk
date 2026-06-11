package mcp_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/mcp"
	"github.com/germanamz/tusk/internal/node"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// callToolText runs a tool and returns its raw text content (the render tool
// returns plain prose, not a JSON envelope).
func callToolText(test *testing.T, srv *mcp.Server, name string, args map[string]any) string {
	test.Helper()

	result, callErr := srv.HandleToolCall(context.Background(), mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{Name: name, Arguments: args},
	})

	if callErr != nil {
		test.Fatalf("%s: %v", name, callErr)
	}

	if result.IsError {
		test.Fatalf("%s returned error: %v", name, fmtError(result))
	}

	textContent, ok := result.Content[0].(mcpgo.TextContent)

	if !ok {
		test.Fatalf("expected TextContent, got %T", result.Content[0])
	}

	return textContent.Text
}

func TestTool_NodeRender_Markdown(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	if _, createErr := rt.NodeService.Create(node.CreateInput{
		RelPath: "notes/hi.md",
		Type:    "note",
		Title:   "Hi",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	srv := mcp.NewServer(rt)

	got := callToolText(test, srv, "tusk_node_render", map[string]any{"id": "notes/hi"})

	if strings.Contains(got, "type: note") {
		test.Errorf("render leaked frontmatter: %q", got)
	}
}

func TestTool_NodeRender_HTML(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	html := "<html><body><h1>Greeting</h1><p>Hello world.</p></body></html>"

	if writeErr := os.WriteFile(filepath.Join(rt.Root, "page.html"), []byte(html), 0o644); writeErr != nil {
		test.Fatalf("write html: %v", writeErr)
	}

	rt.Nodes.Upsert(index.NodeRow{ID: "page.html", Type: "note", Path: "page.html", PropertiesJSON: "{}", LastChecksum: "x"})

	srv := mcp.NewServer(rt)

	got := callToolText(test, srv, "tusk_node_render", map[string]any{"id": "page.html"})

	if strings.Contains(got, "<") {
		test.Errorf("render left tags: %q", got)
	}

	for _, word := range []string{"Greeting", "Hello", "world"} {
		if !strings.Contains(got, word) {
			test.Errorf("render dropped %q: %q", word, got)
		}
	}
}
