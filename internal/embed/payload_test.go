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

func TestBuildHeader_IncludesTypeTitleAndSortedProperties(test *testing.T) {
	parsedNode := &node.Node{
		Type:  "ticket",
		Title: "Fix login",
		Properties: map[string]any{
			"type":     "ticket",
			"title":    "Fix login",
			"priority": 3,
			"area":     "auth",
		},
		Body: []byte("ignored body"),
	}

	header := string(embed.BuildHeader(parsedNode))

	if !strings.HasPrefix(header, "[type] ticket\n") {
		test.Errorf("header should start with `[type] ticket`: %q", header)
	}

	if !strings.Contains(header, "[title] Fix login\n") {
		test.Errorf("header missing title: %q", header)
	}

	if !strings.HasSuffix(header, "---\n") {
		test.Errorf("header should end with `---\\n` separator: %q", header)
	}

	areaIdx := strings.Index(header, "area=auth")
	priorityIdx := strings.Index(header, "priority=3")

	if areaIdx < 0 || priorityIdx < 0 || areaIdx > priorityIdx {
		test.Errorf("properties should be sorted alphabetically; got %q", header)
	}

	if strings.Contains(header, "ignored body") {
		test.Errorf("header must not include body: %q", header)
	}
}

func TestBuildBody_ReturnsParsedBodyVerbatim(test *testing.T) {
	parsedNode := &node.Node{
		Type:  "note",
		Title: "T",
		Body:  []byte("paragraph one\n\nparagraph two\n"),
	}

	body := embed.BuildBody(parsedNode)

	if string(body) != "paragraph one\n\nparagraph two\n" {
		test.Errorf("body = %q", body)
	}
}

func TestBuildPayload_EqualsHeaderPlusBody(test *testing.T) {
	parsedNode := &node.Node{
		Type:  "note",
		Title: "T",
		Properties: map[string]any{
			"type":  "note",
			"title": "T",
			"tag":   "x",
		},
		Body: []byte("body content"),
	}

	combined := append(embed.BuildHeader(parsedNode), embed.BuildBody(parsedNode)...)
	whole := embed.BuildPayload(parsedNode)

	if string(combined) != string(whole) {
		test.Errorf("BuildHeader+BuildBody != BuildPayload:\nheader+body: %q\npayload:     %q", combined, whole)
	}
}
