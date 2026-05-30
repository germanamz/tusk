package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/germanamz/tusk/internal/mcp"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// goldenMCPCase is one row of the MCP golden table. It pins both the error
// encoding (plain toolError text vs structured toolJSONError JSON body) and the
// exact text/body, so a refactor that swaps one encoding for the other — or
// drifts a field — is caught.
type goldenMCPCase struct {
	name        string
	manifest    string // tusk.toml body; "" uses a minimal default
	tool        string
	args        map[string]any
	wantIsError bool
	wantJSONObj bool   // body must parse as a JSON object (toolJSONError) vs plain text (toolError)
	wantText    string // exact result text after scrubbing
}

// TestGoldenMCP pins the two MCP error encodings — the highest-risk trap on the
// MCP side, because callTool collapses both into a Go error and so cannot tell
// them apart. The wire contract distinguishes a plain-text toolError
// (IsError + non-JSON text) from a structured toolJSONError (IsError + a JSON
// object body); both must stay byte-stable across the Initiative 2 refactors.
func TestGoldenMCP(test *testing.T) {
	const ticketManifest = `[workspace]
name = "test"

[node-types.ticket]
properties = [
    { name = "summary", type = "string", required = true },
]
`

	cases := []goldenMCPCase{
		{
			name:        "node_get missing id is a plain-text error",
			tool:        "tusk_node_get",
			args:        map[string]any{},
			wantIsError: true,
			wantJSONObj: false,
			wantText:    `missing or non-string argument "id"`,
		},
		{
			name:        "node_create property rejection is a structured JSON error",
			manifest:    ticketManifest,
			tool:        "tusk_node_create",
			args:        map[string]any{"type": "ticket", "path": "tickets/foo.md"},
			wantIsError: true,
			wantJSONObj: true,
			wantText:    goldenNodeTypesRejection,
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			rt := goldenRuntime(test, testCase.manifest)
			srv := mcp.NewServer(rt)

			text, isError := rawToolResult(test, srv, testCase.tool, testCase.args)

			if isError != testCase.wantIsError {
				test.Errorf("IsError = %v, want %v\ntext:\n%s", isError, testCase.wantIsError, text)
			}

			if gotJSONObj := looksLikeJSONObject(text); gotJSONObj != testCase.wantJSONObj {
				test.Errorf("JSON-object body = %v, want %v\ntext:\n%s", gotJSONObj, testCase.wantJSONObj, text)
			}

			got := scrub(text, "")
			want := scrub(testCase.wantText, "")

			if diff := goldenDiff(want, got); diff != "" {
				test.Errorf("%s", diff)
			}
		})
	}
}

// goldenNodeTypesRejection is the exact JSON body tusk_node_create returns when
// a required property is missing. json.Marshal sorts map keys, so this literal
// is deterministic regardless of map iteration order.
const goldenNodeTypesRejection = `{"error":"node-types-rejection","errors":[{"kind":"required-missing","property":"summary","reason":"is required (declared in [node-types.ticket])","type":"string"}],"node_id":"tickets/foo","node_type":"ticket","op":"create"}`

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
