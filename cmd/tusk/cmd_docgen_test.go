package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGenerateDocsProducesManAndMarkdown(test *testing.T) {
	tempDir := test.TempDir()
	manDir := filepath.Join(tempDir, "man")
	docDir := filepath.Join(tempDir, "docs", "cli")

	if generateErr := generateDocs(manDir, docDir, ""); generateErr != nil {
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

// TestGoreleaserCaskListsEveryManPage keeps the Homebrew cask in step with
// the man pages docgen writes. A cask's manpage stanza takes one file and no
// globs, so .goreleaser.yaml lists every page: one left off never reaches brew
// users, and one listed after its command is gone fails `brew install`.
func TestGoreleaserCaskListsEveryManPage(test *testing.T) {
	tempDir := test.TempDir()
	manDir := filepath.Join(tempDir, "man")

	if generateErr := generateDocs(manDir, filepath.Join(tempDir, "cli"), ""); generateErr != nil {
		test.Fatalf("generateDocs: %v", generateErr)
	}

	entries, readDirErr := os.ReadDir(manDir)

	if readDirErr != nil {
		test.Fatalf("read man dir: %v", readDirErr)
	}

	var generated []string

	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".1") {
			generated = append(generated, "man/"+entry.Name())
		}
	}

	raw, readErr := os.ReadFile(filepath.Join("..", "..", ".goreleaser.yaml"))

	if readErr != nil {
		test.Fatalf("read .goreleaser.yaml: %v", readErr)
	}

	var config struct {
		HomebrewCasks []struct {
			Manpages []string `yaml:"manpages"`
		} `yaml:"homebrew_casks"`
	}

	if parseErr := yaml.Unmarshal(raw, &config); parseErr != nil {
		test.Fatalf("parse .goreleaser.yaml: %v", parseErr)
	}

	if len(config.HomebrewCasks) != 1 {
		test.Fatalf(".goreleaser.yaml has %d homebrew_casks entries, want 1", len(config.HomebrewCasks))
	}

	listed := config.HomebrewCasks[0].Manpages

	for _, page := range generated {
		if !slices.Contains(listed, page) {
			test.Errorf("%s is generated but missing from homebrew_casks manpages in .goreleaser.yaml", page)
		}
	}

	for _, page := range listed {
		if !slices.Contains(generated, page) {
			test.Errorf("%s is listed in homebrew_casks manpages in .goreleaser.yaml but docgen no longer writes it", page)
		}
	}
}
