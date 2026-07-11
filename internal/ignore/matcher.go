// Package ignore decides which paths the reindex walker skips. It combines:
//  1. Built-in ignores: .tusk/, .git/
//  2. The workspace's .gitignore (if present at the root)
//  3. Patterns from [workspace] ignore in tusk.toml
//
// Plan 3 reads only the root .gitignore. Nested .gitignore files in
// subdirectories are a future improvement (Plan 8 doctor flags will surface
// when nested ignores would have been relevant).
package ignore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
)

// Matcher decides whether a workspace-relative path is excluded from indexing.
type Matcher interface {
	// Matches returns true when relPath (workspace-relative, forward-slash) is
	// ignored. The isDir flag affects pattern matching for directory-only
	// entries (`foo/`).
	Matches(relPath string, isDir bool) bool
}

// builtinIgnores are always applied; users cannot disable them.
var builtinIgnores = []string{
	".tusk/",
	".git/",
}

// builtinPatterns matches ONLY the always-on built-in ignores, with no
// .gitignore or workspace patterns layered on. It backs WithinBuiltinIgnore.
var builtinPatterns = gitignore.CompileIgnoreLines(builtinIgnores...)

// WithinBuiltinIgnore reports whether relPath (workspace-relative, forward
// slash) falls inside a directory tusk always ignores — its internal state dir
// (.tusk/) or the VCS dir (.git/). It is the write-surface guard for node
// Create / Rename: a file authored there can never be indexed (the reindex walk
// skips the whole tree) yet the write surface would still mint a row for it,
// leaving a phantom the orphan reaper cannot see (#686). Only the built-ins are
// consulted — user .gitignore / workspace patterns are advisory for the walk,
// not a hard write barrier.
func WithinBuiltinIgnore(relPath string) bool {
	return builtinPatterns.MatchesPath(relPath)
}

// matcher is the standard Matcher implementation.
type matcher struct {
	patterns *gitignore.GitIgnore
}

// NewMatcher reads workspaceRoot/.gitignore (if present), layers
// workspaceIgnore patterns on top, and prepends the built-in ignores.
func NewMatcher(workspaceRoot string, workspaceIgnore []string) (Matcher, error) {
	patternLines := append([]string{}, builtinIgnores...)

	gitignorePath := filepath.Join(workspaceRoot, ".gitignore")
	body, readErr := os.ReadFile(gitignorePath)

	if readErr != nil && !os.IsNotExist(readErr) {
		return nil, fmt.Errorf("ignore: read %s: %w", gitignorePath, readErr)
	}

	if readErr == nil {
		patternLines = append(patternLines, splitLines(string(body))...)
	}

	patternLines = append(patternLines, workspaceIgnore...)

	compiled := gitignore.CompileIgnoreLines(patternLines...)

	return &matcher{patterns: compiled}, nil
}

func (instance *matcher) Matches(relPath string, isDir bool) bool {
	probe := relPath

	if isDir && !strings.HasSuffix(probe, "/") {
		probe = probe + "/"
	}

	return instance.patterns.MatchesPath(probe)
}

func splitLines(body string) []string {
	var lines []string

	for _, raw := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(raw)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		lines = append(lines, trimmed)
	}

	return lines
}
