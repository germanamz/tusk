package mcp_test

import "testing"

// TestGoldenMCP_Node pins the byte-stable behavior of the tusk_node_* tools.
// The two cases here are the highest-risk MCP trap — the two error encodings —
// because callTool collapses both into a Go error and so cannot tell them
// apart. The wire contract distinguishes a plain-text toolError (IsError +
// non-JSON text) from a structured toolJSONError (IsError + a JSON object).
func TestGoldenMCP_Node(test *testing.T) {
	const ticketManifest = `[workspace]
name = "test"

[node-types.ticket]
properties = [
    { name = "summary", type = "string", required = true },
]
`

	runGoldenMCPCases(test, []goldenMCPCase{
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
	})
}

// goldenNodeTypesRejection is the exact JSON body tusk_node_create returns when
// a required property is missing. json.Marshal sorts map keys, so this literal
// is deterministic regardless of map iteration order.
const goldenNodeTypesRejection = `{"error":"node-types-rejection","errors":[{"kind":"required-missing","property":"summary","reason":"is required (declared in [node-types.ticket])","type":"string"}],"node_id":"tickets/foo","node_type":"ticket","op":"create"}`
