package node

import (
	"fmt"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/manifest"
)

// PropertyErrorKind classifies the type of property validation failure.
type PropertyErrorKind int

const (
	// ErrTypeMismatch means the value's Go type does not match the declared type.
	ErrTypeMismatch PropertyErrorKind = iota
	// ErrRequiredMissing means a required property is absent from the node.
	ErrRequiredMissing
	// ErrEnumViolation means the value is not in the declared enum values list.
	ErrEnumViolation
	// ErrCannotUnsetRequired means a Modify attempted to remove a required property.
	ErrCannotUnsetRequired
)

// PropertyError carries one validation failure.
type PropertyError struct {
	Kind     PropertyErrorKind
	Property string
	Type     string // declared type rendering, e.g. "int" or "list-of(string)"
	Value    any    // observed value (for type-mismatch / enum)
	Reason   string // human-readable detail
}

// PropertyDrift carries one informational undeclared-property observation.
type PropertyDrift struct {
	Property string
	Reason   string
}

// PropertyValidationResult is the output of ValidateProperties.
// HardErrors means the caller must reject the write.
// Drift is informational — the caller proceeds with warnings and persists drift rows.
type PropertyValidationResult struct {
	HardErrors []PropertyError
	Drift      []PropertyDrift
}

// ValidateProperties validates the properties of parsed against the declared
// node types in decls. It is pure — no I/O, no graph reads.
//
// Algorithm:
//  1. If decls has no declaration for parsed.Type, return empty (untyped pass-through).
//  2. For each required PropertyDecl absent from parsed.Properties, append ErrRequiredMissing.
//  3. For each entry in parsed.Properties, validate against the declared type.
//  4. Return the accumulated result.
func ValidateProperties(parsed *Node, decls map[string]manifest.NodeType) PropertyValidationResult {
	nodeType, declared := decls[parsed.Type]
	if !declared {
		return PropertyValidationResult{}
	}

	var result PropertyValidationResult

	// Step 2: required-property check in declaration order.
	for _, decl := range nodeType.Properties {
		if !decl.Required {
			continue
		}

		if _, present := parsed.Properties[decl.Name]; !present {
			result.HardErrors = append(result.HardErrors, PropertyError{
				Kind:     ErrRequiredMissing,
				Property: decl.Name,
				Type:     renderDeclType(decl),
				Reason:   fmt.Sprintf("property %q is required (declared in [node-types.%s])", decl.Name, parsed.Type),
			})
		}
	}

	// Build a lookup map from property name to decl for O(1) access.
	declByName := make(map[string]manifest.PropertyDecl, len(nodeType.Properties))
	for _, decl := range nodeType.Properties {
		declByName[decl.Name] = decl
	}

	// Step 3: per-property loop.
	for name, value := range parsed.Properties {
		decl, found := declByName[name]

		if !found {
			result.Drift = append(result.Drift, PropertyDrift{
				Property: name,
				Reason:   fmt.Sprintf("not declared on type %q", parsed.Type),
			})

			continue
		}

		errs := validateValue(name, value, decl, parsed.Type)
		result.HardErrors = append(result.HardErrors, errs...)
	}

	return result
}

// WhichRequiredWereUnset returns the names of properties that were required,
// present on before, and absent from after. Used by Service.Modify to detect
// ErrCannotUnsetRequired violations.
func WhichRequiredWereUnset(before, after *Node, decls map[string]manifest.NodeType) []string {
	nodeType, declared := decls[after.Type]
	if !declared {
		return nil
	}

	var unset []string

	for _, decl := range nodeType.Properties {
		if !decl.Required {
			continue
		}

		_, hadBefore := before.Properties[decl.Name]
		_, hasAfter := after.Properties[decl.Name]

		if hadBefore && !hasAfter {
			unset = append(unset, decl.Name)
		}
	}

	return unset
}

// renderDeclType returns the human-readable type rendering for a PropertyDecl.
// Scalars return their bare type name; enum returns "enum(v1,v2,...)";
// list-of returns "list-of(<inner>)".
func renderDeclType(decl manifest.PropertyDecl) string {
	switch decl.Type {
	case "enum":
		return fmt.Sprintf("enum(%s)", strings.Join(decl.Values, ","))
	case "list-of":
		inner := decl.ItemType
		if decl.ItemType == "enum" {
			inner = fmt.Sprintf("enum(%s)", strings.Join(decl.Values, "|"))
		}

		return fmt.Sprintf("list-of(%s)", inner)
	default:
		return decl.Type
	}
}

// validateValue validates a single property value against its declaration.
// Returns zero or more PropertyErrors.
func validateValue(name string, value any, decl manifest.PropertyDecl, nodeType string) []PropertyError {
	switch decl.Type {
	case "string", "markdown":
		return validateString(name, value, decl)
	case "int":
		return validateInt(name, value, decl)
	case "float":
		return validateFloat(name, value, decl)
	case "bool":
		return validateBool(name, value, decl)
	case "date":
		return validateDate(name, value, decl)
	case "datetime":
		return validateDatetime(name, value, decl)
	case "enum":
		return validateEnum(name, value, decl)
	case "list-of":
		return validateListOf(name, value, decl)
	default:
		return nil
	}
}

