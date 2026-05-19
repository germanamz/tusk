package manifest

import (
	"fmt"
	"os"
	"sort"

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
	"ref":      {},
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

	if validateErr := validateRefTargets(loaded); validateErr != nil {
		return validateErr
	}

	if validateErr := resolveOrdered(loaded); validateErr != nil {
		return validateErr
	}

	if synthesizeErr := synthesizeRefEdgeTypes(loaded); synthesizeErr != nil {
		return synthesizeErr
	}

	synthesizeHierarchyBackCompat(loaded)

	if validateErr := validateHierarchies(loaded); validateErr != nil {
		return validateErr
	}

	return validateBehaviors(loaded)
}

// resolveOrdered decodes the polymorphic OrderedRaw into Ordered + OrderedBy
// for every explicit edge type. Runs after node-type validation so it can
// confirm the named property exists and is sortable on every `from` type.
func resolveOrdered(loaded *Manifest) error {
	if loaded.Meta == nil {
		// Hand-constructed manifest: caller is expected to set Ordered/OrderedBy directly.
		return nil
	}

	for edgeName, edge := range loaded.EdgeTypes {
		// Default values when `ordered` key is absent.
		ordered := false
		orderedBy := ""

		if loaded.Meta.IsDefined("edge-types", edgeName, "ordered") {
			var asBool bool
			boolErr := loaded.Meta.PrimitiveDecode(edge.OrderedRaw, &asBool)

			if boolErr == nil {
				if asBool {
					ordered = true
					orderedBy = "order"
				}
			} else {
				var asString string

				if stringErr := loaded.Meta.PrimitiveDecode(edge.OrderedRaw, &asString); stringErr != nil {
					return fmt.Errorf("manifest: edge-types.%s: ordered must be bool or string (got error: %v)", edgeName, stringErr)
				}

				if asString == "" {
					return fmt.Errorf("manifest: edge-types.%s: ordered cannot be the empty string", edgeName)
				}

				ordered = true
				orderedBy = asString
			}
		}

		edge.Ordered = ordered
		edge.OrderedBy = orderedBy

		if orderedBy != "" {
			if validateErr := validateOrderedByProperty(loaded, edgeName, edge); validateErr != nil {
				return validateErr
			}
		}

		loaded.EdgeTypes[edgeName] = edge
	}

	return nil
}

// sortablePropertyTypes are the property types valid as an ordered-by key.
var sortablePropertyTypes = map[string]struct{}{
	"string":   {},
	"int":      {},
	"float":    {},
	"date":     {},
	"datetime": {},
}

func validateOrderedByProperty(loaded *Manifest, edgeName string, edge EdgeType) error {
	for _, fromType := range edge.From {
		if fromType == "*" {
			return fmt.Errorf(
				"manifest: edge-types.%s: ordered = %q is not allowed when from contains the wildcard %q; set ordered = false or list explicit node types in from",
				edgeName, edge.OrderedBy, "*",
			)
		}

		nodeType, declared := loaded.NodeTypes[fromType]

		if !declared {
			return fmt.Errorf(
				"manifest: edge-types.%s: ordered = %q references node-type %q which is not declared",
				edgeName, edge.OrderedBy, fromType,
			)
		}

		var found *PropertyDecl

		for index := range nodeType.Properties {
			if nodeType.Properties[index].Name == edge.OrderedBy {
				found = &nodeType.Properties[index]
				break
			}
		}

		if found == nil {
			return fmt.Errorf(
				"manifest: edge-types.%s: ordered = %q references property %q which is not declared on node-type %q",
				edgeName, edge.OrderedBy, edge.OrderedBy, fromType,
			)
		}

		if _, sortable := sortablePropertyTypes[found.Type]; !sortable {
			return fmt.Errorf(
				"manifest: edge-types.%s: ordered = %q references property %q on %q whose type %q is not sortable (allowed: string, int, float, date, datetime)",
				edgeName, edge.OrderedBy, edge.OrderedBy, fromType, found.Type,
			)
		}
	}

	return nil
}

// IsRefProperty returns true when the property declaration is a ref-shaped type:
// either Type == "ref" or (Type == "list-of" && ItemType == "ref").
// Exported so packages like node can use it without duplicating the predicate.
func IsRefProperty(prop PropertyDecl) bool {
	return prop.Type == "ref" || (prop.Type == "list-of" && prop.ItemType == "ref")
}

// isRefProperty is the package-internal alias for IsRefProperty.
func isRefProperty(prop PropertyDecl) bool {
	return IsRefProperty(prop)
}

// synthesizeHierarchyBackCompat preserves pre-#405 behavior: a workspace that
// declares a literal edge type named "parent" without setting a hierarchy
// alias gets the alias "parent" applied automatically, and becomes the
// default hierarchy if no other edge has claimed default. Run after parse
// and before validate so downstream validation sees the synthesized values.
func synthesizeHierarchyBackCompat(loaded *Manifest) {
	edge, exists := loaded.EdgeTypes["parent"]

	if !exists {
		return
	}

	if edge.Hierarchy != "" {
		return
	}

	edge.Hierarchy = "parent"

	otherDefaultExists := false

	for otherName, other := range loaded.EdgeTypes {
		if otherName == "parent" {
			continue
		}

		if other.HierarchyDefault {
			otherDefaultExists = true

			break
		}
	}

	if !otherDefaultExists {
		edge.HierarchyDefault = true
	}

	loaded.EdgeTypes["parent"] = edge
}

