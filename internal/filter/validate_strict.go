package filter

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/germanamz/tusk/internal/typeref"
)

// checkDeclaredNames runs ValidateStrict's checks on one property predicate:
// the node type a `type=` names, the property's declaration within scope, and
// an enum value compared with `=` or `!=`.
func (collector *validationCollector) checkDeclaredNames(pred *PropertyPredicate, scope typeScope) {
	if pred.Property == "type" {
		collector.checkDeclaredType(pred)

		return
	}

	if _, core := coreColumns[pred.Property]; core {
		return
	}

	declaredScope := collector.declaredScopeTypes(scope)

	// Every type in scope is undeclared: checkDeclaredType already reported
	// it, and "not declared on node type <typo>" would only add noise.
	if len(scope) > 0 && len(declaredScope) == 0 {
		return
	}

	decls := collector.lookupPropertyDecls(pred.Property, scope)

	if len(decls) == 0 {
		collector.add(pred.Pos, undeclaredPropertyMessage(pred.Property, declaredScope), suggestName(pred.Property, collector.propertyNames(declaredScope)))

		return
	}

	if len(decls) == 1 && (pred.Op == OpEQ || pred.Op == OpNE) {
		collector.checkEnumMember(pred, decls[0].Type == "enum" || (decls[0].Type == "list-of" && decls[0].ItemType == "enum"), decls[0].Values)
	}
}

// checkDeclaredType reports a `type=` / `type!=` value that is not a declared
// node type. The merged manifest includes the built-in sub-unit types whenever
// sub-units are enabled, so `type=section` is declared exactly when sections
// exist.
func (collector *validationCollector) checkDeclaredType(pred *PropertyPredicate) {
	if pred.Op != OpEQ && pred.Op != OpNE {
		return
	}

	stringValue, ok := pred.Value.(StringValue)

	if !ok {
		return
	}

	ref, parseErr := typeref.Parse(stringValue.V)

	if parseErr != nil {
		collector.add(pred.Pos, fmt.Sprintf("type %q is not a valid type reference", stringValue.V), "")

		return
	}

	if _, declared := collector.manifest.NodeTypes[ref.Type]; declared {
		return
	}

	collector.add(pred.Pos, fmt.Sprintf("node type %q not declared in manifest", ref.Type), suggestName(ref.Type, sortedKeys(collector.manifest.NodeTypes)))
}

// checkEnumMember reports an `=` / `!=` value outside the declared values of
// an enum or list-of(enum) property.
func (collector *validationCollector) checkEnumMember(pred *PropertyPredicate, isEnum bool, values []string) {
	if !isEnum {
		return
	}

	stringValue, ok := pred.Value.(StringValue)

	if !ok || slices.Contains(values, stringValue.V) {
		return
	}

	collector.add(pred.Pos, fmt.Sprintf("%q is not a value of enum property %q", stringValue.V, pred.Property), "valid values: "+strings.Join(values, ", "))
}

// declaredScopeTypes returns the sorted names in scope that the manifest
// declares. Empty for an empty scope.
func (collector *validationCollector) declaredScopeTypes(scope typeScope) []string {
	declared := make([]string, 0, len(scope))

	for typeName := range scope {
		if _, ok := collector.manifest.NodeTypes[typeName]; ok {
			declared = append(declared, typeName)
		}
	}

	sort.Strings(declared)

	return declared
}

// propertyNames returns the sorted, distinct property names declared on the
// given node types, or on every node type when typeNames is empty: the
// candidates for a "did you mean" hint.
func (collector *validationCollector) propertyNames(typeNames []string) []string {
	if len(typeNames) == 0 {
		typeNames = sortedKeys(collector.manifest.NodeTypes)
	}

	seen := map[string]struct{}{}

	for _, typeName := range typeNames {
		for _, decl := range collector.manifest.NodeTypes[typeName].Properties {
			seen[decl.Name] = struct{}{}
		}
	}

	return sortedKeys(seen)
}

// undeclaredPropertyMessage names where the property was looked for: the
// predicate's `type=` scope, or every node type when it has none.
func undeclaredPropertyMessage(property string, scopeTypes []string) string {
	switch len(scopeTypes) {
	case 0:
		return fmt.Sprintf("property %q is not declared on any node type", property)
	case 1:
		return fmt.Sprintf("property %q is not declared on node type %q", property, scopeTypes[0])
	}

	quoted := make([]string, len(scopeTypes))

	for position, typeName := range scopeTypes {
		quoted[position] = fmt.Sprintf("%q", typeName)
	}

	return fmt.Sprintf("property %q is not declared on node types %s", property, strings.Join(quoted, ", "))
}
