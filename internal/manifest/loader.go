package manifest

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Load reads and decodes a tusk.toml at manifestPath.
func Load(manifestPath string) (*Manifest, error) {
	body, readErr := os.ReadFile(manifestPath)

	if readErr != nil {
		return nil, fmt.Errorf("manifest: read %s: %w", manifestPath, readErr)
	}

	loaded := &Manifest{}

	if _, decodeErr := toml.Decode(string(body), loaded); decodeErr != nil {
		return nil, fmt.Errorf("manifest: decode %s: %w", manifestPath, decodeErr)
	}

	return loaded, nil
}
