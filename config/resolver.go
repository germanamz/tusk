package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// ResolveConfigFile picks the active config file path.
//
// Precedence:
//  1. explicitFile — if set, must exist; missing file is a hard error.
//  2. walk-up from startDir looking for tusk.toml (when startDir != "" and
//     explicitFile is empty).
//  3. globalDir/config.toml — returned when it exists.
//  4. "" — "defaults only".
func ResolveConfigFile(startDir, explicitFile, globalDir string) (string, error) {
	if explicitFile != "" {
		if _, err := os.Stat(explicitFile); err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("config file not found: %s", explicitFile)
			}
			return "", fmt.Errorf("stat %s: %w", explicitFile, err)
		}
		return explicitFile, nil
	}

	if hit := walkUpForLocal(startDir); hit != "" {
		return hit, nil
	}

	if globalDir == "" {
		return "", nil
	}
	path := filepath.Join(globalDir, "config.toml")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	return "", nil
}

// walkUpForLocal walks up from startDir looking for a tusk.toml. Returns the
// absolute path to the first hit, or "" when startDir is empty or no ancestor
// contains one. Symlinks are not followed — plain os.Stat is used.
func walkUpForLocal(startDir string) string {
	if startDir == "" {
		return ""
	}
	dir := startDir
	for {
		candidate := filepath.Join(dir, "tusk.toml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
