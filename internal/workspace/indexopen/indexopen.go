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
// index.ErrSchemaIncompatible, the index is rebuilt: a fresh database is
// built and repopulated at a sibling temp path (via reindex.Run with the
// Config returned by cfg.ReindexFactory), then atomically rename-swapped
// over the live file. The live index is never deleted up front, so a slow
// or interrupted rebuild leaves the previous index intact and re-openable
// rather than an empty, unqueryable file (#705 Defect A).
//
// On success the returned *index.Index is open and ready for use; the
// caller is responsible for closing it. On error nothing is open and the
// prior on-disk index is untouched.
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

	return rebuildAtomically(cfg)
}

// rebuildAtomically builds a fresh index at a sibling temp path, repopulates it,
// and rename-swaps it over cfg.IndexPath on success. On any failure the temp is
// removed and the live index at cfg.IndexPath is left untouched.
func rebuildAtomically(cfg Config) (*index.Index, error) {
	tmpPath := cfg.IndexPath + ".rebuild"

	// Clear any temp orphaned by a previously crashed rebuild so we start clean.
	if _, removeErr := index.RemoveArtifacts(tmpPath); removeErr != nil {
		return nil, fmt.Errorf("indexopen: clear stale rebuild temp at %s: %w", tmpPath, removeErr)
	}

	fresh, freshErr := index.Open(tmpPath)
	if freshErr != nil {
		return nil, fmt.Errorf("indexopen: open rebuild temp: %w", freshErr)
	}

	// On any failure past this point, drop the half-built temp (handle + files)
	// and leave the live index untouched.
	abandon := func(wrapErr error) (*index.Index, error) {
		fresh.Close()
		_, _ = index.RemoveArtifacts(tmpPath)

		return nil, wrapErr
	}

	reindexCfg := cfg.ReindexFactory(fresh)
	if _, runErr := reindex.Run(reindexCfg); runErr != nil {
		return abandon(fmt.Errorf("indexopen: reindex during rebuild: %w", runErr))
	}

	// Fold the WAL into the main temp file so the rename moves a self-contained
	// database, then close before swapping (the rename replaces the live file).
	if cpErr := fresh.Checkpoint(); cpErr != nil {
		return abandon(fmt.Errorf("indexopen: checkpoint rebuild temp: %w", cpErr))
	}

	if closeErr := fresh.Close(); closeErr != nil {
		_, _ = index.RemoveArtifacts(tmpPath)

		return nil, fmt.Errorf("indexopen: close rebuild temp: %w", closeErr)
	}

	if swapErr := index.SwapInPlace(tmpPath, cfg.IndexPath); swapErr != nil {
		_, _ = index.RemoveArtifacts(tmpPath)

		return nil, fmt.Errorf("indexopen: swap rebuilt index into place: %w", swapErr)
	}

	store, reopenErr := index.Open(cfg.IndexPath)
	if reopenErr != nil {
		return nil, fmt.Errorf("indexopen: reopen after swap: %w", reopenErr)
	}

	return store, nil
}
