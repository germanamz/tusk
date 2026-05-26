package mcp

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

//go:embed help/*.md
var helpFS embed.FS

// helpTopics is the canonical list surfaced to agents in topic indexes
// and in the tool description. Keep the order matched to the recommended
// reading sequence (overview → workflow → schema deep-dives → grammar).
var helpTopics = []string{
	"overview",
	"workflow",
	"node-types",
	"edge-types",
	"manifest",
	"filter",
	"query",
	"packs",
}

func helpContent(topic string) (string, bool) {
	data, readErr := helpFS.ReadFile("help/" + topic + ".md")
	if readErr != nil {
		return "", false
	}
	return string(data), true
}

func helpTopicIndex() string {
	lines := []string{"Available tusk_help topics:", ""}

	sorted := append([]string(nil), helpTopics...)
	sort.Strings(sorted)

	for _, topic := range sorted {
		lines = append(lines, "  - "+topic)
	}

	lines = append(lines, "", `Call tusk_help(topic: "<name>") to read one.`)

	return strings.Join(lines, "\n")
}

func registerHelpTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_help",
		mcpgo.WithDescription("Returns agent-facing documentation for Tusk. Call with no arguments for an overview + topic index, or topic=\"<name>\" for a deep dive. Use this when unsure how Tusk's local-workspace model works, how to declare node/edge types in tusk.toml, what filter expressions are accepted, or how semantic + graph-expansion queries blend scores."),
		mcpgo.WithString("topic", mcpgo.Description("Topic name. Omit for overview + topic index. Known topics: overview, workflow, node-types, edge-types, manifest, filter, query, packs.")),
	)

	srv.register(tool, func(_ context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		topic := strings.TrimSpace(argStringOptional(request, "topic"))

		if topic == "" {
			overview, _ := helpContent("overview")
			return mcpgo.NewToolResultText(overview + "\n\n" + helpTopicIndex()), nil
		}

		content, found := helpContent(topic)
		if !found {
			return mcpgo.NewToolResultText(
				fmt.Sprintf("Unknown topic %q.\n\n%s", topic, helpTopicIndex()),
			), nil
		}

		return mcpgo.NewToolResultText(content), nil
	})
}
