package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// ResolveConfigFile picks the active config file path.
//
// Precedence in this phase:
//  1. explicitFile — if set, must exist; missing file is a hard error.
//  2. globalDir/config.toml — returned when it exists.
//  3. "" — "defaults only", returned when neither above applies.
//
// The startDir parameter is unused in this phase. It exists so that the
// walk-up step added by the Local Config Discovery initiative can slot in
// between (1) and (2) without churning every caller.
func ResolveConfigFile(startDir, explicitFile, globalDir string) (string, error) {
	_ = startDir // reserved for walk-up, see next initiative

	if explicitFile != "" {
		if _, err := os.Stat(explicitFile); err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("config file not found: %s", explicitFile)
			}
			return "", fmt.Errorf("stat %s: %w", explicitFile, err)
		}
		return explicitFile, nil
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
