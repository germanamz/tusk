// Package manifestepoch manages the .tusk/manifest-epoch sentinel: a monotonically
// increasing integer in a file separate from the index DB. A process can read
// it to detect that the manifest was reloaded and recreated even when its own
// in-memory schema has been orphaned by another process's validation-and-swap.
// Bump writes atomically (temp file + rename). The package holds no process state.
package manifestepoch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ManifestEpochFilename is the sentinel file name inside .tusk/.
const ManifestEpochFilename = "manifest-epoch"

func manifestEpochPath(root string) string {
	return filepath.Join(root, ".tusk", ManifestEpochFilename)
}

// Read returns the current manifest epoch for the workspace at root, or 0 when the
// sentinel file does not yet exist.
func Read(root string) (int64, error) {
	data, readErr := os.ReadFile(manifestEpochPath(root))

	if errors.Is(readErr, os.ErrNotExist) {
		return 0, nil
	}

	if readErr != nil {
		return 0, fmt.Errorf("manifestepoch: read: %w", readErr)
	}

	trimmed := strings.TrimSpace(string(data))

	if trimmed == "" {
		return 0, nil
	}

	value, parseErr := strconv.ParseInt(trimmed, 10, 64)

	if parseErr != nil {
		return 0, fmt.Errorf("manifestepoch: parse %q: %w", trimmed, parseErr)
	}

	return value, nil
}

// Bump increments the manifest epoch by one and writes it atomically (temp file in the
// same directory + rename). Returns the new value. Callers serialize concurrent
// bumps with the workspace lock; absent that, Bump is last-writer-wins.
func Bump(root string) (int64, error) {
	dir := filepath.Join(root, ".tusk")

	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return 0, fmt.Errorf("manifestepoch: ensure dir: %w", mkErr)
	}

	current, readErr := Read(root)

	if readErr != nil {
		return 0, readErr
	}

	next := current + 1

	temp, tempErr := os.CreateTemp(dir, ManifestEpochFilename+".tmp-*")

	if tempErr != nil {
		return 0, fmt.Errorf("manifestepoch: temp: %w", tempErr)
	}

	tempName := temp.Name()

	if _, writeErr := temp.WriteString(strconv.FormatInt(next, 10) + "\n"); writeErr != nil {
		_ = temp.Close()
		_ = os.Remove(tempName)

		return 0, fmt.Errorf("manifestepoch: write temp: %w", writeErr)
	}

	if closeErr := temp.Close(); closeErr != nil {
		_ = os.Remove(tempName)

		return 0, fmt.Errorf("manifestepoch: close temp: %w", closeErr)
	}

	if renameErr := os.Rename(tempName, manifestEpochPath(root)); renameErr != nil {
		_ = os.Remove(tempName)

		return 0, fmt.Errorf("manifestepoch: rename: %w", renameErr)
	}

	return next, nil
}
