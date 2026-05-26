// Package typeref parses the canonical <source>:<type> notation
// used throughout tusk for node-type and edge-type references.
// Three forms are accepted:
//
//	<source>:<type>     scoped to one source
//	:<type>             scoped to the user namespace (source = NULL)
//	<type>              shorthand — matches any source (union)
//
// Type and source names are drawn from [a-z0-9-]. The ':' separator
// is unambiguous against that grammar.
package typeref

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Scope is the resolution scope of a parsed reference.
type Scope int

const (
	// ScopeAny matches rows with any source (union).
	ScopeAny Scope = iota
	// ScopeUser matches rows whose source IS NULL.
	ScopeUser
	// ScopeSource matches rows whose source equals Ref.Source.
	ScopeSource
)

// String returns a readable label for debug output.
func (scope Scope) String() string {
	switch scope {
	case ScopeAny:
		return "any"
	case ScopeUser:
		return "user"
	case ScopeSource:
		return "source"
	default:
		return fmt.Sprintf("scope(%d)", int(scope))
	}
}

// Ref is a parsed type reference. Source is populated only when
// Scope == ScopeSource.
type Ref struct {
	Scope  Scope
	Source string
	Type   string
}

// EdgeRef is an alias for Ref used by edge-typed call sites.
type EdgeRef = Ref

// NodeRef is an alias for Ref used by node-typed call sites.
type NodeRef = Ref

// String renders the ref back to its canonical notation. Useful for
// logging and debug formatting; not used in storage.
func (ref Ref) String() string {
	switch ref.Scope {
	case ScopeAny:
		return ref.Type
	case ScopeUser:
		return ":" + ref.Type
	case ScopeSource:
		return ref.Source + ":" + ref.Type
	default:
		return fmt.Sprintf("invalid(%s)", ref.Type)
	}
}

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Parse decodes one of the three canonical forms into a Ref.
// Returns a descriptive error for empty input, malformed segments,
// or invalid characters in either segment.
func Parse(input string) (Ref, error) {
	if input == "" {
		return Ref{}, errors.New("typeref: input is empty")
	}

	if !strings.Contains(input, ":") {
		if !namePattern.MatchString(input) {
			return Ref{}, fmt.Errorf("typeref: type name %q must match [a-z0-9][a-z0-9-]*", input)
		}
		return Ref{Scope: ScopeAny, Type: input}, nil
	}

	parts := strings.SplitN(input, ":", 2)
	if len(parts) != 2 {
		return Ref{}, fmt.Errorf("typeref: input %q has too many ':' separators", input)
	}

	source, typeName := parts[0], parts[1]
	if strings.Contains(typeName, ":") {
		return Ref{}, fmt.Errorf("typeref: input %q has too many ':' separators", input)
	}
	if typeName == "" {
		return Ref{}, fmt.Errorf("typeref: input %q missing type name after ':'", input)
	}
	if !namePattern.MatchString(typeName) {
		return Ref{}, fmt.Errorf("typeref: type name %q must match [a-z0-9][a-z0-9-]*", typeName)
	}

	if source == "" {
		return Ref{Scope: ScopeUser, Type: typeName}, nil
	}

	if !namePattern.MatchString(source) {
		return Ref{}, fmt.Errorf("typeref: source name %q must match [a-z0-9][a-z0-9-]*", source)
	}

	return Ref{Scope: ScopeSource, Source: source, Type: typeName}, nil
}

// ParseMany applies Parse to every input. Returns the first error.
// Useful when callers receive a slice (e.g., graph-expansion config).
func ParseMany(inputs []string) ([]Ref, error) {
	out := make([]Ref, 0, len(inputs))
	for _, in := range inputs {
		ref, err := Parse(in)
		if err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, nil
}
