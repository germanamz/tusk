package lock_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/lock"
)

func TestAcquire_SucceedsOnFreshWorkspace(test *testing.T) {
	root := test.TempDir()

	if mkErr := os.MkdirAll(filepath.Join(root, ".tusk"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	handle, newErr := lock.NewWorkspaceLock(root)

	if newErr != nil {
		test.Fatalf("NewWorkspaceLock: %v", newErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if acquireErr := handle.Acquire(ctx); acquireErr != nil {
		test.Fatalf("Acquire: %v", acquireErr)
	}

	if releaseErr := handle.Release(); releaseErr != nil {
		test.Errorf("Release: %v", releaseErr)
	}
}

func TestAcquire_BlocksWhileHeldThenSucceedsAfterRelease(test *testing.T) {
	root := test.TempDir()

	if mkErr := os.MkdirAll(filepath.Join(root, ".tusk"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	first, _ := lock.NewWorkspaceLock(root)
	second, _ := lock.NewWorkspaceLock(root)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if acquireErr := first.Acquire(ctx); acquireErr != nil {
		test.Fatalf("first Acquire: %v", acquireErr)
	}

	shortCtx, shortCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer shortCancel()

	if blockedErr := second.Acquire(shortCtx); blockedErr == nil {
		test.Fatalf("second Acquire should have failed while first held the lock")
	}

	first.Release()

	postCtx, postCancel := context.WithTimeout(context.Background(), time.Second)
	defer postCancel()

	if postErr := second.Acquire(postCtx); postErr != nil {
		test.Fatalf("post-release Acquire: %v", postErr)
	}

	second.Release()
}

func TestRelease_IsIdempotent(test *testing.T) {
	root := test.TempDir()

	if mkErr := os.MkdirAll(filepath.Join(root, ".tusk"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	handle, _ := lock.NewWorkspaceLock(root)

	if firstErr := handle.Release(); firstErr != nil {
		test.Errorf("first Release on un-acquired lock: %v", firstErr)
	}

	if secondErr := handle.Release(); secondErr != nil {
		test.Errorf("second Release should be idempotent: %v", secondErr)
	}
}
