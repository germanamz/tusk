package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/workspace"
)

func TestFind_FindsManifestInCurrentDir(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	if writeErr := os.WriteFile(manifestPath, []byte("[workspace]\nname=\"test\"\n"), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	found, findErr := workspace.Find(tmpDir)

	if findErr != nil {
		test.Fatalf("Find: %v", findErr)
	}

	if found.Root != tmpDir {
		test.Errorf("Root = %q, want %q", found.Root, tmpDir)
	}

	if found.ManifestPath != manifestPath {
		test.Errorf("ManifestPath = %q, want %q", found.ManifestPath, manifestPath)
	}
}

func TestFind_WalksUpToParent(test *testing.T) {
	tmpDir := test.TempDir()
	subDir := filepath.Join(tmpDir, "sub", "deeper")

	if mkErr := os.MkdirAll(subDir, 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	if writeErr := os.WriteFile(manifestPath, []byte("[workspace]\nname=\"test\"\n"), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	found, findErr := workspace.Find(subDir)

	if findErr != nil {
		test.Fatalf("Find: %v", findErr)
	}

	if found.Root != tmpDir {
		test.Errorf("Root = %q, want %q", found.Root, tmpDir)
	}
}

func TestFind_ReturnsErrNotFoundWhenNoManifest(test *testing.T) {
	tmpDir := test.TempDir()

	_, findErr := workspace.Find(tmpDir)

	if findErr == nil {
		test.Fatalf("expected error, got nil")
	}

	if !errorIsNotFound(findErr) {
		test.Errorf("error = %v, want ErrNotFound", findErr)
	}
}

func errorIsNotFound(err error) bool {
	return err != nil && err.Error() == workspace.ErrNotFound.Error()
}
