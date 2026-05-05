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
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

// Config configures Run.
type Config struct {
	Root      string             // workspace root
	Repo      *index.NodeRepo    // node index repository
	Edges     *index.EdgeRepo    // edge index repository (optional; when nil, edges are not written)
	EdgeTypes manifest.EdgeTypes // declared edge types (optional; when empty, frontmatter edges are not resolved)
}

// Report summarizes a reindex pass.
type Report struct {
	Indexed int // number of node files freshly indexed or refreshed
	Removed int // number of stale rows deleted (file no longer on disk)
	Skipped int // number of files skipped (parse error or off-schema)
}

// Run walks Root, parses every *.md file with valid frontmatter, and upserts
// or removes index rows so the index matches what is on disk. When Edges and
// EdgeTypes are configured, edges are written and removed alongside nodes.
func Run(config Config) (*Report, error) {
	report := &Report{}
	seenPaths := map[string]struct{}{}

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

		if resolveErr := node.ResolveEdges(parsed, config.EdgeTypes); resolveErr != nil {
			report.Skipped++

			return nil
		}

		if _, hasReferences := config.EdgeTypes["references"]; hasReferences {
			for _, target := range node.ExtractWikilinks(parsed.Body) {
				parsed.Edges["references"] = appendUnique(parsed.Edges["references"], target)
			}
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

		if config.Edges != nil {
			edgeRows := flattenEdges(parsed)

			if upsertErr := config.Edges.UpsertAll(parsed.ID, parsed.Path, edgeRows); upsertErr != nil {
				return upsertErr
			}
		}

		seenPaths[parsed.Path] = struct{}{}
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
		if _, kept := seenPaths[row.Path]; kept {
			continue
		}

		if deleteErr := config.Repo.DeleteByPath(row.Path); deleteErr != nil {
			return nil, deleteErr
		}

		if config.Edges != nil {
			if deleteErr := config.Edges.DeleteBySource(row.ID); deleteErr != nil {
				return nil, deleteErr
			}
		}

		report.Removed++
	}

	return report, nil
}

// appendUnique appends value to slice only if not already present.
func appendUnique(slice []string, value string) []string {
	for _, existing := range slice {
		if existing == value {
			return slice
		}
	}

	return append(slice, value)
}

// flattenEdges turns parsed.Edges into the EdgeRow shape expected by EdgeRepo.
func flattenEdges(parsedNode *node.Node) []index.EdgeRow {
	var rows []index.EdgeRow

	for edgeType, targets := range parsedNode.Edges {
		for ordinal, target := range targets {
			rows = append(rows, index.EdgeRow{
				Type:       edgeType,
				SourceID:   parsedNode.ID,
				TargetID:   target,
				Ordinal:    ordinal,
				SourcePath: parsedNode.Path,
			})
		}
	}

	return rows
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

	return strings.HasPrefix(name, ".")
}
