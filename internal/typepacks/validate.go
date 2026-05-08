package typepacks

import (
	"fmt"

	"github.com/BurntSushi/toml"

	"github.com/germanamz/tusk/internal/manifest"
)

// allowedTopLevelKeys is the closed set of section names a pack file
// may declare. Any other top-level key rejects.
var allowedTopLevelKeys = map[string]struct{}{
	"node-types": {},
	"edge-types": {},
	"behaviors":  {},
}

// Validate decodes packBytes, asserts the top-level shape, and runs the
// content through the same manifest validator the engine uses. Returns
// the parsed manifest fragment on success.
func Validate(packBytes []byte) (*manifest.Manifest, error) {
	// (a) Raw decode to detect disallowed top-level keys.
	var raw map[string]toml.Primitive

	rawMeta, rawErr := toml.Decode(string(packBytes), &raw)

	if rawErr != nil {
		return nil, fmt.Errorf("pack add: invalid TOML: decode: %w", rawErr)
	}

	seen := make(map[string]struct{})

	for _, key := range rawMeta.Keys() {
		topLevel := key[0]

		if _, already := seen[topLevel]; already {
			continue
		}

		seen[topLevel] = struct{}{}

		if _, ok := allowedTopLevelKeys[topLevel]; !ok {
			return nil, fmt.Errorf("pack add: pack contains disallowed top-level section %q (packs may only contain [node-types], [edge-types], [behaviors])", topLevel)
		}
	}

	// (b) Typed decode + manifest schema validation.
	loaded := &manifest.Manifest{}

	typedMeta, typedErr := toml.Decode(string(packBytes), loaded)

	if typedErr != nil {
		return nil, fmt.Errorf("pack add: decode pack: %w", typedErr)
	}

	loaded.Meta = &typedMeta

	if validateErr := manifest.Validate(loaded); validateErr != nil {
		return nil, fmt.Errorf("pack add: %w", validateErr)
	}

	return loaded, nil
}
