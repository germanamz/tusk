package reset

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/indexepoch"
)

func TestPerform_DeletesReopensAndBumps(test *testing.T) {
	root := test.TempDir()
	indexPath := filepath.Join(root, ".tusk", "index.db")

	// Seed a real index plus a stale WAL sidecar.
	seed, openErr := index.Open(indexPath)
	if openErr != nil {
		test.Fatalf("seed open: %v", openErr)
	}
	seed.Close()
	if writeErr := os.WriteFile(indexPath+"-wal", []byte("stale"), 0o644); writeErr != nil {
		test.Fatalf("seed wal: %v", writeErr)
	}

	quiesced := false

	result, err := Perform(context.Background(), Config{
		Root:      root,
		IndexPath: indexPath,
		LockTTL:   time.Second,
		Quiesce:   func() error { quiesced = true; return nil },
		Reopen:    func() (*index.Index, error) { return index.Open(indexPath) },
	})
	if err != nil {
		test.Fatalf("Perform: %v", err)
	}
	defer result.Store.Close()

	if !quiesced {
		test.Error("Quiesce was not invoked")
	}
	if result.Epoch != 1 {
		test.Errorf("expected epoch 1, got %d", result.Epoch)
	}
	if got, _ := indexepoch.Read(root); got != 1 {
		test.Errorf("expected persisted epoch 1, got %d", got)
	}
	if len(result.DeletedArtifacts) == 0 {
		test.Error("expected at least the main db reported deleted")
	}
	// Fresh handle must be usable.
	if _, listErr := result.Store.ListTables(); listErr != nil {
		test.Errorf("fresh store unusable: %v", listErr)
	}
}

func TestPerform_ReapsStaging(test *testing.T) {
	root := test.TempDir()
	indexPath := filepath.Join(root, ".tusk", "index.db")
	stagingDir := filepath.Join(root, ".tusk", "staging")

	if mkErr := os.MkdirAll(stagingDir, 0o755); mkErr != nil {
		test.Fatalf("mkdir staging: %v", mkErr)
	}
	if writeErr := os.WriteFile(filepath.Join(stagingDir, "foo.tmp"), []byte("x"), 0o644); writeErr != nil {
		test.Fatalf("seed staging: %v", writeErr)
	}

	result, err := Perform(context.Background(), Config{
		Root:      root,
		IndexPath: indexPath,
		LockTTL:   time.Second,
		Reopen:    func() (*index.Index, error) { return index.Open(indexPath) },
	})
	if err != nil {
		test.Fatalf("Perform: %v", err)
	}
	defer result.Store.Close()

	if _, statErr := os.Stat(stagingDir); !os.IsNotExist(statErr) {
		test.Errorf("staging dir survived reset (stat err: %v)", statErr)
	}
}
