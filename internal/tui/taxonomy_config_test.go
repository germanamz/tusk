package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/germanamz/tusk/config"
)

// writeSeedConfig drops a minimal valid TOML config at path so
// setTaxonomyLevelsInline's LoadFile call succeeds.
func writeSeedConfig(test *testing.T, path string) {
	test.Helper()
	seed := []byte("[storage]\nbackend = \"sqlite\"\npath = \"/tmp/test.db\"\n")
	if err := os.WriteFile(path, seed, 0o644); err != nil {
		test.Fatalf("seeding config: %v", err)
	}
}

func TestSetTaxonomyLevelsInline_RoundTrip(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeSeedConfig(test, path)

	if err := setTaxonomyLevelsInline(path, "milestone:story"); err != nil {
		test.Fatalf("setTaxonomyLevelsInline: %v", err)
	}

	loaded, loadErr := config.LoadFile(path)

	if loadErr != nil {
		test.Fatalf("LoadFile: %v", loadErr)
	}

	want := [][]string{{"milestone"}, {"story"}}
	if !reflect.DeepEqual(loaded.Taxonomy.Levels, want) {
		test.Fatalf("levels: got %+v, want %+v", loaded.Taxonomy.Levels, want)
	}
}

func TestSetTaxonomyLevelsInline_MultiPeer(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeSeedConfig(test, path)

	if err := setTaxonomyLevelsInline(path, "milestone:story:(task,spike)"); err != nil {
		test.Fatalf("setTaxonomyLevelsInline: %v", err)
	}

	loaded, loadErr := config.LoadFile(path)

	if loadErr != nil {
		test.Fatalf("LoadFile: %v", loadErr)
	}

	want := [][]string{{"milestone"}, {"story"}, {"task", "spike"}}
	if !reflect.DeepEqual(loaded.Taxonomy.Levels, want) {
		test.Fatalf("levels: got %+v, want %+v", loaded.Taxonomy.Levels, want)
	}
}

func TestSetTaxonomyLevelsInline_ClearDeletes(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeSeedConfig(test, path)

	// Pre-populate with a taxonomy.
	if err := setTaxonomyLevelsInline(path, "milestone:story"); err != nil {
		test.Fatalf("seed set: %v", err)
	}
	// Clear it.
	if err := setTaxonomyLevelsInline(path, ""); err != nil {
		test.Fatalf("clear: %v", err)
	}

	loaded, loadErr := config.LoadFile(path)

	if loadErr != nil {
		test.Fatalf("LoadFile: %v", loadErr)
	}

	if len(loaded.Taxonomy.Levels) != 0 {
		test.Fatalf("expected empty Taxonomy.Levels after clear, got %+v", loaded.Taxonomy.Levels)
	}
}

func TestSetTaxonomyLevelsInline_MalformedDoesNotWrite(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeSeedConfig(test, path)

	// Capture pre-state bytes so we can confirm no modification.
	pre, preErr := os.ReadFile(path)

	if preErr != nil {
		test.Fatalf("reading pre-state: %v", preErr)
	}

	setErr := setTaxonomyLevelsInline(path, "a::b")
	if setErr == nil {
		test.Fatal("expected error for malformed inline taxonomy")
	}

	post, postErr := os.ReadFile(path)

	if postErr != nil {
		test.Fatalf("reading post-state: %v", postErr)
	}

	if !reflect.DeepEqual(pre, post) {
		test.Fatalf("file modified on error:\npre:  %s\npost: %s", pre, post)
	}
}