func validateString(name string, value any, decl manifest.PropertyDecl) []PropertyError {
	if _, ok := value.(string); !ok {
		return []PropertyError{{
			Kind:     ErrTypeMismatch,
			Property: name,
			Type:     renderDeclType(decl),
			Value:    value,
			Reason:   fmt.Sprintf("expected type %q but value is not a string", decl.Type),
		}}
	}

	return nil
}

func validateInt(name string, value any, decl manifest.PropertyDecl) []PropertyError {
	switch value.(type) {
	case int, int64:
		return nil
	default:
		return []PropertyError{{
			Kind:     ErrTypeMismatch,
			Property: name,
			Type:     renderDeclType(decl),
			Value:    value,
			Reason:   fmt.Sprintf("expected type %q but value %v is not an integer", decl.Type, value),
		}}
	}
}

func validateFloat(name string, value any, decl manifest.PropertyDecl) []PropertyError {
	switch value.(type) {
	case float64, int, int64:
		return nil
	default:
		return []PropertyError{{
			Kind:     ErrTypeMismatch,
			Property: name,
			Type:     renderDeclType(decl),
			Value:    value,
			Reason:   fmt.Sprintf("expected type %q but value %v is not a number", decl.Type, value),
		}}
	}
}

func validateBool(name string, value any, decl manifest.PropertyDecl) []PropertyError {
	if _, ok := value.(bool); !ok {
		return []PropertyError{{
			Kind:     ErrTypeMismatch,
			Property: name,
			Type:     renderDeclType(decl),
			Value:    value,
			Reason:   fmt.Sprintf("expected type %q but value %v is not a boolean", decl.Type, value),
		}}
	}

	return nil
}

func validateDate(name string, value any, decl manifest.PropertyDecl) []PropertyError {
	str, ok := value.(string)

	if !ok {
		return []PropertyError{{
			Kind:     ErrTypeMismatch,
			Property: name,
			Type:     renderDeclType(decl),
			Value:    value,
			Reason:   fmt.Sprintf("expected type %q but value is not a string", decl.Type),
		}}
	}

	if _, parseErr := time.Parse(time.DateOnly, str); parseErr != nil {
		return []PropertyError{{
			Kind:     ErrTypeMismatch,
			Property: name,
			Type:     renderDeclType(decl),
			Value:    value,
			Reason:   fmt.Sprintf("expected type %q but value %q is not a valid date (YYYY-MM-DD)", decl.Type, str),
		}}
	}

	return nil
}

func validateDatetime(name string, value any, decl manifest.PropertyDecl) []PropertyError {
	str, ok := value.(string)

	if !ok {
		return []PropertyError{{
			Kind:     ErrTypeMismatch,
			Property: name,
			Type:     renderDeclType(decl),
			Value:    value,
			Reason:   fmt.Sprintf("expected type %q but value is not a string", decl.Type),
		}}
	}

	if _, parseErr := time.Parse(time.RFC3339, str); parseErr != nil {
		return []PropertyError{{
			Kind:     ErrTypeMismatch,
			Property: name,
			Type:     renderDeclType(decl),
			Value:    value,
			Reason:   fmt.Sprintf("expected type %q but value %q is not a valid RFC3339 datetime", decl.Type, str),
		}}
	}

	return nil
}

func validateEnum(name string, value any, decl manifest.PropertyDecl) []PropertyError {
	str, ok := value.(string)

	if !ok {
		return []PropertyError{{
			Kind:     ErrTypeMismatch,
			Property: name,
			Type:     renderDeclType(decl),
			Value:    value,
			Reason:   fmt.Sprintf("expected type %q but value is not a string", decl.Type),
		}}
	}

	for _, allowed := range decl.Values {
		if str == allowed {
			return nil
		}
	}

	return []PropertyError{{
		Kind:     ErrEnumViolation,
		Property: name,
		Type:     renderDeclType(decl),
		Value:    value,
		Reason:   fmt.Sprintf("value %q not in declared enum [%s]", str, strings.Join(decl.Values, ", ")),
	}}
}

func validateListOf(name string, value any, decl manifest.PropertyDecl) []PropertyError {
	list, ok := value.([]any)

	if !ok {
		return []PropertyError{{
			Kind:     ErrTypeMismatch,
			Property: name,
			Type:     renderDeclType(decl),
			Value:    value,
			Reason:   fmt.Sprintf("expected type %q but value is not a list", renderDeclType(decl)),
		}}
	}

	// Build a synthetic element decl using the item-type.
	elemDecl := manifest.PropertyDecl{
		Type:   decl.ItemType,
		Values: decl.Values,
	}

	var errs []PropertyError

	for idx, elem := range list {
		elemName := fmt.Sprintf("%s[%d]", name, idx)
		elemErrs := validateValue(elemName, elem, elemDecl, "")
		errs = append(errs, elemErrs...)
	}

	return errs
}
