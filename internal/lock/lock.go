// Package lock provides an advisory cross-process workspace write lock backed
// by a flock at .tusk/lock. The lock is reserved for schema migrations and
// other workspace-wide reorganizations; runtime mutations coordinate via the
// per-file lease in internal/index (file_state) and the per-job lease on
// embed_queue.
package lock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// LockFilename is the lock file name inside .tusk/.
const LockFilename = "lock"

// ErrBusy is returned when a lock cannot be acquired before the context is
// cancelled or expires.
var ErrBusy = errors.New("lock: workspace is busy (another tusk process holds the write lock)")

// WorkspaceLock is an exclusive, advisory write lock on a workspace.
type WorkspaceLock struct {
	flockHandle *flock.Flock
}

// NewWorkspaceLock constructs a WorkspaceLock for the workspace at root. The
// lock file is at <root>/.tusk/lock.
func NewWorkspaceLock(root string) (*WorkspaceLock, error) {
	lockPath := filepath.Join(root, ".tusk", LockFilename)

	if mkErr := os.MkdirAll(filepath.Dir(lockPath), 0o755); mkErr != nil {
		return nil, fmt.Errorf("lock: ensure dir: %w", mkErr)
	}

	return &WorkspaceLock{flockHandle: flock.New(lockPath)}, nil
}

// Acquire blocks until the lock is obtained or ctx is cancelled. The poll
// interval is 50ms; cancellation (including deadline expiry) is checked
// between polls via ctx.Done().
func (lockHandle *WorkspaceLock) Acquire(ctx context.Context) error {
	for {
		acquired, tryErr := lockHandle.flockHandle.TryLock()

		if tryErr != nil {
			return fmt.Errorf("lock: try: %w", tryErr)
		}

		if acquired {
			return nil
		}

		select {
		case <-ctx.Done():
			return ErrBusy
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Release releases the lock if held. Idempotent.
func (lockHandle *WorkspaceLock) Release() error {
	return lockHandle.flockHandle.Unlock()
}
