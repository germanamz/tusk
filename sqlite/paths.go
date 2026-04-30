// Package sqlite — paths.go holds path-resolution helpers used when
// wiring a workspace store from a config file's storage.path.
package sqlite

import (
	"os"
	"path/filepath"
)

// ResolveWorkspacePath expands a leading ~ and returns an absolute path.
// Relative paths are resolved against baseDir. Absolute paths are
// returned untouched (cleaned only). Empty baseDir falls back to the
// process working directory.
func ResolveWorkspacePath(path, baseDir string) (string, error) {
	if len(path) > 1 && path[0] == '~' && (path[1] == '/' || path[1] == os.PathSeparator) {
		home, err := os.UserHomeDir()

		if err != nil {
			return "", err
		}

		path = filepath.Join(home, path[2:])
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	if baseDir == "" {
		return filepath.Abs(path)
	}
	return filepath.Clean(filepath.Join(baseDir, path)), nil
}
