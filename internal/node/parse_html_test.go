package node_test

import (
	"errors"
	"testing"

	"github.com/germanamz/tusk/internal/node"
)

func TestParseHTMLFile_ExtractsTypeFromMeta(test *testing.T) {
	content := []byte(`<!DOCTYPE html>
<html>
<head>
<meta name="tusk:type" content="reference">
</head>
<body><p>Body text.</p></body>
</html>`)

	parsed, parseErr := node.ParseHTMLFile("refs/page.html", content)

	if parseErr != nil {
		test.Fatalf("ParseHTMLFile: %v", parseErr)
	}

	if parsed.Type != "reference" {
		test.Errorf("Type = %q, want reference", parsed.Type)
	}

	if parsed.Path != "refs/page.html" {
		test.Errorf("Path = %q, want refs/page.html", parsed.Path)
	}

	if parsed.Edges != nil {
		test.Errorf("Edges = %v, want nil", parsed.Edges)
	}
}

func TestParseHTMLFile_MissingTypeMetaReturnsErrMissingType(test *testing.T) {
	content := []byte(`<html><head><title>No type</title></head><body><p>Hi</p></body></html>`)

	_, parseErr := node.ParseHTMLFile("refs/no-type.html", content)

	if !errors.Is(parseErr, node.ErrMissingType) {
		test.Errorf("err = %v, want ErrMissingType", parseErr)
	}
}

func TestParseHTMLFile_EmptyTypeContentReturnsErrMissingType(test *testing.T) {
	content := []byte(`<html><head><meta name="tusk:type" content=""></head><body></body></html>`)

	_, parseErr := node.ParseHTMLFile("refs/empty-type.html", content)

	if !errors.Is(parseErr, node.ErrMissingType) {
		test.Errorf("err = %v, want ErrMissingType", parseErr)
	}
}

func TestParseHTMLFile_TitleFromMetaWins(test *testing.T) {
	content := []byte(`<html><head>
<meta name="tusk:type" content="reference">
<meta name="tusk:title" content="Meta Title">
<title>Element Title</title>
</head><body><h1>Heading Title</h1></body></html>`)

	parsed, parseErr := node.ParseHTMLFile("p.html", content)

	if parseErr != nil {
		test.Fatalf("ParseHTMLFile: %v", parseErr)
	}

	if parsed.Title != "Meta Title" {
		test.Errorf("Title = %q, want Meta Title", parsed.Title)
	}
}

func TestParseHTMLFile_TitleFallsBackToTitleElement(test *testing.T) {
	content := []byte(`<html><head>
<meta name="tusk:type" content="reference">
<title>Element Title</title>
</head><body><h1>Heading Title</h1></body></html>`)

	parsed, _ := node.ParseHTMLFile("p.html", content)

	if parsed.Title != "Element Title" {
		test.Errorf("Title = %q, want Element Title", parsed.Title)
	}
}

func TestParseHTMLFile_TitleFallsBackToFirstH1(test *testing.T) {
	content := []byte(`<html><head>
<meta name="tusk:type" content="reference">
</head><body><h1>First Heading</h1><h1>Second Heading</h1></body></html>`)

	parsed, _ := node.ParseHTMLFile("p.html", content)

	if parsed.Title != "First Heading" {
		test.Errorf("Title = %q, want First Heading", parsed.Title)
	}
}

func TestParseHTMLFile_TitleEmptyWhenNoSource(test *testing.T) {
	content := []byte(`<html><head>
<meta name="tusk:type" content="reference">
</head><body><p>No title anywhere.</p></body></html>`)

	parsed, _ := node.ParseHTMLFile("p.html", content)

	if parsed.Title != "" {
		test.Errorf("Title = %q, want empty", parsed.Title)
	}
}

