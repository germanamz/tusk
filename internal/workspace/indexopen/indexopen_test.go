package indexopen_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/workspace/indexopen"
)

// fixtureWorkspace seeds a minimal workspace directory with one note
// file so reindex.Run has something to ingest. It returns the
// workspace root and the index path.
func fixtureWorkspace(test *testing.T) (root, indexPath string) {
	test.Helper()

	root = test.TempDir()

	noteContents := "---\ntitle: hello\n---\nbody\n"
	if writeErr := os.WriteFile(filepath.Join(root, "hello.md"), []byte(noteContents), 0o644); writeErr != nil {
		test.Fatalf("write fixture file: %v", writeErr)
	}

	indexPath = filepath.Join(root, ".tusk", "index.db")
	if mkErr := os.MkdirAll(filepath.Dir(indexPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir index dir: %v", mkErr)
	}

	return root, indexPath
}

func TestOpenOrRebuildOpensFreshIndex(test *testing.T) {
	test.Parallel()

	root, indexPath := fixtureWorkspace(test)

	cfg := indexopen.Config{
		IndexPath: indexPath,
		ReindexFactory: func(store *index.Index) reindex.Config {
			return reindex.Config{
				Root:       root,
				Repo:       index.NewNodeRepo(store),
				Meta:       index.NewMetaRepo(store),
				FileStates: index.NewFileStateRepo(store),
				EmbedQueue: index.NewEmbedQueueRepo(store),
			}
		},
		Logger: func(string) {},
	}

	store, openErr := indexopen.OpenOrRebuild(context.Background(), cfg)
	if openErr != nil {
		test.Fatalf("OpenOrRebuild: %v", openErr)
	}
	defer store.Close()

	if _, statErr := os.Stat(indexPath); statErr != nil {
		test.Fatalf("expected index file at %s, got %v", indexPath, statErr)
	}
}

func TestOpenOrRebuildRebuildsOnMismatch(test *testing.T) {
	test.Parallel()

	root, indexPath := fixtureWorkspace(test)

	// First open writes the current SchemaVersion.
	first, openErr := index.Open(indexPath)
	if openErr != nil {
		test.Fatalf("seed open: %v", openErr)
	}

	meta := index.NewMetaRepo(first)
	if setErr := meta.Set(index.MetaSchemaVersionKey, "from-some-other-binary"); setErr != nil {
		test.Fatalf("seed mismatch: %v", setErr)
	}
	if closeErr := first.Close(); closeErr != nil {
		test.Fatalf("close seed: %v", closeErr)
	}

	preInfo, statErr := os.Stat(indexPath)
	if statErr != nil {
		test.Fatalf("stat seeded index: %v", statErr)
	}

	var rebuildMessages []string

	cfg := indexopen.Config{
		IndexPath: indexPath,
		ReindexFactory: func(store *index.Index) reindex.Config {
			return reindex.Config{
				Root:       root,
				Repo:       index.NewNodeRepo(store),
				Meta:       index.NewMetaRepo(store),
				FileStates: index.NewFileStateRepo(store),
				EmbedQueue: index.NewEmbedQueueRepo(store),
			}
		},
		Logger: func(msg string) {
			rebuildMessages = append(rebuildMessages, msg)
		},
	}

	store, openErr := indexopen.OpenOrRebuild(context.Background(), cfg)
	if openErr != nil {
		test.Fatalf("OpenOrRebuild: %v", openErr)
	}
	defer store.Close()

	postInfo, statErr := os.Stat(indexPath)
	if statErr != nil {
		test.Fatalf("stat rebuilt index: %v", statErr)
	}

	if postInfo.ModTime().Equal(preInfo.ModTime()) {
		test.Error("index file was not recreated")
	}

	if len(rebuildMessages) == 0 {
		test.Error("Logger was never called with a rebuild message")
	}

	// Sanity: the rebuilt index has the current schema version.
	got, getErr := index.NewMetaRepo(store).Get(index.MetaSchemaVersionKey)
	if getErr != nil {
		test.Fatalf("read schema_version after rebuild: %v", getErr)
	}
	if got != index.SchemaVersion {
		test.Errorf("rebuilt schema_version = %q, want %q", got, index.SchemaVersion)
	}

	// Confirm the helper did not silently swallow other errors.
	if errors.Is(openErr, index.ErrSchemaIncompatible) {
		test.Error("OpenOrRebuild returned ErrSchemaIncompatible instead of rebuilding")
	}
}
