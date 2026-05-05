package manifest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

func TestLoad_ParsesMinimalManifest(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "my-brain"
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if loaded.Workspace.Name != "my-brain" {
		test.Errorf("Name = %q, want %q", loaded.Workspace.Name, "my-brain")
	}
}

func TestLoad_ParsesIgnorePatterns(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "my-brain"
ignore = ["build/", "*.tmp"]
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if len(loaded.Workspace.Ignore) != 2 {
		test.Fatalf("Ignore len = %d, want 2", len(loaded.Workspace.Ignore))
	}

	if loaded.Workspace.Ignore[0] != "build/" {
		test.Errorf("Ignore[0] = %q", loaded.Workspace.Ignore[0])
	}
}

func TestLoad_ReturnsErrorOnMalformedTOML(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	if writeErr := os.WriteFile(manifestPath, []byte("not = valid = toml"), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	_, loadErr := manifest.Load(manifestPath)

	if loadErr == nil {
		test.Fatalf("expected error, got nil")
	}
}

func TestLoad_ParsesEdgeTypes(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "my-brain"

[edge-types.parent]
description = "Hierarchical parent"
from = ["ticket", "project"]
to = ["ticket", "project"]
cardinality = "many-to-one"
ordered = false

[edge-types.blocks]
description = "Blocks another"
from = ["ticket"]
to = ["ticket"]
cardinality = "many-to-many"
acyclic = true

[edge-types.references]
description = "Implicit references"
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if len(loaded.EdgeTypes) != 3 {
		test.Fatalf("EdgeTypes len = %d, want 3", len(loaded.EdgeTypes))
	}

	parentType, hasParent := loaded.EdgeTypes["parent"]

	if !hasParent {
		test.Fatalf("parent edge type missing")
	}

	if parentType.Cardinality != manifest.CardinalityManyToOne {
		test.Errorf("parent cardinality = %q", parentType.Cardinality)
	}

	if !parentType.AllowsSource("ticket") {
		test.Errorf("parent should allow ticket source")
	}

	blocksType := loaded.EdgeTypes["blocks"]

	if !blocksType.Acyclic {
		test.Errorf("blocks should be acyclic")
	}

	referencesType := loaded.EdgeTypes["references"]

	if !referencesType.AllowsSource("anything") {
		test.Errorf("references should allow any source via wildcard")
	}
}

func TestLoad_RejectsInvalidCardinality(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "x"

[edge-types.bad]
from = ["ticket"]
to = ["ticket"]
cardinality = "bogus"
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	_, loadErr := manifest.Load(manifestPath)

	if loadErr == nil {
		test.Fatalf("expected error for invalid cardinality")
	}
}

func TestLoad_RejectsEmptyFromOrTo(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "x"

[edge-types.bad]
from = []
to = ["ticket"]
cardinality = "many-to-many"
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	_, loadErr := manifest.Load(manifestPath)

	if loadErr == nil {
		test.Fatalf("expected error for empty from list")
	}
}
