package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

// TestRuntime_buildReloaded_SwapsOnValidManifest tests that buildReloaded
// validates a fresh manifest, builds a new Runtime reusing the open Index,
// and returns the fresh Runtime and a diff without mutating the original.
func TestRuntime_buildReloaded_SwapsOnValidManifest(test *testing.T) {
	root := test.TempDir()

	manifestV1 := []byte(`
[workspace]
name = "test"

[node-types.ticket]
properties = [
    { name = "id", type = "string", required = true },
]
`)
	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), manifestV1, 0o644); writeErr != nil {
		test.Fatalf("write manifest v1: %v", writeErr)
	}

	rt, openErr := Open(root)
	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}
	defer rt.Close()

	if _, ok := rt.Manifest.NodeTypes["ticket"]; !ok {
		test.Fatalf("rt.Manifest.NodeTypes missing ticket")
	}

	manifestV2 := []byte(`
[workspace]
name = "test"

[node-types.decision]
properties = [
    { name = "summary", type = "string", required = true },
]
`)
	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), manifestV2, 0o644); writeErr != nil {
		test.Fatalf("write manifest v2: %v", writeErr)
	}

	fresh, diff, buildErr := rt.buildReloaded()
	if buildErr != nil {
		test.Fatalf("buildReloaded: %v", buildErr)
	}
	if fresh == nil {
		test.Fatalf("buildReloaded returned nil fresh Runtime")
	}
	if diff == nil {
		test.Fatalf("buildReloaded returned nil diff")
	}
	if _, ok := fresh.Manifest.NodeTypes["decision"]; !ok {
		test.Errorf("fresh.Manifest.NodeTypes missing decision")
	}
	if _, ok := fresh.Manifest.NodeTypes["ticket"]; ok {
		test.Errorf("fresh.Manifest.NodeTypes should not have ticket after reload")
	}
	if _, ok := rt.Manifest.NodeTypes["ticket"]; !ok {
		test.Errorf("original rt.Manifest.NodeTypes was mutated")
	}
	if len(diff.NodeTypes.Added) != 1 || diff.NodeTypes.Added[0] != "decision" {
		test.Errorf("diff.NodeTypes.Added = %v, want [decision]", diff.NodeTypes.Added)
	}
	if len(diff.NodeTypes.Removed) != 1 || diff.NodeTypes.Removed[0] != "ticket" {
		test.Errorf("diff.NodeTypes.Removed = %v, want [ticket]", diff.NodeTypes.Removed)
	}
	if fresh.Index != rt.Index {
		test.Errorf("fresh.Index does not reuse original rt.Index")
	}
	if closeErr := fresh.Close(); closeErr != nil {
		test.Errorf("fresh.Close: %v", closeErr)
	}
}

// TestRuntime_buildReloaded_ReturnsErrorOnParseFailure tests that a TOML
// parse error causes buildReloaded to return (nil, nil, error) without mutating.
func TestRuntime_buildReloaded_ReturnsErrorOnParseFailure(test *testing.T) {
	root := test.TempDir()

	manifestValid := []byte(`
[workspace]
name = "test"

[node-types.ticket]
properties = [
    { name = "id", type = "string", required = true },
]
`)
	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), manifestValid, 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	rt, openErr := Open(root)
	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}
	defer rt.Close()

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte("[workspace\nname = \"test\""), 0o644); writeErr != nil {
		test.Fatalf("write malformed manifest: %v", writeErr)
	}

	fresh, diff, buildErr := rt.buildReloaded()
	if buildErr == nil {
		test.Fatalf("buildReloaded expected error, got nil")
	}
	if fresh != nil {
		test.Errorf("buildReloaded returned non-nil fresh on error")
	}
	if diff != nil {
		test.Errorf("buildReloaded returned non-nil diff on error")
	}
	if _, ok := rt.Manifest.NodeTypes["ticket"]; !ok {
		test.Errorf("original rt.Manifest was mutated on buildReloaded error")
	}
}

// TestRuntime_buildReloaded_ReturnsErrorOnBehaviorEngineBuildFailure tests that
// a behavior-engine build error causes buildReloaded to return (nil, nil, error).
func TestRuntime_buildReloaded_ReturnsErrorOnBehaviorEngineBuildFailure(test *testing.T) {
	root := test.TempDir()

	manifestValid := []byte(`
[workspace]
name = "test"

[node-types.ticket]
properties = [
    { name = "id", type = "string", required = true },
]
`)
	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), manifestValid, 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	rt, openErr := Open(root)
	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}
	defer rt.Close()

	manifestWithBadBehavior := []byte(`
[workspace]
name = "test"

[node-types.ticket]
properties = [
    { name = "id", type = "string", required = true },
]

[behaviors."unknown-kind".instance1]
some_field = "value"
`)
	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), manifestWithBadBehavior, 0o644); writeErr != nil {
		test.Fatalf("write bad-behavior manifest: %v", writeErr)
	}

	fresh, diff, buildErr := rt.buildReloaded()
	if buildErr == nil {
		test.Fatalf("buildReloaded expected error for unknown behavior kind, got nil")
	}
	if fresh != nil {
		test.Errorf("buildReloaded returned non-nil fresh on behavior-engine error")
	}
	if diff != nil {
		test.Errorf("buildReloaded returned non-nil diff on behavior-engine error")
	}
	if _, ok := rt.Manifest.NodeTypes["ticket"]; !ok {
		test.Errorf("original rt.Manifest was mutated on buildReloaded error")
	}
}

// TestRuntime_buildReloaded_SurfacesDanglingAliasAsWarningNotError tests that an
// invalid alias is dropped and recorded on the fresh Manifest (AliasErrors) while
// the swap still proceeds (non-blocking validation).
func TestRuntime_buildReloaded_SurfacesDanglingAliasAsWarningNotError(test *testing.T) {
	root := test.TempDir()

	manifestValid := []byte(`
[workspace]
name = "test"

[node-types.ticket]
properties = [
    { name = "id", type = "string", required = true },
]
`)
	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), manifestValid, 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	rt, openErr := Open(root)
	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}
	defer rt.Close()

	manifestWithBadAlias := []byte(`
[workspace]
name = "test"

[node-types.ticket]
properties = [
    { name = "id", type = "string", required = true },
]

[alias.bad_alias]
command = "nonexistent_verb"
`)
	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), manifestWithBadAlias, 0o644); writeErr != nil {
		test.Fatalf("write bad-alias manifest: %v", writeErr)
	}

	introspector := func(verb string) ([]manifest.FlagSpec, bool) {
		return nil, false
	}
	rt.SetAliasIntrospector(introspector)

	fresh, diff, buildErr := rt.buildReloaded()
	if buildErr != nil {
		test.Fatalf("buildReloaded with dangling alias: %v", buildErr)
	}
	if fresh == nil {
		test.Fatalf("buildReloaded returned nil fresh on dangling alias (should proceed)")
	}
	if diff == nil {
		test.Fatalf("buildReloaded returned nil diff on dangling alias")
	}
	if len(fresh.Manifest.AliasErrors) == 0 {
		test.Errorf("fresh.Manifest.AliasErrors is empty; expected a dangling-alias error")
	}
	if _, ok := fresh.Manifest.Aliases["bad_alias"]; ok {
		test.Errorf("fresh.Manifest.Aliases should not contain bad_alias (it was invalid)")
	}
	if len(rt.Manifest.AliasErrors) > 0 {
		test.Errorf("original rt.Manifest was mutated (AliasErrors populated)")
	}
	if closeErr := fresh.Close(); closeErr != nil {
		test.Errorf("fresh.Close: %v", closeErr)
	}
}
