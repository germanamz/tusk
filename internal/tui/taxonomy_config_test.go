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
func writeSeedConfig(t *testing.T, path string) {
	t.Helper()
	seed := []byte("[storage]\nbackend = \"sqlite\"\npath = \"/tmp/test.db\"\n")
	if err := os.WriteFile(path, seed, 0o644); err != nil {
		t.Fatalf("seeding config: %v", err)
	}
}

func TestSetTaxonomyLevelsInline_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeSeedConfig(t, path)

	if err := setTaxonomyLevelsInline(path, "milestone:story"); err != nil {
		t.Fatalf("setTaxonomyLevelsInline: %v", err)
	}

	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	want := [][]string{{"milestone"}, {"story"}}
	if !reflect.DeepEqual(loaded.Taxonomy.Levels, want) {
		t.Fatalf("levels: got %+v, want %+v", loaded.Taxonomy.Levels, want)
	}
}

func TestSetTaxonomyLevelsInline_MultiPeer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeSeedConfig(t, path)

	if err := setTaxonomyLevelsInline(path, "milestone:story:(task,spike)"); err != nil {
		t.Fatalf("setTaxonomyLevelsInline: %v", err)
	}

	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	want := [][]string{{"milestone"}, {"story"}, {"task", "spike"}}
	if !reflect.DeepEqual(loaded.Taxonomy.Levels, want) {
		t.Fatalf("levels: got %+v, want %+v", loaded.Taxonomy.Levels, want)
	}
}

func TestSetTaxonomyLevelsInline_ClearDeletes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeSeedConfig(t, path)

	// Pre-populate with a taxonomy.
	if err := setTaxonomyLevelsInline(path, "milestone:story"); err != nil {
		t.Fatalf("seed set: %v", err)
	}
	// Clear it.
	if err := setTaxonomyLevelsInline(path, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}

	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(loaded.Taxonomy.Levels) != 0 {
		t.Fatalf("expected empty Taxonomy.Levels after clear, got %+v", loaded.Taxonomy.Levels)
	}
}

func TestSetTaxonomyLevelsInline_MalformedDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeSeedConfig(t, path)

	// Capture pre-state bytes so we can confirm no modification.
	pre, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading pre-state: %v", err)
	}

	err = setTaxonomyLevelsInline(path, "a::b")
	if err == nil {
		t.Fatal("expected error for malformed inline taxonomy")
	}

	post, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading post-state: %v", err)
	}
	if !reflect.DeepEqual(pre, post) {
		t.Fatalf("file modified on error:\npre:  %s\npost: %s", pre, post)
	}
}
