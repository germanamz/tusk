package selfupdate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ManDirFor picks the man-page destination for a binary at targetPath,
// mirroring install.sh: a binary in <prefix>/bin gets <prefix>/share/man, and
// anything else falls back to ~/.local/share/man. The MAN_DIR environment
// variable overrides both, exactly as it does for install.sh.
func ManDirFor(targetPath string) string {
	if override := os.Getenv("MAN_DIR"); override != "" {
		return override
	}

	dir := filepath.Dir(targetPath)

	if filepath.Base(dir) == "bin" {
		return filepath.Join(filepath.Dir(dir), "share", "man")
	}

	home, homeErr := os.UserHomeDir()

	if homeErr != nil {
		return ""
	}

	return filepath.Join(home, ".local", "share", "man")
}

// installManPages copies extracted man pages into manDir/man1. It is
// deliberately best-effort: the binary update has already succeeded by the
// time this runs, and a read-only man directory (/usr/local without sudo,
// say) must not turn a successful update into a failure. The returned error
// is for reporting only — callers surface it as a note.
func installManPages(pages []string, manDir string) error {
	if len(pages) == 0 || manDir == "" {
		return nil
	}

	section := filepath.Join(manDir, "man1")

	if mkdirErr := os.MkdirAll(section, 0o755); mkdirErr != nil {
		return fmt.Errorf("creating %s: %w", section, mkdirErr)
	}

	for _, page := range pages {
		if copyErr := copyFile(page, filepath.Join(section, filepath.Base(page))); copyErr != nil {
			return copyErr
		}
	}

	return nil
}

// copyFile writes source to target, replacing whatever was there.
func copyFile(sourcePath string, targetPath string) error {
	source, openErr := os.Open(sourcePath)

	if openErr != nil {
		return fmt.Errorf("opening %s: %w", sourcePath, openErr)
	}

	defer source.Close()

	target, createErr := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)

	if createErr != nil {
		return fmt.Errorf("creating %s: %w", targetPath, createErr)
	}

	defer target.Close()

	if _, copyErr := io.Copy(target, source); copyErr != nil {
		return fmt.Errorf("writing %s: %w", targetPath, copyErr)
	}

	return nil
}
