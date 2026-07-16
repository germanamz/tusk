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

func TestChangeSourceSignal(test *testing.T) {
	root := test.TempDir()
	// The epoch package exposes only Read + Bump(root) (increment-by-one); there is
	// NO Write(value). Seed by bumping five times (0 -> 5), or write the sentinel
	// file directly. Bump is the API-faithful path:
	for i := 0; i < 5; i++ {
		if _, err := epoch.Index.Bump(root); err != nil {
			test.Fatalf("seed epoch: %v", err)
		}
	}
	source := NewChangeSource(root, fakeMeta{gen: "42"})
	sig, err := source.Signal()
	if err != nil {
		test.Fatalf("Signal: %v", err)
	}
	if sig.Generation != 42 || sig.Epoch != 5 {
		test.Fatalf("got %+v want {42 5}", sig)
	}
}

// TestChangeSourceSignalNonNumericGen verifies that a non-numeric reindex_gen
// is silently ignored (leaves Generation at 0) with no error returned. This
// documents intentional behavior inherited from graphview.
func TestChangeSourceSignalNonNumericGen(test *testing.T) {
	root := test.TempDir()
	for i := 0; i < 5; i++ {
		if _, err := epoch.Index.Bump(root); err != nil {
			test.Fatalf("seed epoch: %v", err)
		}
	}
	source := NewChangeSource(root, fakeMeta{gen: "abc"})
	sig, err := source.Signal()

	if err != nil {
		test.Fatalf("Signal with non-numeric gen should not error: %v", err)
	}

	if sig.Generation != 0 {
		test.Fatalf("Generation with non-numeric gen: got %d, want 0", sig.Generation)
	}

	if sig.Epoch != 5 {
		test.Fatalf("Epoch: got %d, want 5", sig.Epoch)
	}
}