// buildEdgeTypeFromRef constructs an EdgeType from the owning node-type name
// and a ref-shaped PropertyDecl, applying the synthesis rules from the spec.
func buildEdgeTypeFromRef(owningType string, prop PropertyDecl) EdgeType {
	cardinality := CardinalityManyToOne

	if prop.Type == "list-of" && prop.ItemType == "ref" {
		cardinality = CardinalityManyToMany
	}

	ordered := false
	orderedBy := ""

	if prop.Type == "list-of" && prop.ItemType == "ref" && prop.Ordered {
		ordered = true
		orderedBy = "order"
	}

	return EdgeType{
		Description: fmt.Sprintf("auto-generated from %s.%s", owningType, prop.Name),
		From:        []string{owningType},
		To:          []string{prop.To},
		Cardinality: cardinality,
		Ordered:     ordered,
		OrderedBy:   orderedBy,
		Inverse:     prop.Inverse,
		Acyclic:     prop.Acyclic,
	}
}

// synthesizeRefEdgeTypes walks every ref-shaped PropertyDecl and synthesizes
// one EdgeType per ref-property name in loaded.EdgeTypes. It runs after
// validateRefTargets so all ref shapes are structurally valid before synthesis.
// The function mutates loaded.EdgeTypes in place.
func synthesizeRefEdgeTypes(loaded *Manifest) error {
	if loaded.EdgeTypes == nil {
		loaded.EdgeTypes = EdgeTypes{}
	}

	// Snapshot names already present before synthesis begins — these are
	// explicit user-declared [edge-types.X] blocks (Task 5 collision check).
	explicit := make(map[string]struct{}, len(loaded.EdgeTypes))

	for name := range loaded.EdgeTypes {
		explicit[name] = struct{}{}
	}

	// synthesized tracks which edge-type names were produced in this pass and
	// which node-type first produced each.
	synthesized := make(map[string]string, len(loaded.NodeTypes))

	// Iterate node-types in sorted order for deterministic From slice ordering.
	typeNames := make([]string, 0, len(loaded.NodeTypes))

	for typeName := range loaded.NodeTypes {
		typeNames = append(typeNames, typeName)
	}

	sort.Strings(typeNames)

	for _, typeName := range typeNames {
		nodeType := loaded.NodeTypes[typeName]

		for _, prop := range nodeType.Properties {
			if !isRefProperty(prop) {
				continue
			}

			// Collision with an explicit user-declared edge type rejects.
			if _, isExplicit := explicit[prop.Name]; isExplicit {
				return fmt.Errorf(
					"manifest: edge type %q is auto-generated by ref property %q.%q; remove the explicit [edge-types.%s] declaration or rename the property",
					prop.Name, typeName, prop.Name, prop.Name,
				)
			}

			candidate := buildEdgeTypeFromRef(typeName, prop)

			firstOwner, alreadySynthesized := synthesized[prop.Name]

			if !alreadySynthesized {
				// First occurrence: store directly.
				loaded.EdgeTypes[prop.Name] = candidate
				synthesized[prop.Name] = typeName

				continue
			}

			// Subsequent occurrence: merge by extending From, or reject on conflict.
			existing := loaded.EdgeTypes[prop.Name]

			if mergeErr := assertCompatibleSynthesis(existing, candidate, prop.Name, firstOwner, typeName); mergeErr != nil {
				return mergeErr
			}

			existing.From = appendUniqueString(existing.From, typeName)
			loaded.EdgeTypes[prop.Name] = existing
		}
	}

	return nil
}

// assertCompatibleSynthesis checks that two synthesized EdgeType values agree
// on all non-From attributes. Returns an error naming the first mismatch.
func assertCompatibleSynthesis(existing, candidate EdgeType, propName, firstOwner, newOwner string) error {
	if existing.Cardinality != candidate.Cardinality {
		return fmt.Errorf(
			"manifest: ref property %q is declared on both %q and %q with conflicting attributes (cardinality: %s vs %s); align the declarations or use distinct property names",
			propName, firstOwner, newOwner, existing.Cardinality, candidate.Cardinality,
		)
	}

	if len(existing.To) != len(candidate.To) || (len(existing.To) > 0 && existing.To[0] != candidate.To[0]) {
		return fmt.Errorf(
			"manifest: ref property %q is declared on both %q and %q with conflicting attributes (to: %v vs %v); align the declarations or use distinct property names",
			propName, firstOwner, newOwner, existing.To, candidate.To,
		)
	}

	if existing.OrderedBy != candidate.OrderedBy {
		return fmt.Errorf(
			"manifest: ref property %q is declared on both %q and %q with conflicting attributes (ordered-by: %q vs %q); align the declarations or use distinct property names",
			propName, firstOwner, newOwner, existing.OrderedBy, candidate.OrderedBy,
		)
	}

	if existing.Inverse != candidate.Inverse {
		return fmt.Errorf(
			"manifest: ref property %q is declared on both %q and %q with conflicting attributes (inverse: %q vs %q); align the declarations or use distinct property names",
			propName, firstOwner, newOwner, existing.Inverse, candidate.Inverse,
		)
	}

	if existing.Acyclic != candidate.Acyclic {
		return fmt.Errorf(
			"manifest: ref property %q is declared on both %q and %q with conflicting attributes (acyclic: %v vs %v); align the declarations or use distinct property names",
			propName, firstOwner, newOwner, existing.Acyclic, candidate.Acyclic,
		)
	}

	return nil
}

