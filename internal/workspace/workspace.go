// Package workspace discovers the Tusk workspace root by walking up from a
// starting directory looking for tusk.toml.
package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ManifestFilename is the canonical manifest file name at the workspace root.
const ManifestFilename = "tusk.toml"

// IndexDirname is the gitignored directory holding the local index database.
const IndexDirname = ".tusk"

// IndexFilename is the SQLite index file name inside IndexDirname.
const IndexFilename = "index.db"

// ErrNotFound matches (via errors.Is) the *NotFoundError Find returns when no
// tusk.toml is found by walking up.
var ErrNotFound = errors.New("workspace: no tusk.toml found")

// NotFoundError is the error Find returns when the walk reaches the filesystem
// root without finding tusk.toml. It names the directory the walk started
// from, which an MCP client that launched the server in / would not otherwise
// show.
type NotFoundError struct {
	Dir string // absolute directory the walk started from
}

func (notFound *NotFoundError) Error() string {
	return fmt.Sprintf("no tusk.toml in %s or any parent directory", notFound.Dir)
}

// Is reports ErrNotFound as a match, so callers keep testing with errors.Is.
func (notFound *NotFoundError) Is(target error) bool {
	return target == ErrNotFound
}

// Workspace describes a located Tusk workspace on disk.
type Workspace struct {
	Root         string // absolute path to workspace root
	ManifestPath string // absolute path to tusk.toml
	IndexPath    string // absolute path to .tusk/index.db (may not yet exist)
}

// Find walks up from startDir looking for tusk.toml. Returns a *NotFoundError
// (matching ErrNotFound) once it reaches the filesystem root without finding
// one.
func Find(startDir string) (*Workspace, error) {
	current, absErr := filepath.Abs(startDir)

	if absErr != nil {
		return nil, absErr
	}

	start := current

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
			return nil, &NotFoundError{Dir: start}
		}

		current = parent
	}
}
