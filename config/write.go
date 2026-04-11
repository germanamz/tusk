package config

import (
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

// ConfigFilePath resolves the config file path using the same logic as Load():
// custom path option > TUSK_CONFIG_DIR env > ~/.config/tusk/config.toml.
// Returns the path regardless of whether the file exists.
func ConfigFilePath(opts ...Option) (string, error) {
	var lo loadOptions
	for _, opt := range opts {
		opt(&lo)
	}

	var searchPath string
	switch {
	case lo.searchPath != "":
		searchPath = lo.searchPath
	case os.Getenv("TUSK_CONFIG_DIR") != "":
		searchPath = os.Getenv("TUSK_CONFIG_DIR")
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home directory: %w", err)
		}
		searchPath = filepath.Join(home, ".config", "tusk")
	}

	return filepath.Join(searchPath, "config.toml"), nil
}

// LoadFile parses a single TOML config file into a Config struct.
// Unlike Load(), this uses go-toml directly — no Viper, no env merging, no defaults.
// Used by config set (load-modify-write) and config validate (file-only validation).
func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	return &cfg, nil
}

// WriteConfig marshals a Config struct to TOML and writes it to path atomically.
// Writes to a temporary file first, then renames to avoid partial writes.
func WriteConfig(cfg *Config, path string) error {
	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "tusk-config-*.toml")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming temp file: %w", err)
	}

	return nil
}