func TestParseHTMLFile_MetaPropertiesAreYAMLTyped(test *testing.T) {
	content := []byte(`<html><head>
<meta name="tusk:type" content="reference">
<meta name="tusk:priority" content="3">
<meta name="tusk:active" content="true">
<meta name="tusk:owner" content="german">
</head><body></body></html>`)

	parsed, parseErr := node.ParseHTMLFile("p.html", content)

	if parseErr != nil {
		test.Fatalf("ParseHTMLFile: %v", parseErr)
	}

	priority, hasPriority := parsed.Properties["priority"]

	if !hasPriority {
		test.Fatalf("priority not in Properties")
	}

	if priorityInt, isInt := priority.(int); !isInt || priorityInt != 3 {
		test.Errorf("priority = %v (%T), want 3 (int)", priority, priority)
	}

	if active, isBool := parsed.Properties["active"].(bool); !isBool || active != true {
		test.Errorf("active = %v (%T), want true (bool)", parsed.Properties["active"], parsed.Properties["active"])
	}

	if owner, isStr := parsed.Properties["owner"].(string); !isStr || owner != "german" {
		test.Errorf("owner = %v (%T), want \"german\" (string)", parsed.Properties["owner"], parsed.Properties["owner"])
	}
}

func TestParseHTMLFile_MetaPropertyRepeatLastWins(test *testing.T) {
	content := []byte(`<html><head>
<meta name="tusk:type" content="reference">
<meta name="tusk:priority" content="1">
<meta name="tusk:priority" content="9">
</head><body></body></html>`)

	parsed, _ := node.ParseHTMLFile("p.html", content)

	if priorityInt, isInt := parsed.Properties["priority"].(int); !isInt || priorityInt != 9 {
		test.Errorf("priority = %v (%T), want 9 (int)", parsed.Properties["priority"], parsed.Properties["priority"])
	}
}

func TestParseHTMLFile_TypeAndTitleNotInProperties(test *testing.T) {
	content := []byte(`<html><head>
<meta name="tusk:type" content="reference">
<meta name="tusk:title" content="A Title">
</head><body></body></html>`)

	parsed, _ := node.ParseHTMLFile("p.html", content)

	if _, hasType := parsed.Properties["type"]; hasType {
		test.Errorf("type leaked into Properties")
	}

	if _, hasTitle := parsed.Properties["title"]; hasTitle {
		test.Errorf("title leaked into Properties")
	}
}

func TestParseHTMLFile_DataAttributesCollectedIntoSignals(test *testing.T) {
	content := []byte(`<html><head>
<meta name="tusk:type" content="reference">
</head><body>
<div data-topic="auth" data-stage="draft">x</div>
<span data-topic="oauth">y</span>
</body></html>`)

	parsed, parseErr := node.ParseHTMLFile("p.html", content)

	if parseErr != nil {
		test.Fatalf("ParseHTMLFile: %v", parseErr)
	}

	signals, hasSignals := parsed.Properties[node.HTMLSignalsKey].(map[string][]string)

	if !hasSignals {
		test.Fatalf("data signals not a map[string][]string: %T", parsed.Properties[node.HTMLSignalsKey])
	}

	topic := signals["topic"]

	if len(topic) != 2 || topic[0] != "auth" || topic[1] != "oauth" {
		test.Errorf("topic = %v, want [auth oauth] in document order", topic)
	}

	stage := signals["stage"]

	if len(stage) != 1 || stage[0] != "draft" {
		test.Errorf("stage = %v, want [draft]", stage)
	}
}

func TestParseHTMLFile_NoDataAttributesOmitsSignalsKey(test *testing.T) {
	content := []byte(`<html><head>
<meta name="tusk:type" content="reference">
</head><body><p>No data attributes.</p></body></html>`)

	parsed, _ := node.ParseHTMLFile("p.html", content)

	if _, present := parsed.Properties[node.HTMLSignalsKey]; present {
		test.Errorf("signals key present with no data-* attributes")
	}
}

func TestHTMLSignalsKeyValue(test *testing.T) {
	if node.HTMLSignalsKey != "data" {
		test.Errorf("HTMLSignalsKey = %q, want data", node.HTMLSignalsKey)
	}
}
