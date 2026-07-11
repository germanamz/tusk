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

// TestRun_UnparseableFileIsNamedInSkipLog guards #682 item 2: a file that fails
// to parse is dropped from the index and counted only as an anonymous "skipped"
// in the report. The worker must name it in a WARN so the omission is not
// silent and invisible to every query.
func TestRun_UnparseableFileIsNamedInSkipLog(test *testing.T) {
	root := test.TempDir()

	// No frontmatter → ParseFile fails → the file is skipped.
	writeRaw(test, root, "broken.md", []byte("no frontmatter, just prose\n"))
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