// appendUniqueString appends str to slice only if it is not already present.
func appendUniqueString(slice []string, str string) []string {
	for _, existing := range slice {
		if existing == str {
			return slice
		}
	}

	return append(slice, str)
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

// validatePropertyTypeConstraints enforces per-type rules for enum, list-of, ref, and
// misplaced constraint keys.
func validatePropertyTypeConstraints(typeName string, prop PropertyDecl) error {
	switch prop.Type {
	case "enum":
		if enumErr := validateEnumValues(typeName, prop.Name, prop.Values); enumErr != nil {
			return enumErr
		}

	case "ref":
		if prop.To == "" {
			return fmt.Errorf("manifest: node-types.%s.%s: ref property requires `to`", typeName, prop.Name)
		}

		if len(prop.Values) > 0 {
			return fmt.Errorf("manifest: node-types.%s.%s: ref property cannot declare values", typeName, prop.Name)
		}

		if prop.ItemType != "" {
			return fmt.Errorf("manifest: node-types.%s.%s: ref property cannot declare item-type", typeName, prop.Name)
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

		if prop.ItemType == "ref" && prop.To == "" {
			return fmt.Errorf("manifest: node-types.%s.%s: ref property requires `to`", typeName, prop.Name)
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

// validateRefTargets checks that every ref property's To target is either "*"
// or the name of a declared node-type. This pass runs after validateNodeTypes
// so the full NodeTypes map is available.
func validateRefTargets(loaded *Manifest) error {
	for typeName, nodeType := range loaded.NodeTypes {
		for _, prop := range nodeType.Properties {
			isRef := prop.Type == "ref"
			isListOfRef := prop.Type == "list-of" && prop.ItemType == "ref"

			if !isRef && !isListOfRef {
				continue
			}

			if prop.To == "" || prop.To == "*" {
				continue
			}

			if _, exists := loaded.NodeTypes[prop.To]; !exists {
				return fmt.Errorf("manifest: node-types.%s.%s: ref to unknown node-type %q", typeName, prop.Name, prop.To)
			}
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

// shortcutKeywords are reserved query-shortcut identifiers; hierarchy aliases
// must not collide with them, since the parser keys off these names.
var shortcutKeywords = map[string]bool{"tree": true, "parent": true, "root": true}

// validateHierarchies enforces the structural rules for hierarchy aliases and
// defaults: no collision with keywords, no duplicate aliases, at most one
// default, and default-only when hierarchy is set.
func validateHierarchies(loaded *Manifest) error {
	aliasToEdge := map[string]string{}

	var defaultEdge string

	for edgeName, edge := range loaded.EdgeTypes {
		if edge.Hierarchy == "" {
			if edge.HierarchyDefault {
				return fmt.Errorf("edge %q: hierarchy-default requires hierarchy alias", edgeName)
			}

			continue
		}

		// Special case: allow bare "parent" edge to use "parent" hierarchy for back-compat.
		// This is the one exception to the keyword-collision rule.
		if shortcutKeywords[edge.Hierarchy] && (edgeName != "parent" || edge.Hierarchy != "parent") {
			return fmt.Errorf("edge %q: hierarchy alias %q collides with shortcut keyword", edgeName, edge.Hierarchy)
		}

		if previous, exists := aliasToEdge[edge.Hierarchy]; exists {
			return fmt.Errorf("duplicate hierarchy alias %q on edges %q and %q", edge.Hierarchy, previous, edgeName)
		}

		aliasToEdge[edge.Hierarchy] = edgeName

		if edge.HierarchyDefault {
			if defaultEdge != "" {
				return fmt.Errorf("multiple edges declare hierarchy-default = true: %q, %q", defaultEdge, edgeName)
			}

			defaultEdge = edgeName
		}
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

		if loaded.Embeddings.Workers < 0 {
			return fmt.Errorf("manifest: embeddings.workers must be >= 0 (got %d); zero or absent uses the default", loaded.Embeddings.Workers)
		}

		if loaded.Embeddings.TimeoutSeconds < 0 {
			return fmt.Errorf("manifest: embeddings.timeout-seconds must be >= 0 (got %d); zero or absent uses the default", loaded.Embeddings.TimeoutSeconds)
		}
	}

	return nil
}
