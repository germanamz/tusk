// Package reset performs the destructive "drop the index and rebuild" core,
// independent of the CLI or MCP surface. Perform runs under the workspace lock
// and guarantees the ordering: acquire lock → quiesce caller's handle → delete
// the SQLite artifacts → reap staging → reopen a fresh handle (caller hook) →
// bump the epoch → release. The reindex that repopulates the index is the
// caller's responsibility (the Async choice is surface-specific), keeping the
// lock hold short.
package reset

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/germanamz/tusk/internal/epoch"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/lock"
)

// Config drives Perform.
type Config struct {
	Root      string                       // workspace root (absolute)
	IndexPath string                       // .tusk/index.db
	LockTTL   time.Duration                // bound on lock acquisition; 0 means inherit ctx deadline only
	Quiesce   func() error                 // stop/close anything holding the old handle; nil = no-op
	Reopen    func() (*index.Index, error) // reopen fresh and rebuild caller repos
}

// Result reports the outcome of a successful reset.
type Result struct {
	Epoch            int64        // the bumped epoch value
	DeletedArtifacts []string     // artifact paths actually removed
	Store            *index.Index // the freshly reopened handle; caller owns its lifecycle
}

// AcquireLock acquires the workspace flock for a reset, bounded by ttl (0 means
// inherit ctx's deadline only). The caller owns the returned handle and must
// Release it. Exposed so a live MCP daemon can hold the flock WITHOUT also
// holding its in-process runtime write-lock during the (possibly contended)
// flock-await — the tool takes the brief write-lock only around the swap.
func AcquireLock(ctx context.Context, root string, ttl time.Duration) (*lock.WorkspaceLock, error) {
	lockHandle, lockErr := lock.NewWorkspaceLock(root)

	if lockErr != nil {
		return nil, fmt.Errorf("reset: lock: %w", lockErr)
	}

	acquireCtx := ctx

	if ttl > 0 {
		var cancel context.CancelFunc

		acquireCtx, cancel = context.WithTimeout(ctx, ttl)
		defer cancel()
	}

	if acquireErr := lockHandle.Acquire(acquireCtx); acquireErr != nil {
		return nil, fmt.Errorf("reset: acquire lock: %w", acquireErr)
	}

	return lockHandle, nil
}

// PerformLocked runs the destructive reset core assuming the workspace flock is
// ALREADY held by the caller (via AcquireLock). It does NOT acquire or release
// the flock. Order: quiesce caller's handle → delete artifacts → reap staging →
// reopen fresh → bump epoch. On any error before the epoch bump, the epoch is
// left untouched so siblings are not signaled toward a half-built index.
func PerformLocked(cfg Config) (*Result, error) {
	if cfg.Root == "" || cfg.IndexPath == "" {
		return nil, errors.New("reset: Root and IndexPath are required")
	}

	if cfg.Reopen == nil {
		return nil, errors.New("reset: Reopen is required")
	}

	if cfg.Quiesce != nil {
		if quiesceErr := cfg.Quiesce(); quiesceErr != nil {
			return nil, fmt.Errorf("reset: quiesce: %w", quiesceErr)
		}
	}

	deleted, removeErr := index.RemoveArtifacts(cfg.IndexPath)

	if removeErr != nil {
		return nil, fmt.Errorf("reset: remove artifacts: %w", removeErr)
	}

	stagingDir := filepath.Join(cfg.Root, ".tusk", "staging")

	if reapErr := os.RemoveAll(stagingDir); reapErr != nil {
		return nil, fmt.Errorf("reset: reap staging: %w", reapErr)
	}

	store, reopenErr := cfg.Reopen()

	if reopenErr != nil {
		return nil, fmt.Errorf("reset: reopen: %w", reopenErr)
	}

	bumped, bumpErr := epoch.Index.Bump(cfg.Root)

	if bumpErr != nil {
		_ = store.Close()

		return nil, fmt.Errorf("reset: bump epoch: %w", bumpErr)
	}

	return &Result{Epoch: bumped, DeletedArtifacts: deleted, Store: store}, nil
}

// Perform is the one-shot convenience used by the CLI (which has no live
// in-process readers): acquire the flock, run the core, release. A live MCP
// daemon does NOT use Perform — it calls AcquireLock (readers still served) then
// takes its brief runtime write-lock around PerformLocked + the pointer swap.
func Perform(ctx context.Context, cfg Config) (*Result, error) {
	lockHandle, acquireErr := AcquireLock(ctx, cfg.Root, cfg.LockTTL)

	if acquireErr != nil {
		return nil, acquireErr
	}

	defer func() { _ = lockHandle.Release() }()

	return PerformLocked(cfg)
}
