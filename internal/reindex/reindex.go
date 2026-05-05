// Package reindex walks a workspace, parses every markdown node, and brings the
// index up to date with what is on disk.
package reindex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
)

// Config configures Run.
type Config struct {
	Root string          // workspace root
	Repo *index.NodeRepo // index repository
}

// Report summarizes a reindex pass.
type Report struct {
	Indexed int // number of node files freshly indexed or refreshed
	Removed int // number of stale rows deleted (file no longer on disk)
	Skipped int // number of files skipped (parse error or off-schema)
}

// Run walks Root, parses every *.md file with valid frontmatter, and upserts
// or removes index rows so the index matches what is on disk.
func Run(config Config) (*Report, error) {
	report := &Report{}
	seen := map[string]struct{}{}

	walkErr := filepath.WalkDir(config.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if shouldSkipDir(config.Root, path, entry.Name()) {
				return filepath.SkipDir
			}

			return nil
		}

		if filepath.Ext(path) != ".md" {
			return nil
		}

		relPath, relErr := filepath.Rel(config.Root, path)

		if relErr != nil {
			return relErr
		}

		// Normalize to forward slashes for cross-platform IDs.
		relPath = filepath.ToSlash(relPath)

		content, readErr := os.ReadFile(path)

		if readErr != nil {
			return fmt.Errorf("reindex: read %s: %w", path, readErr)
		}

		parsed, parseErr := node.ParseFile(relPath, content)

		if parseErr != nil {
			report.Skipped++

			return nil
		}

		stat, statErr := entry.Info()

		if statErr != nil {
			return fmt.Errorf("reindex: stat %s: %w", path, statErr)
		}

		propertiesJSON, marshalErr := json.Marshal(parsed.Properties)

		if marshalErr != nil {
			return fmt.Errorf("reindex: marshal %s: %w", relPath, marshalErr)
		}

		checksum := sha256.Sum256(content)

		if upsertErr := config.Repo.Upsert(index.NodeRow{
			ID:             parsed.ID,
			Type:           parsed.Type,
			Path:           parsed.Path,
			Title:          parsed.Title,
			PropertiesJSON: string(propertiesJSON),
			LastMtime:      stat.ModTime().UnixNano(),
			LastSize:       stat.Size(),
			LastChecksum:   hex.EncodeToString(checksum[:]),
		}); upsertErr != nil {
			return upsertErr
		}

		seen[parsed.Path] = struct{}{}
		report.Indexed++

		return nil
	})

	if walkErr != nil {
		return nil, fmt.Errorf("reindex: walk: %w", walkErr)
	}

	existingRows, listErr := config.Repo.List(index.ListFilter{})

	if listErr != nil {
		return nil, listErr
	}

	for _, row := range existingRows {
		if _, kept := seen[row.Path]; kept {
			continue
		}

		if deleteErr := config.Repo.DeleteByPath(row.Path); deleteErr != nil {
			return nil, deleteErr
		}

		report.Removed++
	}

	return report, nil
}

// shouldSkipDir returns true for directories the walker must not descend into.
// Plan 1b only skips .tusk and .git; .gitignore parsing arrives in Plan 3.
func shouldSkipDir(root, dirPath, name string) bool {
	if dirPath == root {
		return false
	}

	switch name {
	case ".tusk", ".git":
		return true
	}

	if strings.HasPrefix(name, ".") {
		// Skip hidden directories by default; users can opt in via inline manifest later.
		return true
	}

	return false
}
