package graphview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

type stubMeta struct{ gen string }

func (stub stubMeta) Get(key string) (string, error) {
	if key == "reindex_gen" {
		return stub.gen, nil
	}

	return "", nil
}

func TestChangeSource_CombinesGenAndEpoch(t *testing.T) {
	root := t.TempDir()

	cs := NewChangeSource(root, stubMeta{gen: "5"})

	sig, err := cs.Signal()
	if err != nil {
		t.Fatalf("Signal: %v", err)
	}

	if sig.Generation != 5 {
		t.Fatalf("generation = %d, want 5", sig.Generation)
	}

	if sig.Epoch != 0 { // no .tusk/epoch file => 0
		t.Fatalf("epoch = %d, want 0", sig.Epoch)
	}
}

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
