package manifest

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// validCardinalities lists the legal Cardinality values for runtime validation.
var validCardinalities = map[Cardinality]struct{}{
	CardinalityOneToOne:   {},
	CardinalityOneToMany:  {},
	CardinalityManyToOne:  {},
	CardinalityManyToMany: {},
}

// Load reads and decodes a tusk.toml at manifestPath, validating its shape.
func Load(manifestPath string) (*Manifest, error) {
	body, readErr := os.ReadFile(manifestPath)

	if readErr != nil {
		return nil, fmt.Errorf("manifest: read %s: %w", manifestPath, readErr)
	}

	loaded := &Manifest{}

	if _, decodeErr := toml.Decode(string(body), loaded); decodeErr != nil {
		return nil, fmt.Errorf("manifest: decode %s: %w", manifestPath, decodeErr)
	}

	if validateErr := validate(loaded); validateErr != nil {
		return nil, validateErr
	}

	return loaded, nil
}

// validate walks the manifest and surfaces structural problems before they
// reach the engine.
func validate(loaded *Manifest) error {
	for name, edgeType := range loaded.EdgeTypes {
		if _, valid := validCardinalities[edgeType.Cardinality]; !valid {
			return fmt.Errorf("manifest: edge-type %q: invalid cardinality %q (want one-to-one|one-to-many|many-to-one|many-to-many)", name, edgeType.Cardinality)
		}

		if len(edgeType.From) == 0 {
			return fmt.Errorf("manifest: edge-type %q: from list must be non-empty", name)
		}

		if len(edgeType.To) == 0 {
			return fmt.Errorf("manifest: edge-type %q: to list must be non-empty", name)
		}
	}

	return nil
}
