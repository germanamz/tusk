package typepacks_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/typepacks"
	"github.com/germanamz/tusk/packs"
)

func TestLoadPack_BuiltinNameReadsTheCompiledInCopy(test *testing.T) {
	for _, name := range packs.Names() {
		body, loadErr := typepacks.LoadPack(context.Background(), name)

		if loadErr != nil {
			test.Fatalf("LoadPack(%q): %v", name, loadErr)
		}

		want, _ := packs.Read(name)

		if string(body) != string(want) {
			test.Errorf("LoadPack(%q) did not return the compiled-in pack", name)
		}

		if _, validateErr := typepacks.Validate(body); validateErr != nil {
			test.Errorf("built-in pack %q does not validate: %v", name, validateErr)
		}
	}
}

func TestLoadPack_FileURLIsFetched(test *testing.T) {
	packPath := filepath.Join(test.TempDir(), "pack.toml")

	if writeErr := os.WriteFile(packPath, []byte("[node-types.task]\nproperties = []\n"), 0o644); writeErr != nil {
		test.Fatalf("write pack: %v", writeErr)
	}

	body, loadErr := typepacks.LoadPack(context.Background(), "file://"+packPath)

	if loadErr != nil {
		test.Fatalf("LoadPack(file): %v", loadErr)
	}

	if !strings.Contains(string(body), "[node-types.task]") {
		test.Errorf("body = %q", body)
	}
}

func TestLoadPack_UnknownNameRejects(test *testing.T) {
	_, loadErr := typepacks.LoadPack(context.Background(), "not-a-pack")

	if loadErr == nil {
		test.Fatal("expected error")
	}

	if !strings.Contains(loadErr.Error(), "unknown pack name") {
		test.Errorf("err = %v", loadErr)
	}

	if !strings.Contains(loadErr.Error(), "kanban, tags, vault") {
		test.Errorf("err should list supported names: %v", loadErr)
	}
}
