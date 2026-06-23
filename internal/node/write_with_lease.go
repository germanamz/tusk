package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/google/uuid"
)

// MutationKind names the three shapes a Mutator may return from
// WriteWithLease: replace the file's contents, tombstone it (soft
// delete), or signal no change at all.
type MutationKind int

const (
	// MutationReplace asks WriteWithLease to stage new bytes and rename
	// over the target path.
	MutationReplace MutationKind = iota

	// MutationTombstone asks WriteWithLease to remove the on-disk file
	// and transition the file_state row to state='tombstone'. The row
	// itself remains as an audit record.
	MutationTombstone

	// MutationNoChange asks WriteWithLease to release the lease without
	// touching the file or the observed-state columns.
	MutationNoChange
)

// Mutation is the outcome of a Mutator call. Only Content is consulted,
// and only when Kind == MutationReplace.
type Mutation struct {
	Kind    MutationKind
	Content []byte
}

// WriteReplace builds a Mutation that replaces the file with content.
func WriteReplace(content []byte) Mutation {
	return Mutation{Kind: MutationReplace, Content: content}
}

// WriteTombstone builds a Mutation that removes the file and marks the
// file_state row as tombstoned.
func WriteTombstone() Mutation {
	return Mutation{Kind: MutationTombstone}
}

// WriteNoChange builds a Mutation that leaves the file untouched and
// releases the lease without committing observed-state changes.
func WriteNoChange() Mutation {
	return Mutation{Kind: MutationNoChange}
}

// Mutator transforms the bytes currently on disk into a Mutation. When
// the target file does not exist, current is nil — the mutator decides
// whether that is an error for its operation. Returning a non-nil error
// aborts the write; WriteWithLease releases the lease via the abandon
// path and propagates the error.
type Mutator func(current []byte) (Mutation, error)

// WriteWithLease performs a lease-protected, atomically-renamed write
// to relPath under root. It is the single helper that handler
// conversions (T4.2-4.5) route their mutation logic through.
//
// The flow per the spec § Write flow:
//
//  1. Lazy-insert a placeholder file_state row so pre-existing nodes
//     (no row yet) become claimable.
//  2. Claim the lease (auto-cleans any stale .tusk/staging/ temp left
//     by a crashed predecessor).
//  3. Read the current on-disk bytes (lease guarantees no in-tusk
//     writer interleaves).
//  4. Invoke mutator on those bytes.
//  5. Dispatch by Mutation.Kind:
//     - Replace: stage to .tusk/staging/<uuid>, record pending_*,
//     os.Rename over the target, then release with new
//     content_hash/mtime_ns/size.
//     - Tombstone: os.Remove the file, then release with
//     state='tombstone'.
//     - NoChange: release without updating observed state.
//
// Any error after a successful Claim takes the abandon path: the lease
// and pending_* columns clear, observed-state columns stay where they
// were. ErrBusy from Claim is propagated unchanged — handlers decide
// whether to wait or surface the busy state.
func WriteWithLease(
	ctx context.Context,
	root string,
	repo *index.FileStateRepo,
	workerID string,
	ttl time.Duration,
	relPath string,
	mutator Mutator,
) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}

	if ensureErr := repo.EnsurePlaceholder(relPath); ensureErr != nil {
		return ensureErr
	}

	lease, claimErr := repo.Claim(relPath, workerID, ttl)

	if claimErr != nil {
		return claimErr
	}

	_ = lease

	absPath := filepath.Join(root, relPath)

	current, readErr := os.ReadFile(absPath)

	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return abandonLease(repo, relPath, workerID, fmt.Errorf("node: read %s: %w", relPath, readErr))
	}

	if errors.Is(readErr, os.ErrNotExist) {
		current = nil
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return abandonLease(repo, relPath, workerID, ctxErr)
	}

	mutation, mutErr := mutator(current)

	if mutErr != nil {
		return abandonLease(repo, relPath, workerID, mutErr)
	}

	switch mutation.Kind {
	case MutationReplace:
		return commitReplace(ctx, repo, root, absPath, relPath, workerID, mutation.Content)

	case MutationTombstone:
		return commitTombstone(repo, absPath, relPath, workerID)

	case MutationNoChange:
		return releaseNoChange(repo, relPath, workerID)

	default:
		return abandonLease(repo, relPath, workerID,
			fmt.Errorf("node: write-with-lease %s: invalid mutation kind %d", relPath, mutation.Kind))
	}
}

