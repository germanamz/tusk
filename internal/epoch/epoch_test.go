package epoch

import (
	"os"
	"path/filepath"
	"testing"
)

// handles exercises both sentinels through the shared machinery so a
// filename-specific regression in either is caught.
var handles = []struct {
	name string
	ep   Epoch
}{
	{name: "index", ep: Index},
	{name: "manifest", ep: Manifest},
}

func TestRead_AbsentReturnsZero(test *testing.T) {
	for _, handle := range handles {
		test.Run(handle.name, func(test *testing.T) {
			root := test.TempDir()

			got, err := handle.ep.Read(root)
			if err != nil {
				test.Fatalf("Read: %v", err)
			}
			if got != 0 {
				test.Fatalf("expected 0 for absent epoch, got %d", got)
			}
		})
	}
}

func TestRead_ParsesExistingValue(test *testing.T) {
	for _, handle := range handles {
		test.Run(handle.name, func(test *testing.T) {
			root := test.TempDir()
			if mkErr := os.MkdirAll(filepath.Join(root, ".tusk"), 0o755); mkErr != nil {
				test.Fatalf("mkdir: %v", mkErr)
			}
			if writeErr := os.WriteFile(filepath.Join(root, ".tusk", handle.ep.Filename()), []byte("42\n"), 0o644); writeErr != nil {
				test.Fatalf("seed: %v", writeErr)
			}

			got, err := handle.ep.Read(root)
			if err != nil {
				test.Fatalf("Read: %v", err)
			}
			if got != 42 {
				test.Fatalf("expected 42, got %d", got)
			}
		})
	}
}

func TestBump_Monotonic(test *testing.T) {
	for _, handle := range handles {
		test.Run(handle.name, func(test *testing.T) {
			root := test.TempDir()

			first, err := handle.ep.Bump(root)
			if err != nil {
				test.Fatalf("Bump 1: %v", err)
			}
			if first != 1 {
				test.Fatalf("expected first bump = 1, got %d", first)
			}

			second, err := handle.ep.Bump(root)
			if err != nil {
				test.Fatalf("Bump 2: %v", err)
			}
			if second != 2 {
				test.Fatalf("expected second bump = 2, got %d", second)
			}

			readBack, err := handle.ep.Read(root)
			if err != nil {
				test.Fatalf("Read: %v", err)
			}
			if readBack != 2 {
				test.Fatalf("expected persisted 2, got %d", readBack)
			}
		})
	}
}

func TestBump_LeavesNoTempFile(test *testing.T) {
	for _, handle := range handles {
		test.Run(handle.name, func(test *testing.T) {
			root := test.TempDir()
			if _, err := handle.ep.Bump(root); err != nil {
				test.Fatalf("Bump: %v", err)
			}

			entries, readErr := os.ReadDir(filepath.Join(root, ".tusk"))
			if readErr != nil {
				test.Fatalf("readdir: %v", readErr)
			}
			for _, entry := range entries {
				if entry.Name() != handle.ep.Filename() {
					test.Errorf("unexpected leftover file in .tusk: %q", entry.Name())
				}
			}
		})
	}
}

// TestSentinelFilenamesAreStable pins the on-disk sentinel names: they are a
// wire contract with sibling daemons.
func TestSentinelFilenamesAreStable(test *testing.T) {
	if IndexEpochFile != "epoch" {
		test.Errorf("IndexEpochFile = %q, want %q", IndexEpochFile, "epoch")
	}
	if ManifestEpochFile != "manifest-epoch" {
		test.Errorf("ManifestEpochFile = %q, want %q", ManifestEpochFile, "manifest-epoch")
	}
	if Index.Filename() != IndexEpochFile {
		test.Errorf("Index.Filename() = %q, want %q", Index.Filename(), IndexEpochFile)
	}
	if Manifest.Filename() != ManifestEpochFile {
		test.Errorf("Manifest.Filename() = %q, want %q", Manifest.Filename(), ManifestEpochFile)
	}
}
