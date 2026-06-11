package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestNodeRenderCmd_Markdown_StripsMarkup(test *testing.T) {
	initWorkspace(test)

	create := newRootCmd()
	create.SetArgs([]string{"node", "create", "--type", "note", "--title", "Hi", "--path", "notes/m.md"})

	if execErr := create.Execute(); execErr != nil {
		test.Fatalf("create: %v", execErr)
	}

	output := &bytes.Buffer{}

	renderCmd := newRootCmd()
	renderCmd.SetOut(output)
	renderCmd.SetErr(output)
	renderCmd.SetArgs([]string{"node", "render", "notes/m"})

	if execErr := renderCmd.Execute(); execErr != nil {
		test.Fatalf("render: %v\noutput: %s", execErr, output.String())
	}

	if bytes.Contains(output.Bytes(), []byte("type: note")) {
		test.Errorf("render leaked frontmatter: %s", output.String())
	}
}

func TestNodeRenderCmd_HTML_StripsTags(test *testing.T) {
	root := initWorkspace(test)

	html := "<html><head><meta name=\"tusk:type\" content=\"note\"></head>" +
		"<body><h1>Greeting</h1><p>Hello <b>world</b>.</p></body></html>"

	if writeErr := os.WriteFile(filepath.Join(root, "page.html"), []byte(html), 0o644); writeErr != nil {
		test.Fatalf("write html: %v", writeErr)
	}

	reindex := newRootCmd()
	reindex.SetArgs([]string{"reindex"})

	if execErr := reindex.Execute(); execErr != nil {
		test.Fatalf("reindex: %v", execErr)
	}

	output := &bytes.Buffer{}

	renderCmd := newRootCmd()
	renderCmd.SetOut(output)
	renderCmd.SetErr(output)
	renderCmd.SetArgs([]string{"node", "render", "page.html"})

	if execErr := renderCmd.Execute(); execErr != nil {
		test.Fatalf("render: %v\noutput: %s", execErr, output.String())
	}

	got := output.String()

	if bytes.Contains(output.Bytes(), []byte("<")) {
		test.Errorf("render left tags in output: %s", got)
	}

	for _, word := range []string{"Greeting", "Hello", "world"} {
		if !bytes.Contains(output.Bytes(), []byte(word)) {
			test.Errorf("render dropped %q: %s", word, got)
		}
	}
}
