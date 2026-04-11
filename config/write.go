package config

import (
	"fmt"
	"os"
	"path/filepath"
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
