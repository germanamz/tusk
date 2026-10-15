package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// structuredOutputTool is the tool Claude Code adds for --json-schema. The
// agent calls it once to return its answer, so it is not a retrieval call
// and is left out of the tool counts.
const structuredOutputTool = "StructuredOutput"

// Usage is the token accounting of a run, as Claude Code reports it.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	OutputTokens             int `json:"output_tokens"`
}

// TotalInput is every input token the model read across the run, cached or
// not. Caching changes what the tokens cost, not how many were read, so this
// is the number to compare across runs that warmed the cache differently.
func (usage Usage) TotalInput() int {
	return usage.InputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens
}

// ResultEvent is the final event of a claude -p stream.
type ResultEvent struct {
	Subtype          string          `json:"subtype"`
	IsError          bool            `json:"is_error"`
	NumTurns         int             `json:"num_turns"`
	DurationMS       int64           `json:"duration_ms"`
	TotalCostUSD     float64         `json:"total_cost_usd"`
	Usage            Usage           `json:"usage"`
	StructuredOutput json.RawMessage `json:"structured_output"`
	Result           string          `json:"result"`
}

// StreamSummary is what one claude -p stream-json run did.
type StreamSummary struct {
	// Model and Tools come from the init event: the model the alias
	// resolved to and the exact tool list the agent was given.
	Model string
	Tools []string
	// MCPServers maps each MCP server name to its connection status.
	MCPServers map[string]string
	// ToolCalls counts retrieval tool calls by tool name.
	ToolCalls map[string]int
	// ToolOutputBytes is the size of everything retrieval tools returned.
	ToolOutputBytes int
	Result          *ResultEvent
}

// TotalToolCalls sums ToolCalls across tools.
func (summary StreamSummary) TotalToolCalls() int {
	total := 0

	for _, count := range summary.ToolCalls {
		total += count
	}

	return total
}

type streamEvent struct {
	Type       string   `json:"type"`
	Subtype    string   `json:"subtype"`
	Model      string   `json:"model"`
	Tools      []string `json:"tools"`
	MCPServers []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	} `json:"mcp_servers"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	Text      string          `json:"text"`
}

// maxStreamLine bounds one stream-json line. A Read of a large file comes
// back as a single tool_result line, so the default 64 KiB scanner buffer is
// far too small.
const maxStreamLine = 64 << 20

// ParseStream reads a claude -p --output-format stream-json transcript.
// Token totals come from the result event rather than from summing the
// assistant events, because Claude Code emits one event per content block
// and repeats the message's usage on each.
func ParseStream(reader io.Reader) (StreamSummary, error) {
	summary := StreamSummary{
		MCPServers: make(map[string]string),
		ToolCalls:  make(map[string]int),
	}

	toolNames := make(map[string]string)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 1<<20), maxStreamLine)

	for scanner.Scan() {
		line := scanner.Bytes()

		if len(line) == 0 {
			continue
		}

		var event streamEvent

		if decodeErr := json.Unmarshal(line, &event); decodeErr != nil {
			return summary, fmt.Errorf("decoding stream event: %w", decodeErr)
		}

		switch event.Type {
		case "system":
			if event.Subtype == "init" {
				summary.Model = event.Model
				summary.Tools = event.Tools

				for _, server := range event.MCPServers {
					summary.MCPServers[server.Name] = server.Status
				}
			}
		case "assistant":
			for _, block := range decodeBlocks(event.Message.Content) {
				if block.Type != "tool_use" || block.ID == "" {
					continue
				}

				if _, counted := toolNames[block.ID]; counted {
					continue
				}

				toolNames[block.ID] = block.Name

				if block.Name != structuredOutputTool {
					summary.ToolCalls[block.Name]++
				}
			}
		case "user":
			for _, block := range decodeBlocks(event.Message.Content) {
				if block.Type != "tool_result" || toolNames[block.ToolUseID] == structuredOutputTool {
					continue
				}

				summary.ToolOutputBytes += toolResultBytes(block.Content)
			}
		case "result":
			var result ResultEvent

			if decodeErr := json.Unmarshal(line, &result); decodeErr != nil {
				return summary, fmt.Errorf("decoding result event: %w", decodeErr)
			}

			summary.Result = &result
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return summary, fmt.Errorf("reading stream: %w", scanErr)
	}

	return summary, nil
}

// decodeBlocks decodes a message's content, which is either a string (no
// blocks of interest) or an array of content blocks.
func decodeBlocks(raw json.RawMessage) []contentBlock {
	var blocks []contentBlock

	if decodeErr := json.Unmarshal(raw, &blocks); decodeErr != nil {
		return nil
	}

	return blocks
}

// toolResultBytes measures a tool_result's content, which is either a plain
// string or an array of text blocks.
func toolResultBytes(raw json.RawMessage) int {
	var text string

	if json.Unmarshal(raw, &text) == nil {
		return len(text)
	}

	total := 0

	for _, block := range decodeBlocks(raw) {
		total += len(block.Text)
	}

	return total
}
