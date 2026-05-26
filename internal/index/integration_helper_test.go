package index_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/workspace/indexopen"
)

// openRebuilt seeds a stale schema_version at indexPath so OpenOrRebuild
// trips the version mismatch and exercises the full drop → reopen →
// reindex path end-to-end. The caller owns the returned store and must
// Close it.
func openRebuilt(test *testing.T, indexPath string, factory func(*index.Index) reindex.Config) *index.Index {
	test.Helper()

	if mkErr := os.MkdirAll(filepath.Dir(indexPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir index dir: %v", mkErr)
	}

	stale, openErr := index.Open(indexPath)
	if openErr != nil {
		test.Fatalf("seed open: %v", openErr)
	}
	if setErr := index.NewMetaRepo(stale).Set(index.MetaSchemaVersionKey, "stale-version"); setErr != nil {
		test.Fatalf("seed mismatch: %v", setErr)
	}
	if closeErr := stale.Close(); closeErr != nil {
		test.Fatalf("close seed: %v", closeErr)
	}

	store, openErr := indexopen.OpenOrRebuild(context.Background(), indexopen.Config{
		IndexPath:      indexPath,
		ReindexFactory: factory,
		Logger:         func(string) {},
	})
	if openErr != nil {
		test.Fatalf("OpenOrRebuild: %v", openErr)
	}

	return store
}
