package typepacks_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/typepacks"
)

func TestAddPack_HappyFileURL(test *testing.T) {
	dir := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(dir, "tusk.toml"), []byte(`
[workspace]
name = "test"
`), 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}

	packPath := filepath.Join(dir, "pack.toml")

	if writeErr := os.WriteFile(packPath, []byte(`
[node-types.task]
properties = [{ name = "summary", type = "string" }]
`), 0o644); writeErr != nil {
		test.Fatalf("write pack.toml: %v", writeErr)
	}

	if addErr := typepacks.AddPack(context.Background(), "file://"+packPath, false, dir); addErr != nil {
		test.Fatalf("AddPack: %v", addErr)
	}

	body, _ := os.ReadFile(filepath.Join(dir, "tusk.toml"))

	if !strings.Contains(string(body), "[node-types.task]") {
		test.Errorf("tusk.toml = %q", body)
	}

	if !strings.Contains(string(body), "Added by `tusk pack add") {
		test.Errorf("missing pack header comment: %q", body)
	}
}

func TestAddPack_CollisionRejectsWithoutForce(test *testing.T) {
	dir := test.TempDir()

	os.WriteFile(filepath.Join(dir, "tusk.toml"), []byte(`
[workspace]
name = "test"

[node-types.task]
properties = [{ name = "summary", type = "string" }]
`), 0o644)

	packPath := filepath.Join(dir, "pack.toml")
	os.WriteFile(packPath, []byte(`
[node-types.task]
properties = [{ name = "summary", type = "string" }, { name = "priority", type = "int" }]
`), 0o644)

	addErr := typepacks.AddPack(context.Background(), "file://"+packPath, false, dir)

	if addErr == nil {
		test.Fatal("expected collision error")
	}

	if !strings.Contains(addErr.Error(), "node-types.task") {
		test.Errorf("err = %v", addErr)
	}

	// User manifest must be byte-identical to before.
	body, _ := os.ReadFile(filepath.Join(dir, "tusk.toml"))

	if strings.Contains(string(body), "priority") {
		test.Errorf("user manifest unexpectedly mutated: %q", body)
	}
}

func TestAddPack_CollisionWithForceOverwrites(test *testing.T) {
	dir := test.TempDir()

	os.WriteFile(filepath.Join(dir, "tusk.toml"), []byte(`
[workspace]
name = "test"

[node-types.task]
properties = [{ name = "summary", type = "string" }]

[node-types.note]
properties = [{ name = "summary", type = "string" }]
`), 0o644)

	packPath := filepath.Join(dir, "pack.toml")
	os.WriteFile(packPath, []byte(`
[node-types.task]
properties = [{ name = "summary", type = "string" }, { name = "priority", type = "int" }]
`), 0o644)

	if addErr := typepacks.AddPack(context.Background(), "file://"+packPath, true, dir); addErr != nil {
		test.Fatalf("AddPack --force: %v", addErr)
	}

	body, _ := os.ReadFile(filepath.Join(dir, "tusk.toml"))

	// task section is replaced (priority appears now).
	if !strings.Contains(string(body), "priority") {
		test.Errorf("expected pack content with priority, got %q", body)
	}

	// note section is preserved.
	if !strings.Contains(string(body), "[node-types.note]") {
		test.Errorf("--force should not touch unrelated sections: %q", body)
	}
}

func TestAddPack_RejectsBadPack(test *testing.T) {
	dir := test.TempDir()

	os.WriteFile(filepath.Join(dir, "tusk.toml"), []byte("[workspace]\nname = \"test\"\n"), 0o644)

	packPath := filepath.Join(dir, "pack.toml")
	os.WriteFile(packPath, []byte("[workspace]\nname = \"evil\"\n"), 0o644)

	addErr := typepacks.AddPack(context.Background(), "file://"+packPath, false, dir)

	if addErr == nil || !strings.Contains(addErr.Error(), "workspace") {
		test.Errorf("expected disallowed-section error, got %v", addErr)
	}

	// Manifest unchanged.
	body, _ := os.ReadFile(filepath.Join(dir, "tusk.toml"))

	if strings.Contains(string(body), "evil") {
		test.Errorf("manifest unexpectedly mutated: %q", body)
	}
}

const packWithRule = `
[node-types.task]
properties = [{ name = "owner", type = "string" }]

[rule.task-has-an-owner]
description = "every task names its owner"
filter      = "type=task AND owner!=*"
`

func TestAddPack_AddsRule(test *testing.T) {
	dir := test.TempDir()
	manifestPath := filepath.Join(dir, "tusk.toml")

	if writeErr := os.WriteFile(manifestPath, []byte("[workspace]\nname = \"test\"\n"), 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}

	packPath := filepath.Join(dir, "pack.toml")

	if writeErr := os.WriteFile(packPath, []byte(packWithRule), 0o644); writeErr != nil {
		test.Fatalf("write pack.toml: %v", writeErr)
	}

	if addErr := typepacks.AddPack(context.Background(), "file://"+packPath, false, dir); addErr != nil {
		test.Fatalf("AddPack: %v", addErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load after AddPack: %v", loadErr)
	}

	rule, declared := loaded.Rules["task-has-an-owner"]

	if !declared {
		test.Fatalf("Rules = %v, want task-has-an-owner", loaded.Rules)
	}

	if rule.Filter != "type=task AND owner!=*" {
		test.Errorf("filter = %q", rule.Filter)
	}
}

func TestAddPack_RuleCollisionRejectsWithoutForce(test *testing.T) {
	dir := test.TempDir()
	manifestPath := filepath.Join(dir, "tusk.toml")

	before := []byte(`
[workspace]
name = "test"

[rule.task-has-an-owner]
filter = "type=note"
`)

	if writeErr := os.WriteFile(manifestPath, before, 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}

	packPath := filepath.Join(dir, "pack.toml")

	if writeErr := os.WriteFile(packPath, []byte(packWithRule), 0o644); writeErr != nil {
		test.Fatalf("write pack.toml: %v", writeErr)
	}

	addErr := typepacks.AddPack(context.Background(), "file://"+packPath, false, dir)

	if addErr == nil || !strings.Contains(addErr.Error(), "rule.task-has-an-owner") {
		test.Fatalf("expected a collision naming rule.task-has-an-owner, got %v", addErr)
	}

	after, _ := os.ReadFile(manifestPath)

	if string(after) != string(before) {
		test.Errorf("tusk.toml changed on a refused add:\n%s", after)
	}
}

func TestAddPack_RuleCollisionWithForceOverwrites(test *testing.T) {
	dir := test.TempDir()
	manifestPath := filepath.Join(dir, "tusk.toml")

	if writeErr := os.WriteFile(manifestPath, []byte(`
[workspace]
name = "test"

[rule.task-has-an-owner]
filter = "type=note"

[rule.kept]
filter = "type=note"
`), 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}

	packPath := filepath.Join(dir, "pack.toml")

	if writeErr := os.WriteFile(packPath, []byte(packWithRule), 0o644); writeErr != nil {
		test.Fatalf("write pack.toml: %v", writeErr)
	}

	if addErr := typepacks.AddPack(context.Background(), "file://"+packPath, true, dir); addErr != nil {
		test.Fatalf("AddPack --force: %v", addErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load after AddPack --force: %v", loadErr)
	}

	if got := loaded.Rules["task-has-an-owner"].Filter; got != "type=task AND owner!=*" {
		test.Errorf("forced rule filter = %q, want the pack's", got)
	}

	if _, kept := loaded.Rules["kept"]; !kept {
		test.Errorf("--force removed an unrelated rule: %v", loaded.Rules)
	}
}
