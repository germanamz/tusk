package indexopen_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/workspace/indexopen"
)

func TestFullCycle_OpenMismatchRebuildReopen(test *testing.T) {
	test.Parallel()

	root := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(root, "hello.md"), []byte("---\ntitle: hello\n---\nbody\n"), 0o644); writeErr != nil {
		test.Fatalf("seed file: %v", writeErr)
	}

	indexPath := filepath.Join(root, ".tusk", "index.db")

	if mkErr := os.MkdirAll(filepath.Dir(indexPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	factory := func(idx *index.Index) reindex.Config {
		return reindex.Config{
			Root:       root,
			Repo:       index.NewNodeRepo(idx),
			Meta:       index.NewMetaRepo(idx),
			FileStates: index.NewFileStateRepo(idx),
		}
	}

	var logs []string

	cfg := indexopen.Config{
		IndexPath:      indexPath,
		ReindexFactory: factory,
		Logger:         func(m string) { logs = append(logs, m) },
	}

	first, openErr := indexopen.OpenOrRebuild(context.Background(), cfg)

	if openErr != nil {
		test.Fatalf("fresh open: %v", openErr)
	}

	if got, _ := index.NewMetaRepo(first).Get(index.MetaSchemaVersionKey); got != index.SchemaVersion {
		test.Errorf("fresh schema_version = %q, want %q", got, index.SchemaVersion)
	}

	if closeErr := first.Close(); closeErr != nil {
		test.Fatalf("close fresh: %v", closeErr)
	}

	if len(logs) != 0 {
		test.Errorf("unexpected rebuild log on fresh open: %v", logs)
	}

	second, openErr := index.Open(indexPath)

	if openErr != nil {
		test.Fatalf("seed reopen: %v", openErr)
	}

	if setErr := index.NewMetaRepo(second).Set(index.MetaSchemaVersionKey, "from-other-binary"); setErr != nil {
		test.Fatalf("seed mismatch: %v", setErr)
	}

	if closeErr := second.Close(); closeErr != nil {
		test.Fatalf("close seed: %v", closeErr)
	}

	third, openErr := indexopen.OpenOrRebuild(context.Background(), cfg)

	if openErr != nil {
		test.Fatalf("rebuild open: %v", openErr)
	}

	defer third.Close()

	if got, _ := index.NewMetaRepo(third).Get(index.MetaSchemaVersionKey); got != index.SchemaVersion {
		test.Errorf("after rebuild schema_version = %q, want %q", got, index.SchemaVersion)
	}

	if len(logs) == 0 {
		test.Error("expected at least one rebuild log entry")
	}

	logsBefore := len(logs)

	fourth, openErr := indexopen.OpenOrRebuild(context.Background(), cfg)

	if openErr != nil {
		test.Fatalf("steady-state reopen: %v", openErr)
	}

	defer fourth.Close()

	if len(logs) != logsBefore {
		test.Errorf("unexpected rebuild log on matching-version reopen: %v", logs[logsBefore:])
	}
}
