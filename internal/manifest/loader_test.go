package manifest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

func TestLoad_ParsesMinimalManifest(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "my-brain"
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if loaded.Workspace.Name != "my-brain" {
		test.Errorf("Name = %q, want %q", loaded.Workspace.Name, "my-brain")
	}
}

func TestLoad_ParsesIgnorePatterns(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "my-brain"
ignore = ["build/", "*.tmp"]
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if len(loaded.Workspace.Ignore) != 2 {
		test.Fatalf("Ignore len = %d, want 2", len(loaded.Workspace.Ignore))
	}

	if loaded.Workspace.Ignore[0] != "build/" {
		test.Errorf("Ignore[0] = %q", loaded.Workspace.Ignore[0])
	}
}

func TestLoad_ReturnsErrorOnMalformedTOML(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	if writeErr := os.WriteFile(manifestPath, []byte("not = valid = toml"), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	_, loadErr := manifest.Load(manifestPath)

	if loadErr == nil {
		test.Fatalf("expected error, got nil")
	}
}
