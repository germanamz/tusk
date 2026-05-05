// Package workspace discovers the Tusk workspace root by walking up from a
// starting directory looking for tusk.toml.
package workspace

import (
	"errors"
	"os"
	"path/filepath"
)

// ManifestFilename is the canonical manifest file name at the workspace root.
const ManifestFilename = "tusk.toml"

// IndexDirname is the gitignored directory holding the local index database.
const IndexDirname = ".tusk"

// IndexFilename is the SQLite index file name inside IndexDirname.
const IndexFilename = "index.db"

// ErrNotFound is returned by Find when no tusk.toml is found by walking up.
var ErrNotFound = errors.New("workspace: no tusk.toml found")

// Workspace describes a located Tusk workspace on disk.
type Workspace struct {
	Root         string // absolute path to workspace root
	ManifestPath string // absolute path to tusk.toml
	IndexPath    string // absolute path to .tusk/index.db (may not yet exist)
}

// Find walks up from startDir looking for tusk.toml. Returns ErrNotFound
// once it reaches the filesystem root without finding one.
func Find(startDir string) (*Workspace, error) {
	current, absErr := filepath.Abs(startDir)

	if absErr != nil {
		return nil, absErr
	}

	for {
		manifestPath := filepath.Join(current, ManifestFilename)
		stat, statErr := os.Stat(manifestPath)

		if statErr == nil && !stat.IsDir() {
			return &Workspace{
				Root:         current,
				ManifestPath: manifestPath,
				IndexPath:    filepath.Join(current, IndexDirname, IndexFilename),
			}, nil
		}

		parent := filepath.Dir(current)

		if parent == current {
			return nil, ErrNotFound
		}

		current = parent
	}
}
