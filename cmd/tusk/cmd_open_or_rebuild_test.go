package main_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestReindexCommandRebuildsOnSchemaMismatch(test *testing.T) {
	binary := filepath.Join(test.TempDir(), "tusk")
	build := exec.Command("go", "build", "-o", binary, "./cmd/tusk")
	build.Dir = repoRoot(test)
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if buildErr := build.Run(); buildErr != nil {
		test.Fatalf("build tusk: %v", buildErr)
	}

	root := test.TempDir()
	if writeErr := os.WriteFile(filepath.Join(root, "hello.md"), []byte("---\ntitle: hello\n---\nbody\n"), 0o644); writeErr != nil {
		test.Fatalf("write fixture: %v", writeErr)
	}
	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte("[workspace]\nname = \"test\"\n"), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	indexPath := filepath.Join(root, ".tusk", "index.db")
	if mkErr := os.MkdirAll(filepath.Dir(indexPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir index dir: %v", mkErr)
	}

	store, openErr := index.Open(indexPath)
	if openErr != nil {
		test.Fatalf("seed index open: %v", openErr)
	}
	if setErr := index.NewMetaRepo(store).Set(index.MetaSchemaVersionKey, "from-other-binary"); setErr != nil {
		test.Fatalf("seed mismatch: %v", setErr)
	}
	if closeErr := store.Close(); closeErr != nil {
		test.Fatalf("close seed: %v", closeErr)
	}

	cmd := exec.Command(binary, "reindex")
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		test.Fatalf("tusk reindex: %v\nstderr:\n%s", runErr, stderr.String())
	}

	reopened, reopenErr := index.Open(indexPath)
	if reopenErr != nil {
		test.Fatalf("reopen after rebuild: %v", reopenErr)
	}
	defer reopened.Close()

	got, getErr := index.NewMetaRepo(reopened).Get(index.MetaSchemaVersionKey)
	if getErr != nil {
		test.Fatalf("read schema_version: %v", getErr)
	}
	if got != index.SchemaVersion {
		test.Errorf("rebuilt schema_version = %q, want %q", got, index.SchemaVersion)
	}
}

func repoRoot(test *testing.T) string {
	test.Helper()
	wd, wdErr := os.Getwd()
	if wdErr != nil {
		test.Fatalf("getwd: %v", wdErr)
	}
	// cmd/tusk -> repo root is two levels up.
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}
