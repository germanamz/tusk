package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateDocsProducesManAndMarkdown(test *testing.T) {
	tempDir := test.TempDir()
	manDir := filepath.Join(tempDir, "man")
	docDir := filepath.Join(tempDir, "docs", "cli")

	if generateErr := generateDocs(manDir, docDir); generateErr != nil {
		test.Fatalf("generateDocs: %v", generateErr)
	}

	wantMan := []string{
		"tusk.1",
		"tusk-init.1",
		"tusk-node.1",
		"tusk-node-create.1",
	}

	for _, name := range wantMan {
		path := filepath.Join(manDir, name)

		info, statErr := os.Stat(path)

		if statErr != nil {
			test.Errorf("missing man page %s: %v", name, statErr)

			continue
		}

		if info.Size() == 0 {
			test.Errorf("empty man page %s", name)
		}
	}

	wantMarkdown := []string{
		"tusk.md",
		"tusk_init.md",
		"tusk_node.md",
		"tusk_node_create.md",
		"README.md",
	}

	for _, name := range wantMarkdown {
		path := filepath.Join(docDir, name)

		info, statErr := os.Stat(path)

		if statErr != nil {
			test.Errorf("missing markdown page %s: %v", name, statErr)

			continue
		}

		if info.Size() == 0 {
			test.Errorf("empty markdown page %s", name)
		}
	}

	// The hidden docgen command must not document itself.
	if _, statErr := os.Stat(filepath.Join(manDir, "tusk-docgen.1")); !os.IsNotExist(statErr) {
		test.Errorf("tusk-docgen.1 should not be generated (hidden command), stat err = %v", statErr)
	}
}
