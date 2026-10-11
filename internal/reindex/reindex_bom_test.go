package reindex_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
)

// writeRaw writes exact bytes to a workspace file, creating parent dirs. Unlike
// writeNode it does not wrap the content in frontmatter, so a test can place a
// UTF-8 BOM at the very head of the file.
func writeRaw(test *testing.T, root, relPath string, content []byte) {
	test.Helper()

	abs := filepath.Join(root, relPath)

	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(abs, content, 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}
}

// TestRun_BOMFileIsIndexedNotSilentlyDropped guards #682 item 2: a UTF-8 BOM at
// the head of a typed file must not cause the whole file to be silently dropped
// from the index, and a BOM at the head of the body must not demote the first
// heading to a paragraph. Both files must index the same as a plain control.
func TestRun_BOMFileIsIndexedNotSilentlyDropped(test *testing.T) {
	root := test.TempDir()

	const bom = "\xef\xbb\xbf"

	// File-start BOM: BOM then frontmatter then a heading + body.
	writeRaw(test, root, "bom.md", []byte(bom+"---\ntype: note\n---\n# Title\n\nbody text here\n"))
	// Body-start BOM: frontmatter, then a BOM before the first heading.
	writeRaw(test, root, "bodybom.md", []byte("---\ntype: note\n---\n"+bom+"# Title\n\nbody text here\n"))
	// Control: no BOM anywhere.
	writeRaw(test, root, "plain.md", []byte("---\ntype: note\n---\n# Title\n\nbody text here\n"))

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))
	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)

	loaded := &manifest.Manifest{}
	manifest.MergeBuiltinPacks(loaded)

	cfg := withGen(store, reindex.Config{
		Root:      root,
		Repo:      nodes,
		Edges:     edges,
		EdgeTypes: loaded.EdgeTypes,
		Manifest:  loaded,
	})

	report, runErr := reindex.Run(cfg)
	if runErr != nil {
		test.Fatalf("reindex.Run: %v", runErr)
	}

	if report.Indexed != 3 {
		test.Errorf("Indexed = %d, want 3 (no file silently skipped); report = %+v", report.Indexed, report)
	}

	// Every file must produce a file-level node row.
	for _, id := range []string{"bom", "bodybom", "plain"} {
		if _, getErr := nodes.Get(id); getErr != nil {
			test.Errorf("node %q missing after reindex: %v", id, getErr)
		}
	}

	// Every file's first heading must parse as a section sub-unit (a demoted
	// heading would show up as a paragraph instead).
	for _, id := range []string{"bom", "bodybom", "plain"} {
		subs, listErr := nodes.ListSubUnitsForFile(id)
		if listErr != nil {
			test.Fatalf("ListSubUnitsForFile %q: %v", id, listErr)
		}

		var sawSection bool

		for _, sub := range subs {
			if sub.Type == "section" && sub.Title == "Title" {
				sawSection = true
			}
		}

		if !sawSection {
			test.Errorf("file %q has no section 'Title' sub-unit; heading was demoted. subs = %+v", id, subs)
		}
	}
}

// TestRun_UnparseableFileIsNamedInSkipLog guards #682 item 2: a file whose
// frontmatter cannot be decoded is dropped from the index and counted only as
// an anonymous "skipped" in the report. The worker must name it in a WARN so
// the omission is not silent and invisible to every query. A file that is
// simply not a node is the opposite case — see TestRun_NonNodesSkipQuietly.
func TestRun_UnparseableFileIsNamedInSkipLog(test *testing.T) {
	root := test.TempDir()

	// Undecodable frontmatter → a real parse failure → skipped and warned.
	writeRaw(test, root, "broken.md", []byte(brokenFrontmatter))
	// A healthy control so the pass still indexes something.
	writeRaw(test, root, "good.md", []byte("---\ntype: note\n---\n# Ok\n\nbody\n"))

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))
	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := withGen(store, reindex.Config{
		Root:   root,
		Repo:   index.NewNodeRepo(store),
		Logger: logger,
	})

	report, runErr := reindex.Run(cfg)
	if runErr != nil {
		test.Fatalf("reindex.Run: %v", runErr)
	}

	if report.Skipped < 1 {
		test.Errorf("Skipped = %d, want >= 1", report.Skipped)
	}

	out := buf.String()

	if !strings.Contains(out, "reindex skip: unparseable file") {
		test.Errorf("expected a skip WARN; got %q", out)
	}

	if !strings.Contains(out, "broken.md") {
		test.Errorf("skip WARN did not name the offending file; got %q", out)
	}
}

// TestRun_NonNodesSkipQuietly guards #789: markdown with no frontmatter, or
// frontmatter with no type, is not a node — vaults keep such files on purpose.
// They are counted as skipped and recorded for doctor by #759's rule, but a
// first reindex of an existing folder of notes must not print one WARN per
// file, which reads as "tusk can't parse my notes".
func TestRun_NonNodesSkipQuietly(test *testing.T) {
	root := test.TempDir()

	writeRaw(test, root, "plain.md", []byte("# Just prose\n\nno frontmatter at all\n"))
	writeRaw(test, root, "notyped.md", []byte("---\ntitle: prose only\n---\n\n# No type\n"))
	writeRaw(test, root, "broken.md", []byte(brokenFrontmatter))
	writeRaw(test, root, "good.md", []byte("---\ntype: note\n---\n# Ok\n\nbody\n"))

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))
	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := withGen(store, reindex.Config{
		Root:   root,
		Repo:   index.NewNodeRepo(store),
		Logger: logger,
	})

	report, runErr := reindex.Run(cfg)
	if runErr != nil {
		test.Fatalf("reindex.Run: %v", runErr)
	}

	// All three non-indexed files are still counted, and the healthy one indexed.
	if report.Skipped != 3 {
		test.Errorf("Skipped = %d, want 3", report.Skipped)
	}

	if report.Indexed != 1 {
		test.Errorf("Indexed = %d, want 1", report.Indexed)
	}

	out := buf.String()

	var warnLines []string

	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "level=WARN") {
			warnLines = append(warnLines, line)
		}
	}

	// Only the real parse failure keeps a WARN, and it still names the file.
	if len(warnLines) != 1 || !strings.Contains(warnLines[0], "reindex skip: unparseable file") ||
		!strings.Contains(warnLines[0], "broken.md") {
		test.Errorf("WARN lines = %q, want exactly one unparseable-file WARN naming broken.md", warnLines)
	}

	// The quiet path still says why each file was skipped, at debug level.
	for _, relPath := range []string{"plain.md", "notyped.md"} {
		sawQuietLine := false

		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "reindex skip: not a node") && strings.Contains(line, relPath) {
				sawQuietLine = true

				break
			}
		}

		if !sawQuietLine {
			test.Errorf("no not-a-node debug line for %s; log = %q", relPath, out)
		}
	}
}
