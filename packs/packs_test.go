package packs_test

import (
	"os"
	"slices"
	"testing"

	"github.com/germanamz/tusk/packs"
)

func TestNames_ListsTheBuiltinPacks(test *testing.T) {
	if got := packs.Names(); !slices.Equal(got, []string{"kanban", "tags", "vault"}) {
		test.Errorf("Names() = %q, want [kanban tags vault]", got)
	}
}

func TestRead_ReturnsThePackFile(test *testing.T) {
	for _, name := range packs.Names() {
		body, found := packs.Read(name)

		if !found {
			test.Fatalf("Read(%q) found nothing", name)
		}

		onDisk, readErr := os.ReadFile(name + ".toml")

		if readErr != nil {
			test.Fatalf("read %s.toml: %v", name, readErr)
		}

		if string(body) != string(onDisk) {
			test.Errorf("Read(%q) differs from %s.toml", name, name)
		}
	}
}

func TestRead_UnknownName(test *testing.T) {
	// dev.toml is tusk's own dogfooding pack, installed by URL, not a built-in.
	for _, name := range []string{"dev", "not-a-pack", "vault.toml"} {
		if _, found := packs.Read(name); found {
			test.Errorf("Read(%q) found a pack, want none", name)
		}
	}
}
