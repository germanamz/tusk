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
