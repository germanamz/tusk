package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestParseStream(test *testing.T) {
	file, openErr := os.Open("testdata/stream.jsonl")

	if openErr != nil {
		test.Fatal(openErr)
	}

	defer file.Close()

	summary, parseErr := ParseStream(file)

	if parseErr != nil {
		test.Fatal(parseErr)
	}

	if summary.Model != "claude-sonnet-5-5" {
		test.Errorf("model = %q", summary.Model)
	}

	if strings.Join(summary.Tools, ",") != "Read,StructuredOutput,mcp__tusk__tusk_query" {
		test.Errorf("tools = %v", summary.Tools)
	}

	if summary.MCPServers["tusk"] != "connected" {
		test.Errorf("mcp servers = %v", summary.MCPServers)
	}

	// The Read call is emitted twice with one id; StructuredOutput is not
	// a retrieval call.
	if summary.ToolCalls["mcp__tusk__tusk_query"] != 1 || summary.ToolCalls["Read"] != 1 || len(summary.ToolCalls) != 2 {
		test.Errorf("tool calls = %v", summary.ToolCalls)
	}

	if summary.TotalToolCalls() != 2 {
		test.Errorf("total tool calls = %d, want 2", summary.TotalToolCalls())
	}

	// 15 bytes from the array result, 20 from the string one; the
	// StructuredOutput acknowledgement doesn't count.
	if summary.ToolOutputBytes != 35 {
		test.Errorf("tool output bytes = %d, want 35", summary.ToolOutputBytes)
	}

	if summary.Result == nil {
		test.Fatal("no result")
	}

	// Totals come from the result event, not the repeated per-block usage.
	if got := summary.Result.Usage.TotalInput(); got != 6+13078+19905 {
		test.Errorf("total input = %d", got)
	}

	var answer struct {
		Citations []Citation `json:"citations"`
	}

	if decodeErr := json.Unmarshal(summary.Result.StructuredOutput, &answer); decodeErr != nil {
		test.Fatal(decodeErr)
	}

	if len(answer.Citations) != 1 || answer.Citations[0].Heading != "Public surface" {
		test.Errorf("citations = %+v", answer.Citations)
	}
}

func TestParseStreamWithoutResult(test *testing.T) {
	summary, parseErr := ParseStream(strings.NewReader(`{"type":"system","subtype":"init","tools":["Read"]}` + "\n"))

	if parseErr != nil {
		test.Fatal(parseErr)
	}

	if summary.Result != nil {
		test.Error("expected no result event")
	}
}
