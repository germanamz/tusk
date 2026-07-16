package graphview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

// The change source moved to internal/webui in the shared-webui extraction;
// webui.TestChangeSourceSignal covers it (gen + epoch), so graphview no longer
// tests it here.

func TestRenderer_ReadsAndRenders(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "n.md"), []byte("---\ntype: note\n---\nHello body\n"), 0o644)

	nr := NewRenderer(root, &fakeNodes{byID: map[string]index.NodeRow{
		"n": fileRow("n", "note", "N", ""),
	}})

	text, err := nr.Render("n")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if text == "" || !strings.Contains(text, "Hello body") {
		t.Fatalf("render = %q", text)
	}
}
