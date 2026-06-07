package indexepoch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRead_AbsentReturnsZero(test *testing.T) {
	root := test.TempDir()

	got, err := Read(root)
	if err != nil {
		test.Fatalf("Read: %v", err)
	}
	if got != 0 {
		test.Fatalf("expected 0 for absent epoch, got %d", got)
	}
}

func TestRead_ParsesExistingValue(test *testing.T) {
	root := test.TempDir()
	if mkErr := os.MkdirAll(filepath.Join(root, ".tusk"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}
	if writeErr := os.WriteFile(filepath.Join(root, ".tusk", "epoch"), []byte("42\n"), 0o644); writeErr != nil {
		test.Fatalf("seed: %v", writeErr)
	}

	got, err := Read(root)
	if err != nil {
		test.Fatalf("Read: %v", err)
	}
	if got != 42 {
		test.Fatalf("expected 42, got %d", got)
	}
}

func TestBump_Monotonic(test *testing.T) {
	root := test.TempDir()

	first, err := Bump(root)
	if err != nil {
		test.Fatalf("Bump 1: %v", err)
	}
	if first != 1 {
		test.Fatalf("expected first bump = 1, got %d", first)
	}

	second, err := Bump(root)
	if err != nil {
		test.Fatalf("Bump 2: %v", err)
	}
	if second != 2 {
		test.Fatalf("expected second bump = 2, got %d", second)
	}

	readBack, err := Read(root)
	if err != nil {
		test.Fatalf("Read: %v", err)
	}
	if readBack != 2 {
		test.Fatalf("expected persisted 2, got %d", readBack)
	}
}

func TestBump_LeavesNoTempFile(test *testing.T) {
	root := test.TempDir()
	if _, err := Bump(root); err != nil {
		test.Fatalf("Bump: %v", err)
	}

	entries, readErr := os.ReadDir(filepath.Join(root, ".tusk"))
	if readErr != nil {
		test.Fatalf("readdir: %v", readErr)
	}
	for _, entry := range entries {
		if entry.Name() != EpochFilename {
			test.Errorf("unexpected leftover file in .tusk: %q", entry.Name())
		}
	}
}
