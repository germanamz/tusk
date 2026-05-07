package typepacks_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/typepacks"
)

func TestResolve_BuiltinNameMapsToURL(test *testing.T) {
	url, resolveErr := typepacks.Resolve("kanban")

	if resolveErr != nil {
		test.Fatalf("Resolve(kanban): %v", resolveErr)
	}

	if !strings.HasPrefix(url, "https://raw.githubusercontent.com/germanamz/tusk/") {
		test.Errorf("kanban URL = %q", url)
	}
}

func TestResolve_RawURLPassesThrough(test *testing.T) {
	url, resolveErr := typepacks.Resolve("https://example.com/pack.toml")

	if resolveErr != nil {
		test.Fatalf("Resolve(https): %v", resolveErr)
	}

	if url != "https://example.com/pack.toml" {
		test.Errorf("URL = %q", url)
	}
}

func TestResolve_FileURLPassesThrough(test *testing.T) {
	url, resolveErr := typepacks.Resolve("file:///tmp/pack.toml")

	if resolveErr != nil {
		test.Fatalf("Resolve(file): %v", resolveErr)
	}

	if url != "file:///tmp/pack.toml" {
		test.Errorf("URL = %q", url)
	}
}

func TestResolve_UnknownNameRejects(test *testing.T) {
	_, resolveErr := typepacks.Resolve("not-a-pack")

	if resolveErr == nil {
		test.Fatal("expected error")
	}

	if !strings.Contains(resolveErr.Error(), "unknown pack name") {
		test.Errorf("err = %v", resolveErr)
	}

	if !strings.Contains(resolveErr.Error(), "kanban") {
		test.Errorf("err should list supported names: %v", resolveErr)
	}
}
