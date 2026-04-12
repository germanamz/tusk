// Package sqlite — registry.go owns the StoreRegistry, which resolves
// project IDs to lazily-opened *Store instances keyed by absolute path.
package sqlite

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/germanamz/tusk/config"
)

// StoreRegistry resolves project IDs to lazily-opened SQLite stores.
// Projects without an explicit db_path share the default store.
type StoreRegistry struct {
	defaultPath string
	baseDir     string
	projects    map[string]config.ProjectConfig
	migrations  fs.FS

	mu     sync.Mutex
	stores map[string]*Store
}

// NewStoreRegistry creates a registry. The default store is opened eagerly;
// per-project stores are opened on first access. baseDir is the directory
// used to resolve relative db_path values (typically the directory holding
// the effective config file).
func NewStoreRegistry(defaultPath, baseDir string, projects map[string]config.ProjectConfig, migrations fs.FS) (*StoreRegistry, error) {
	abs, err := resolveDBPath(defaultPath, baseDir)
	if err != nil {
		return nil, err
	}
	reg := &StoreRegistry{
		defaultPath: abs,
		baseDir:     baseDir,
		projects:    projects,
		migrations:  migrations,
		stores:      make(map[string]*Store),
	}
	if _, err := reg.openPath(abs); err != nil {
		return nil, err
	}
	return reg, nil
}

// Get returns the store that handles the given project ID.
func (r *StoreRegistry) Get(projectID string) (*Store, error) {
	r.mu.Lock()
	proj, ok := r.projects[projectID]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown project %q", projectID)
	}
	path := r.defaultPath
	if proj.DBPath != "" {
		abs, err := resolveDBPath(proj.DBPath, r.baseDir)
		if err != nil {
			return nil, err
		}
		path = abs
	}
	return r.openPath(path)
}

// Default returns the default store.
func (r *StoreRegistry) Default() (*Store, error) {
	return r.openPath(r.defaultPath)
}

// ProjectIDs returns the set of project IDs known to the registry, sorted.
func (r *StoreRegistry) ProjectIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0, len(r.projects))
	for id := range r.projects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Close closes every opened store. Safe to call multiple times.
func (r *StoreRegistry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var firstErr error
	for p, s := range r.stores {
		if err := s.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("closing %s: %w", p, err)
		}
		delete(r.stores, p)
	}
	return firstErr
}

func (r *StoreRegistry) openPath(path string) (*Store, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.stores[path]; ok {
		return s, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating db dir: %w", err)
	}
	s, err := New(path, r.migrations)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	r.stores[path] = s
	return s, nil
}

// resolveDBPath expands ~ and returns an absolute path. Relative paths are
// resolved against baseDir. Absolute paths are returned as-is.
func resolveDBPath(path, baseDir string) (string, error) {
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
