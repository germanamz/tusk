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
