package webui

import (
	"testing"

	"github.com/germanamz/tusk/internal/epoch"
)

type fakeMeta struct{ gen string }

func (fakeMetaReader fakeMeta) Get(key string) (string, error) {
	if key == "reindex_gen" {
		return fakeMetaReader.gen, nil
	}
	return "", nil
}

func TestChangeSourceSignal(t *testing.T) {
	root := t.TempDir()
	// The epoch package exposes only Read + Bump(root) (increment-by-one); there is
	// NO Write(value). Seed by bumping five times (0 -> 5), or write the sentinel
	// file directly. Bump is the API-faithful path:
	for i := 0; i < 5; i++ {
		if _, err := epoch.Index.Bump(root); err != nil {
			t.Fatalf("seed epoch: %v", err)
		}
	}
	changeSource := NewChangeSource(root, fakeMeta{gen: "42"})
	sig, err := changeSource.Signal()
	if err != nil {
		t.Fatalf("Signal: %v", err)
	}
	if sig.Generation != 42 || sig.Epoch != 5 {
		t.Fatalf("got %+v want {42 5}", sig)
	}
}
