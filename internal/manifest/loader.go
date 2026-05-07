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

// supportedPropertyTypes is the set of valid property type strings.
var supportedPropertyTypes = map[string]struct{}{
	"string":   {},
	"int":      {},
	"float":    {},
	"bool":     {},
	"date":     {},
	"datetime": {},
	"enum":     {},
	"markdown": {},
	"list-of":  {},
}

// reservedPropertyNames are the names that cannot appear in any property declaration.
var reservedPropertyNames = map[string]struct{}{
	"type":  {},
	"title": {},
}

// Load reads and decodes a tusk.toml at manifestPath, validating its shape.
func Load(manifestPath string) (*Manifest, error) {
	body, readErr := os.ReadFile(manifestPath)

	if readErr != nil {
		return nil, fmt.Errorf("manifest: read %s: %w", manifestPath, readErr)
	}

	loaded := &Manifest{}

	meta, decodeErr := toml.Decode(string(body), loaded)

	if decodeErr != nil {
		return nil, fmt.Errorf("manifest: decode %s: %w", manifestPath, decodeErr)
	}

	loaded.Meta = &meta

	if validateErr := Validate(loaded); validateErr != nil {
		return nil, validateErr
	}

	return loaded, nil
}

// Validate is exported so tests can validate hand-constructed manifests.
// Production code should use Load.
func Validate(loaded *Manifest) error {
	if validateErr := validate(loaded); validateErr != nil {
		return validateErr
	}

	if validateErr := validateNodeTypes(loaded); validateErr != nil {
		return validateErr
	}

	return validateBehaviors(loaded)
}

// validateNodeTypes enforces the structural rules for the [node-types] section.
func validateNodeTypes(loaded *Manifest) error {
	for typeName, nodeType := range loaded.NodeTypes {
		if typeName == "" {
			return fmt.Errorf("manifest: node-types: empty type name")
		}

		seen := make(map[string]struct{}, len(nodeType.Properties))

		for _, prop := range nodeType.Properties {
			if prop.Name == "" {
				return fmt.Errorf("manifest: node-types.%s: empty property name", typeName)
			}

			if _, reserved := reservedPropertyNames[prop.Name]; reserved {
				return fmt.Errorf("manifest: node-types.%s: property name %q is reserved", typeName, prop.Name)
			}

			if _, exists := seen[prop.Name]; exists {
				return fmt.Errorf("manifest: node-types.%s: duplicate property name %q", typeName, prop.Name)
			}

			seen[prop.Name] = struct{}{}

			if _, valid := supportedPropertyTypes[prop.Type]; !valid {
				return fmt.Errorf("manifest: node-types.%s.%s: unknown property type %q", typeName, prop.Name, prop.Type)
			}

			if typeErr := validatePropertyTypeConstraints(typeName, prop); typeErr != nil {
				return typeErr
			}
		}
	}

	return nil
}

// validatePropertyTypeConstraints enforces per-type rules for enum, list-of, and
// misplaced constraint keys.
func validatePropertyTypeConstraints(typeName string, prop PropertyDecl) error {
	switch prop.Type {
	case "enum":
		if enumErr := validateEnumValues(typeName, prop.Name, prop.Values); enumErr != nil {
			return enumErr
		}

	case "list-of":
		if prop.ItemType == "" {
			return fmt.Errorf("manifest: node-types.%s.%s: list-of requires item-type", typeName, prop.Name)
		}

		if prop.ItemType == "list-of" {
			return fmt.Errorf("manifest: node-types.%s.%s: cannot nest list-of inside list-of", typeName, prop.Name)
		}

		if prop.ItemType == "enum" {
			if enumErr := validateEnumValues(typeName, prop.Name, prop.Values); enumErr != nil {
				return enumErr
			}
		}

	default:
		// Misplaced constraint: values on a non-enum type.
		if len(prop.Values) > 0 {
			return fmt.Errorf("manifest: node-types.%s.%s: values is only valid for enum, not %q", typeName, prop.Name, prop.Type)
		}

		// Misplaced constraint: item-type on a non-list-of type.
		if prop.ItemType != "" {
			return fmt.Errorf("manifest: node-types.%s.%s: item-type is only valid for list-of, not %q", typeName, prop.Name, prop.Type)
		}
	}

	return nil
}

// validateEnumValues enforces the rules that apply to both enum and list-of(enum)
// values declarations.
func validateEnumValues(typeName, propName string, values []string) error {
	if len(values) == 0 {
		return fmt.Errorf("manifest: node-types.%s.%s: enum requires at least one value in values", typeName, propName)
	}

	seen := make(map[string]struct{}, len(values))

	for _, val := range values {
		if val == "" {
			return fmt.Errorf("manifest: node-types.%s.%s: enum values must not contain empty string", typeName, propName)
		}

		if _, exists := seen[val]; exists {
			return fmt.Errorf("manifest: node-types.%s.%s: duplicate enum value %q", typeName, propName, val)
		}

		seen[val] = struct{}{}
	}

	return nil
}

// validateBehaviors enforces the structural rules that apply to every
// behavior pack regardless of kind: non-empty kind name, non-empty
// instance name. Kind-specific schema lives in each pack's NewInstance.
func validateBehaviors(loaded *Manifest) error {
	for kindName, perInstance := range loaded.Behaviors {
		if kindName == "" {
			return fmt.Errorf("manifest: behaviors: empty kind name")
		}

		for instanceName := range perInstance {
			if instanceName == "" {
				return fmt.Errorf("manifest: behaviors.%s: empty instance name", kindName)
			}
		}
	}

	return nil
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

	if loaded.Embeddings.Provider != "" {
		if loaded.Embeddings.Provider != "ollama" {
			return fmt.Errorf("manifest: embeddings.provider = %q is not supported (Plan 5 supports \"ollama\" only; OpenAI/Voyage/Anthropic land in Plan 5.x)", loaded.Embeddings.Provider)
		}

		if loaded.Embeddings.Dim <= 0 {
			return fmt.Errorf("manifest: embeddings.dim must be > 0")
		}

		if loaded.Embeddings.Model == "" {
			return fmt.Errorf("manifest: embeddings.model must be set when embeddings.provider is configured")
		}
	}

	return nil
}
