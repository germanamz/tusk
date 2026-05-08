package embed_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/node"
)

func TestBuildPayload_IncludesTypeTitleAndBody(test *testing.T) {
	parsedNode := &node.Node{
		Type:  "ticket",
		Title: "Fix login",
		Properties: map[string]any{
			"type":     "ticket",
			"title":    "Fix login",
			"priority": 3,
		},
		Body: []byte("Body text here.\n"),
	}

	payload := embed.BuildPayload(parsedNode)

	rendered := string(payload)

	if !strings.Contains(rendered, "[type] ticket") {
		test.Errorf("payload missing type marker: %q", rendered)
	}

	if !strings.Contains(rendered, "[title] Fix login") {
		test.Errorf("payload missing title marker: %q", rendered)
	}

	if !strings.Contains(rendered, "Body text here.") {
		test.Errorf("payload missing body: %q", rendered)
	}

	if !strings.Contains(rendered, "priority=3") {
		test.Errorf("payload missing extra property: %q", rendered)
	}
}

func TestBuildPayload_StableOrder(test *testing.T) {
	parsedNode := &node.Node{
		Type: "note",
		Properties: map[string]any{
			"type":  "note",
			"title": "X",
			"a":     "1",
			"b":     "2",
			"c":     "3",
		},
		Body: []byte("body"),
	}

	first := embed.BuildPayload(parsedNode)
	second := embed.BuildPayload(parsedNode)

	if string(first) != string(second) {
		test.Errorf("BuildPayload not stable:\nfirst:  %q\nsecond: %q", first, second)
	}
}
