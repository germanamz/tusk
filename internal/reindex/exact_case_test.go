package reindex

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExistsWithExactCase verifies the reaper's case-exact existence probe
// (#686): a path is present only when every segment matches the on-disk case
// byte for byte. This is deterministic on both case-sensitive and
// case-insensitive filesystems — a case-folded alias never matches the listing.
func TestExistsWithExactCase(test *testing.T) {
	root := test.TempDir()

	if mkErr := os.MkdirAll(filepath.Join(root, "notes"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "notes", "Foo.md"), []byte("x"), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	if !existsWithExactCase(root, "notes/Foo.md") {
		test.Errorf("existsWithExactCase(notes/Foo.md) = false, want true (exact case on disk)")
	}

	if existsWithExactCase(root, "notes/foo.md") {
		test.Errorf("existsWithExactCase(notes/foo.md) = true, want false (only a case alias exists)")
	}

	if existsWithExactCase(root, "Notes/Foo.md") {
		test.Errorf("existsWithExactCase(Notes/Foo.md) = true, want false (directory case differs)")
	}

	if existsWithExactCase(root, "notes/missing.md") {
		test.Errorf("existsWithExactCase(notes/missing.md) = true, want false (absent)")
	}
}
