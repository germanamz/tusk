package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/mcp"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func TestNodeRender_CLIMatchesMCP(test *testing.T) {
	root := initWorkspace(test)

	create := newRootCmd()
	create.SetArgs([]string{"node", "create", "--type", "note", "--title", "Parity", "--path", "notes/p.md"})

	if execErr := create.Execute(); execErr != nil {
		test.Fatalf("create: %v", execErr)
	}

	html := "<html><head><meta name=\"tusk:type\" content=\"note\"></head>" +
		"<body><h1>Doc</h1><p>Some &amp; prose.</p></body></html>"

	if writeErr := os.WriteFile(filepath.Join(root, "page.html"), []byte(html), 0o644); writeErr != nil {
		test.Fatalf("write html: %v", writeErr)
	}

	reindex := newRootCmd()
	reindex.SetArgs([]string{"reindex"})

	if execErr := reindex.Execute(); execErr != nil {
		test.Fatalf("reindex: %v", execErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("mcp.Open: %v", openErr)
	}

	defer rt.Close()

	srv := mcp.NewServer(rt)

	for _, id := range []string{"notes/p", "page.html"} {
		cliOut := &bytes.Buffer{}
		cliCmd := newRootCmd()
		cliCmd.SetOut(cliOut)
		cliCmd.SetErr(cliOut)
		cliCmd.SetArgs([]string{"node", "render", id})

		if execErr := cliCmd.Execute(); execErr != nil {
			test.Fatalf("cli render %s: %v\n%s", id, execErr, cliOut.String())
		}

		result, callErr := srv.HandleToolCall(context.Background(), mcpgo.CallToolRequest{
			Params: mcpgo.CallToolParams{Name: "tusk_node_render", Arguments: map[string]any{"id": id}},
		})

		if callErr != nil || result.IsError {
			test.Fatalf("mcp render %s: callErr=%v isError=%v", id, callErr, result.IsError)
		}

		mcpText := result.Content[0].(mcpgo.TextContent).Text

		// The CLI appends a trailing newline (Fprintln); the MCP tool returns
		// the raw NodeText. Compare on the trimmed prose so the two surfaces
		// are asserted to render identical content.
		if strings.TrimRight(cliOut.String(), "\n") != strings.TrimRight(mcpText, "\n") {
			test.Errorf("parity mismatch for %s:\nCLI:  %q\nMCP:  %q", id, cliOut.String(), mcpText)
		}
	}

	_ = index.NodeRow{} // index imported for symmetry with other parity tests
}
