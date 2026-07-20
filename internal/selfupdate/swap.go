package selfupdate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// backupSuffix names the moved-aside previous binary. On Windows the running
// image cannot be deleted, so a leftover with this suffix is expected and is
// swept at the start of the next update.
const backupSuffix = ".old"

// stagePrefix marks the partially written replacement binary. Staging happens
// in the target's own directory so the final rename is a same-filesystem
// operation and therefore atomic.
const stagePrefix = ".tusk-update-"

// stagingGrace is how long a staging file is left alone before it is treated
// as abandoned. It must comfortably exceed the time to copy a binary, so a
// concurrent update's in-flight staging file is never swept.
const stagingGrace = time.Hour

// preflightWritable checks that the binary's directory accepts writes before
// anything is downloaded. Failing here costs a syscall; failing after the
// download costs the user a multi-megabyte transfer.
func preflightWritable(targetPath string) error {
	dir := filepath.Dir(targetPath)

	probe, createErr := os.CreateTemp(dir, stagePrefix+"probe-*")

	if createErr != nil {
		return fmt.Errorf("%w: cannot write to %s: %w\n"+
			"  Re-run with elevated privileges, or reinstall somewhere you own:\n"+
			"    curl -fsSL https://raw.githubusercontent.com/germanamz/tusk/main/install.sh | INSTALL_DIR=~/.local/bin sh",
			ErrPermission, dir, createErr)
	}

	name := probe.Name()

	probe.Close()

	if removeErr := os.Remove(name); removeErr != nil {
		return fmt.Errorf("%w: cannot clean up probe file %s: %w", ErrPermission, name, removeErr)
	}

	return nil
}

// stageBinary copies the extracted binary next to the target so the swap is
// a same-filesystem rename. Returns the staged path.
func stageBinary(sourcePath string, targetPath string) (string, error) {
	dir := filepath.Dir(targetPath)

	staged, createErr := os.CreateTemp(dir, stagePrefix+"*")

	if createErr != nil {
		return "", fmt.Errorf("%w: staging replacement in %s: %w", ErrPermission, dir, createErr)
	}

	// Match whatever the current binary allows rather than assuming 0755: a
	// deliberately restricted install (0750 in a shared directory, say) must
	// not be silently widened by updating it. The owner-execute bit is forced
	// on regardless, since an unrunnable binary is not a working update.
	mode := os.FileMode(0o755)

	if info, statErr := os.Stat(targetPath); statErr == nil {
		mode = info.Mode().Perm() | 0o100
	}

	stagedPath := staged.Name()

	source, openErr := os.Open(sourcePath)

	if openErr != nil {
		staged.Close()
		_ = os.Remove(stagedPath)

		return "", fmt.Errorf("opening %s: %w", sourcePath, openErr)
	}

	defer source.Close()

	_, copyErr := io.Copy(staged, source)

	if copyErr != nil {
		staged.Close()
		_ = os.Remove(stagedPath)

		return "", fmt.Errorf("%w: staging replacement binary: %w", ErrPermission, copyErr)
	}

	if syncErr := staged.Sync(); syncErr != nil {
		staged.Close()
		_ = os.Remove(stagedPath)

		return "", fmt.Errorf("%w: flushing staged binary: %w", ErrPermission, syncErr)
	}

	if closeErr := staged.Close(); closeErr != nil {
		_ = os.Remove(stagedPath)

		return "", fmt.Errorf("%w: closing staged binary: %w", ErrPermission, closeErr)
	}

	// CreateTemp makes the file 0600; the replacement has to be runnable.
	if chmodErr := os.Chmod(stagedPath, mode); chmodErr != nil {
		_ = os.Remove(stagedPath)

		return "", fmt.Errorf("%w: making staged binary executable: %w", ErrPermission, chmodErr)
	}

	return stagedPath, nil
}

// swapBinary replaces targetPath with stagedPath, keeping the previous binary
// aside until the swap succeeds.
//
// The move-aside-then-rename sequence is used on every platform rather than
// branching per OS. On Unix a plain rename over a running binary would work —
// the process holds the inode — but the uniform path gives real rollback
// everywhere. On Windows it is mandatory: a running .exe cannot be
// overwritten, only renamed out of the way.
func swapBinary(stagedPath string, targetPath string) error {
	// Sweep leftovers from an earlier run before claiming a new slot: on
	// Windows the previous update could not delete its own backup, because
	// the image was still running.
	sweepLeftovers(targetPath)

	// The backup name is per-run rather than a fixed ".old". Two concurrent
	// updates sharing one backup path would clobber each other's rollback
	// copy, and on Windows a backup still locked by a running process would
	// permanently occupy the only slot and block every future update.
	backupPath := fmt.Sprintf("%s%s.%d", targetPath, backupSuffix, os.Getpid())

	if renameErr := os.Rename(targetPath, backupPath); renameErr != nil {
		_ = os.Remove(stagedPath)

		return fmt.Errorf("%w: moving %s aside: %w", ErrPermission, targetPath, renameErr)
	}

	if renameErr := os.Rename(stagedPath, targetPath); renameErr != nil {
		// Put the original back before reporting: leaving no binary at
		// targetPath would uninstall tusk rather than fail to update it.
		restoreErr := os.Rename(backupPath, targetPath)

		_ = os.Remove(stagedPath)

		if restoreErr != nil {
			return fmt.Errorf("%w: installing %s failed (%w) and the previous binary could not be restored (%w)\n"+
				"  The previous binary is at %s — move it back manually",
				ErrPermission, targetPath, renameErr, restoreErr, backupPath)
		}

		return fmt.Errorf("%w: installing %s: %w (previous binary restored)", ErrPermission, targetPath, renameErr)
	}

	_ = os.Remove(backupPath)

	return nil
}

// sweepLeftovers best-effort removes backups and staging files abandoned by
// earlier runs. Every removal is advisory: on Windows a backup is the
// still-running image and cannot be deleted until that process exits, and a
// concurrent update's live staging file must not be ripped out from under it.
//
// Staging files are age-gated for exactly that reason — a file younger than
// stagingGrace probably belongs to an update running right now.
func sweepLeftovers(targetPath string) {
	backups, _ := filepath.Glob(targetPath + backupSuffix + "*")

	for _, backup := range backups {
		_ = os.Remove(backup)
	}

	staged, _ := filepath.Glob(filepath.Join(filepath.Dir(targetPath), stagePrefix+"*"))

	for _, candidate := range staged {
		info, statErr := os.Stat(candidate)

		if statErr != nil || time.Since(info.ModTime()) < stagingGrace {
			continue
		}

		_ = os.Remove(candidate)
	}
}

// resolveTarget returns the absolute, symlink-resolved path of the running
// binary. Symlinks are followed so that updating through a linked name
// replaces the real file rather than turning the link into a regular file.
func resolveTarget(execPath string) (string, error) {
	absolute, absErr := filepath.Abs(execPath)

	if absErr != nil {
		return "", fmt.Errorf("resolving %s: %w", execPath, absErr)
	}

	resolved, linkErr := filepath.EvalSymlinks(absolute)

	if linkErr != nil {
		// A broken or unreadable link is not fatal here: fall back to the
		// absolute path and let the swap surface any real problem.
		return absolute, nil
	}

	return resolved, nil
}
