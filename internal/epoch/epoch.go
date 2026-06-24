// Package epoch manages the .tusk epoch sentinels: monotonically increasing
// integers in files separate from the index DB and the in-memory manifest. A
// process reads a sentinel to detect that a sibling reset the index (or
// reloaded the manifest) even when its own DB handle / schema has been orphaned
// by another process's delete-and-recreate. Bump writes atomically (temp file +
// rename). The package holds no process state.
//
// Two sentinels share this machinery, distinguished only by filename:
//
//   - Index    (.tusk/epoch)          — bumped on index reset.
//   - Manifest (.tusk/manifest-epoch) — bumped on manifest reload.
//
// The on-disk format is a single decimal integer followed by a newline. Both
// sentinel filenames and that format are a wire contract with sibling daemons;
// do not change them.
package epoch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// IndexEpochFile is the index-reset sentinel file name inside .tusk/.
const IndexEpochFile = "epoch"

// ManifestEpochFile is the manifest-reload sentinel file name inside .tusk/.
const ManifestEpochFile = "manifest-epoch"

// Epoch is a typed handle over one sentinel file. The two package-level
// values Index and Manifest cover the two sentinels; callers rarely construct
// their own.
type Epoch struct {
	// name is the sentinel file's base name inside .tusk/.
	name string
}

// Index is the handle for the .tusk/epoch (index-reset) sentinel.
var Index = Epoch{name: IndexEpochFile}

// Manifest is the handle for the .tusk/manifest-epoch (manifest-reload)
// sentinel.
var Manifest = Epoch{name: ManifestEpochFile}

// Filename returns the sentinel's base name inside .tusk/ (e.g. "epoch" or
// "manifest-epoch"). Used by the fsnotify fast-watchers to filter events.
func (ep Epoch) Filename() string {
	return ep.name
}

// path is the absolute path to this sentinel inside root's .tusk dir.
func (ep Epoch) path(root string) string {
	return filepath.Join(root, ".tusk", ep.name)
}

// Read returns the current epoch for the workspace at root, or 0 when the
// sentinel file does not yet exist.
func (ep Epoch) Read(root string) (int64, error) {
	data, readErr := os.ReadFile(ep.path(root))

	if errors.Is(readErr, os.ErrNotExist) {
		return 0, nil
	}

	if readErr != nil {
		return 0, fmt.Errorf("epoch %s: read: %w", ep.name, readErr)
	}

	trimmed := strings.TrimSpace(string(data))

	if trimmed == "" {
		return 0, nil
	}

	value, parseErr := strconv.ParseInt(trimmed, 10, 64)

	if parseErr != nil {
		return 0, fmt.Errorf("epoch %s: parse %q: %w", ep.name, trimmed, parseErr)
	}

	return value, nil
}

// Bump increments the epoch by one and writes it atomically (temp file in the
// same directory + rename). Returns the new value. Callers serialize concurrent
// bumps with the workspace lock; absent that, Bump is last-writer-wins.
func (ep Epoch) Bump(root string) (int64, error) {
	dir := filepath.Join(root, ".tusk")

	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return 0, fmt.Errorf("epoch %s: ensure dir: %w", ep.name, mkErr)
	}

	current, readErr := ep.Read(root)

	if readErr != nil {
		return 0, readErr
	}

	next := current + 1

	temp, tempErr := os.CreateTemp(dir, ep.name+".tmp-*")

	if tempErr != nil {
		return 0, fmt.Errorf("epoch %s: temp: %w", ep.name, tempErr)
	}

	tempName := temp.Name()

	if _, writeErr := temp.WriteString(strconv.FormatInt(next, 10) + "\n"); writeErr != nil {
		_ = temp.Close()
		_ = os.Remove(tempName)

		return 0, fmt.Errorf("epoch %s: write temp: %w", ep.name, writeErr)
	}

	if closeErr := temp.Close(); closeErr != nil {
		_ = os.Remove(tempName)

		return 0, fmt.Errorf("epoch %s: close temp: %w", ep.name, closeErr)
	}

	if renameErr := os.Rename(tempName, ep.path(root)); renameErr != nil {
		_ = os.Remove(tempName)

		return 0, fmt.Errorf("epoch %s: rename: %w", ep.name, renameErr)
	}

	return next, nil
}
