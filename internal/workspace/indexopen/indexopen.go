// Package indexopen wraps index.Open with rebuild-on-mismatch
// semantics. It lives outside both internal/index and internal/reindex
// to avoid an import cycle (reindex already depends on index; the
// rebuild flow needs to invoke reindex.Run when index.Open trips the
// schema-version sentinel).
package indexopen

import (
	"errors"
	"fmt"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/reindex"
)

// Config drives OpenOrRebuild.
type Config struct {
	// IndexPath is the on-disk SQLite file. Passed directly to
	// index.Open and deleted on a schema-version mismatch.
	IndexPath string
	// ReindexFactory builds a reindex.Config for the freshly created
	// index. It is invoked only after a rebuild, with the new
	// *index.Index as input so the caller can construct repos.
	ReindexFactory func(*index.Index) reindex.Config
	// Logger receives a one-line human-readable message when a
	// rebuild happens. Optional; nil disables logging.
	Logger func(string)
}

// OpenOrRebuild opens the index at cfg.IndexPath. If the open trips
// index.ErrSchemaIncompatible, the on-disk file is deleted, the
// index is re-opened (which writes the current SchemaVersion to a
// fresh database), and reindex.Run repopulates it from source files
// using the Config returned by cfg.ReindexFactory.
//
// On success the returned *index.Index is open and ready for use;
// the caller is responsible for closing it. On error nothing is
// open.
func OpenOrRebuild(cfg Config) (*index.Index, error) {
	if cfg.IndexPath == "" {
		return nil, errors.New("indexopen: IndexPath is required")
	}
	if cfg.ReindexFactory == nil {
		return nil, errors.New("indexopen: ReindexFactory is required")
	}

	store, openErr := index.Open(cfg.IndexPath)
	if openErr == nil {
		return store, nil
	}

	if !errors.Is(openErr, index.ErrSchemaIncompatible) {
		return nil, openErr
	}

	if cfg.Logger != nil {
		cfg.Logger("index schema changed in this version, rebuilding…")
	}

	if _, removeErr := index.RemoveArtifacts(cfg.IndexPath); removeErr != nil {
		return nil, fmt.Errorf("indexopen: delete stale index at %s: %w", cfg.IndexPath, removeErr)
	}

	fresh, freshErr := index.Open(cfg.IndexPath)
	if freshErr != nil {
		return nil, fmt.Errorf("indexopen: reopen after delete: %w", freshErr)
	}

	reindexCfg := cfg.ReindexFactory(fresh)
	if _, runErr := reindex.Run(reindexCfg); runErr != nil {
		fresh.Close()
		return nil, fmt.Errorf("indexopen: reindex during rebuild: %w", runErr)
	}

	return fresh, nil
}
