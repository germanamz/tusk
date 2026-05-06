// Package reindex walks a workspace, parses every markdown node, and brings the
// index up to date with what is on disk.
package reindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/ignore"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

// Config configures Run.
type Config struct {
	Root            string             // workspace root
	Repo            *index.NodeRepo    // node index repository
	Edges           *index.EdgeRepo    // edge index repository (optional; when nil, edges are not written)
	EdgeTypes       manifest.EdgeTypes // declared edge types (optional; when empty, frontmatter edges are not resolved)
	WorkspaceIgnore []string           // patterns from [workspace] ignore in tusk.toml

	// Embedding pipeline (optional). When all four are set, Run drains the
	// embed_queue at the end of the pass by invoking Embedder for each node.
	EmbedQueue    *index.EmbedQueueRepo
	EmbeddingRepo *index.EmbeddingRepo
	Embedder      embed.Embedder
	Chunker       embed.ChunkingStrategy

	// Meta is optional; when set, Run records `last_reindex_at` (unix nanoseconds
	// formatted as decimal string) at the end of every successful pass.
	Meta *index.MetaRepo
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

	matcher, matcherErr := ignore.NewMatcher(config.Root, config.WorkspaceIgnore)

	if matcherErr != nil {
		return nil, fmt.Errorf("reindex: build ignore matcher: %w", matcherErr)
	}

	walkErr := filepath.WalkDir(config.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, relErr := filepath.Rel(config.Root, path)

		if relErr != nil {
			return relErr
		}

		relPath = filepath.ToSlash(relPath)

		// Always allow the walk to start at the root.
		if relPath != "." {
			if matcher.Matches(relPath, entry.IsDir()) {
				if entry.IsDir() {
					return filepath.SkipDir
				}

				return nil
			}
		}

		if entry.IsDir() {
			return nil
		}

		if filepath.Ext(path) != ".md" {
			return nil
		}

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

	if config.Embedder != nil {
		// Enqueue every indexed node so the drain loop covers them.
		for path := range seenPaths {
			id := strings.TrimSuffix(path, ".md")
			_ = config.EmbedQueue.Enqueue(id)
		}

		if _, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
			Root:       config.Root,
			Nodes:      config.Repo,
			Queue:      config.EmbedQueue,
			Embeddings: config.EmbeddingRepo,
			Embedder:   config.Embedder,
			Chunker:    config.Chunker,
		}); drainErr != nil {
			return nil, drainErr
		}
	}

	if config.Meta != nil {
		if setErr := config.Meta.Set("last_reindex_at", fmt.Sprintf("%d", time.Now().UnixNano())); setErr != nil {
			return nil, fmt.Errorf("reindex: record last_reindex_at: %w", setErr)
		}
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