func commitReplace(
	ctx context.Context,
	repo *index.FileStateRepo,
	root, absPath, relPath, workerID string,
	content []byte,
) error {
	stagingDir := filepath.Join(root, ".tusk", "staging")

	if mkErr := os.MkdirAll(stagingDir, 0o755); mkErr != nil {
		return abandonLease(repo, relPath, workerID,
			fmt.Errorf("node: mkdir %s: %w", stagingDir, mkErr))
	}

	tempPath := filepath.Join(stagingDir, uuid.NewString())
	newHash := sha256Hex(content)

	if writeErr := os.WriteFile(tempPath, content, 0o644); writeErr != nil {
		return abandonLease(repo, relPath, workerID,
			fmt.Errorf("node: stage %s: %w", relPath, writeErr))
	}

	if pendingErr := repo.SetPending(relPath, workerID, tempPath, newHash); pendingErr != nil {
		_ = os.Remove(tempPath)

		return abandonLease(repo, relPath, workerID, pendingErr)
	}

	if mkErr := os.MkdirAll(filepath.Dir(absPath), 0o755); mkErr != nil {
		_ = os.Remove(tempPath)

		return abandonLease(repo, relPath, workerID,
			fmt.Errorf("node: mkdir %s: %w", filepath.Dir(absPath), mkErr))
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		_ = os.Remove(tempPath)

		return abandonLease(repo, relPath, workerID, ctxErr)
	}

	if renameErr := os.Rename(tempPath, absPath); renameErr != nil {
		_ = os.Remove(tempPath)

		return abandonLease(repo, relPath, workerID,
			fmt.Errorf("node: rename %s: %w", relPath, renameErr))
	}

	stat, statErr := os.Stat(absPath)

	if statErr != nil {
		return abandonLease(repo, relPath, workerID,
			fmt.Errorf("node: stat %s: %w", relPath, statErr))
	}

	if releaseErr := repo.Release(index.ReleaseContext{
		Path:        relPath,
		WorkerID:    workerID,
		Success:     true,
		State:       index.FileStateLive,
		ContentHash: newHash,
		MtimeNs:     stat.ModTime().UnixNano(),
		Size:        stat.Size(),
	}); releaseErr != nil {
		return fmt.Errorf("node: write-with-lease %s: release commit: %w", relPath, releaseErr)
	}

	return nil
}

func commitTombstone(repo *index.FileStateRepo, absPath, relPath, workerID string) error {
	if rmErr := os.Remove(absPath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		return abandonLease(repo, relPath, workerID,
			fmt.Errorf("node: remove %s: %w", relPath, rmErr))
	}

	if releaseErr := repo.Release(index.ReleaseContext{
		Path:        relPath,
		WorkerID:    workerID,
		Success:     true,
		State:       index.FileStateTombstone,
		ContentHash: "",
		MtimeNs:     time.Now().UnixNano(),
		Size:        0,
	}); releaseErr != nil {
		return fmt.Errorf("node: write-with-lease %s: release tombstone: %w", relPath, releaseErr)
	}

	return nil
}

func releaseNoChange(repo *index.FileStateRepo, relPath, workerID string) error {
	if releaseErr := repo.Release(index.ReleaseContext{
		Path:     relPath,
		WorkerID: workerID,
		Success:  false,
	}); releaseErr != nil {
		return fmt.Errorf("node: write-with-lease %s: release no-change: %w", relPath, releaseErr)
	}

	return nil
}

// abandonLease releases the lease on the abandon path and returns cause
// (wrapped if the release itself failed). The abandon path clears the
// lease and pending_* columns without touching observed-state columns,
// matching the spec § Write flow's failure semantics.
func abandonLease(repo *index.FileStateRepo, relPath, workerID string, cause error) error {
	if releaseErr := repo.Release(index.ReleaseContext{
		Path:     relPath,
		WorkerID: workerID,
		Success:  false,
	}); releaseErr != nil {
		return fmt.Errorf("node: write-with-lease %s: abandon: %w (cause: %v)", relPath, releaseErr, cause)
	}

	return cause
}
