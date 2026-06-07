// Package indexepoch manages the .tusk/epoch sentinel: a monotonically
// increasing integer in a file separate from the index DB. A process can read
// it to detect that the index was reset and recreated even when its own DB
// handle has been orphaned by another process's delete-and-recreate. Bump
// writes atomically (temp file + rename). The package holds no process state.
package indexepoch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// EpochFilename is the sentinel file name inside .tusk/.
const EpochFilename = "epoch"

func epochPath(root string) string {
	return filepath.Join(root, ".tusk", EpochFilename)
}

// Read returns the current epoch for the workspace at root, or 0 when the
// sentinel file does not yet exist.
func Read(root string) (int64, error) {
	data, readErr := os.ReadFile(epochPath(root))

	if errors.Is(readErr, os.ErrNotExist) {
		return 0, nil
	}

	if readErr != nil {
		return 0, fmt.Errorf("indexepoch: read: %w", readErr)
	}

	trimmed := strings.TrimSpace(string(data))

	if trimmed == "" {
		return 0, nil
	}

	value, parseErr := strconv.ParseInt(trimmed, 10, 64)

	if parseErr != nil {
		return 0, fmt.Errorf("indexepoch: parse %q: %w", trimmed, parseErr)
	}

	return value, nil
}

// Bump increments the epoch by one and writes it atomically (temp file in the
// same directory + rename). Returns the new value. Callers serialize concurrent
// bumps with the workspace lock; absent that, Bump is last-writer-wins.
func Bump(root string) (int64, error) {
	dir := filepath.Join(root, ".tusk")

	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return 0, fmt.Errorf("indexepoch: ensure dir: %w", mkErr)
	}

	current, readErr := Read(root)

	if readErr != nil {
		return 0, readErr
	}

	next := current + 1

	temp, tempErr := os.CreateTemp(dir, EpochFilename+".tmp-*")

	if tempErr != nil {
		return 0, fmt.Errorf("indexepoch: temp: %w", tempErr)
	}

	tempName := temp.Name()

	if _, writeErr := temp.WriteString(strconv.FormatInt(next, 10) + "\n"); writeErr != nil {
		_ = temp.Close()
		_ = os.Remove(tempName)

		return 0, fmt.Errorf("indexepoch: write temp: %w", writeErr)
	}

	if closeErr := temp.Close(); closeErr != nil {
		_ = os.Remove(tempName)

		return 0, fmt.Errorf("indexepoch: close temp: %w", closeErr)
	}

	if renameErr := os.Rename(tempName, epochPath(root)); renameErr != nil {
		_ = os.Remove(tempName)

		return 0, fmt.Errorf("indexepoch: rename: %w", renameErr)
	}

	return next, nil
}
