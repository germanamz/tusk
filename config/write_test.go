package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigFilePath_WithSearchPath(t *testing.T) {
	dir := t.TempDir()
	got, err := ConfigFilePath(WithSearchPath(dir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(dir, "config.toml")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestConfigFilePath_WithEnvVar(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TUSK_CONFIG_DIR", dir)
	got, err := ConfigFilePath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(dir, "config.toml")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestConfigFilePath_Default(t *testing.T) {
	// Clear env to force default path.
	t.Setenv("TUSK_CONFIG_DIR", "")
	got, err := ConfigFilePath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "tusk", "config.toml")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
