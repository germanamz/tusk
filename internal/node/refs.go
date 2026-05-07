package node

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// RefLookup is the I/O surface ResolveRefs needs. Production wires it
// to *index.NodeRepo; tests pass synthetic fakes.
type RefLookup interface {
	FindByID(nodeID string) (nodeType string, found bool)
	FindByTitle(targetType, title string) ([]string, error)
}

// RefErrKind classifies a ref resolution failure for doctor and MCP.
type RefErrKind string

const (
	RefErrDangling     RefErrKind = "ref_dangling"
	RefErrAmbiguous    RefErrKind = "ref_ambiguous"
	RefErrTypeMismatch RefErrKind = "ref_type_mismatch"
	// RefErrCycle is emitted by the cycle detector layer, not by
	// ResolveRefs. Declared here so callers reference one constant set.
	RefErrCycle RefErrKind = "ref_cycle"
)

// RefError is one ref-resolution failure for one (property, value).
type RefError struct {
	Kind       RefErrKind
	Property   string
	Value      string
	To         string
	Candidates []string // populated for Ambiguous
	ActualType string   // populated for TypeMismatch
	Reason     string   // human-readable; built from the structured fields
}

// RefEdge is one resolved (edgeType, targetID, ordinal) tuple.
type RefEdge struct {
	EdgeType string
	TargetID string
	Ordinal  int
}

// RefResolutionResult is the resolver's verdict.
type RefResolutionResult struct {
	Edges      []RefEdge
	HardErrors []RefError
}

// refWikilinkPattern matches an entire string of the form "[[X]]"; the
// captured group is X. This is distinct from wikilinks.go's wikilinkPattern,
// which is unanchored and used to extract wikilinks from document bodies.
var refWikilinkPattern = regexp.MustCompile(`^\[\[(.+?)\]\]$`)

// ResolveRefs walks every ref-shaped property declared on parsed.Type,
// reads the corresponding value(s) from parsed.Properties, and resolves
// each into a RefEdge or a RefError. Pure relative to lookup.
func ResolveRefs(parsed *Node, decls map[string]manifest.NodeType, lookup RefLookup) RefResolutionResult {
	nodeType, declared := decls[parsed.Type]
	if !declared {
		return RefResolutionResult{}
	}

	var result RefResolutionResult

	for _, prop := range nodeType.Properties {
		if !manifest.IsRefProperty(prop) {
			continue
		}

		rawValue, present := parsed.Properties[prop.Name]
		if !present || rawValue == nil {
			continue
		}

		if prop.Type == "ref" {
			// Plain ref: value must be a single string.
			strValue, isString := rawValue.(string)

			if !isString {
				result.HardErrors = append(result.HardErrors, RefError{
					Kind:     RefErrDangling,
					Property: prop.Name,
					To:       prop.To,
					Reason:   fmt.Sprintf("ref property %q expects a single string value", prop.Name),
				})

				continue
			}

			edge, refErr := resolveOneValue(prop.Name, strValue, 0, prop.To, lookup)

			if refErr != nil {
				result.HardErrors = append(result.HardErrors, *refErr)
			} else if edge != nil {
				result.Edges = append(result.Edges, *edge)
			}
		} else {
			// list-of(ref): value must be []any.
			list, isList := rawValue.([]any)

			if !isList {
				result.HardErrors = append(result.HardErrors, RefError{
					Kind:     RefErrDangling,
					Property: prop.Name,
					To:       prop.To,
					Reason:   fmt.Sprintf("list-of(ref) property %q expects a list value", prop.Name),
				})

				continue
			}

			for idx, elem := range list {
				elemStr, isStr := elem.(string)

				if !isStr {
					result.HardErrors = append(result.HardErrors, RefError{
						Kind:     RefErrDangling,
						Property: prop.Name,
						To:       prop.To,
						Reason:   fmt.Sprintf("list-of(ref) property %q element [%d] is not a string", prop.Name, idx),
					})

					continue
				}

				edge, refErr := resolveOneValue(prop.Name, elemStr, idx, prop.To, lookup)

				if refErr != nil {
					result.HardErrors = append(result.HardErrors, *refErr)
				} else if edge != nil {
					result.Edges = append(result.Edges, *edge)
				}
			}
		}
	}

	return result
}

// resolveOneValue resolves a single string value for a ref property.
// Returns a RefEdge on success, a RefError on failure, or (nil, nil) when the
// value is empty (skip).
func resolveOneValue(propName, value string, ordinal int, targetType string, lookup RefLookup) (*RefEdge, *RefError) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}

	matches := refWikilinkPattern.FindStringSubmatch(trimmed)
	if len(matches) == 2 {
		// Wikilink branch: resolve by node ID.
		nodeID := matches[1]
		foundType, found := lookup.FindByID(nodeID)

		if !found {
			return nil, &RefError{
				Kind:     RefErrDangling,
				Property: propName,
				Value:    value,
				To:       targetType,
				Reason:   fmt.Sprintf("ref property %q — value %q did not match any node", propName, value),
			}
		}

		if targetType != "*" && foundType != targetType {
			return nil, &RefError{
				Kind:       RefErrTypeMismatch,
				Property:   propName,
				Value:      value,
				To:         targetType,
				ActualType: foundType,
				Reason:     fmt.Sprintf("ref property %q — value %q target type %q does not match required %q", propName, value, foundType, targetType),
			}
		}

		return &RefEdge{EdgeType: propName, TargetID: nodeID, Ordinal: ordinal}, nil
	}

	// Title lookup branch.
	candidates, lookupErr := lookup.FindByTitle(targetType, trimmed)

	if lookupErr != nil {
		return nil, &RefError{
			Kind:     RefErrDangling,
			Property: propName,
			Value:    value,
			To:       targetType,
			Reason:   fmt.Sprintf("ref property %q — title lookup error: %v", propName, lookupErr),
		}
	}

	switch len(candidates) {
	case 0:
		return nil, &RefError{
			Kind:     RefErrDangling,
			Property: propName,
			Value:    value,
			To:       targetType,
			Reason:   fmt.Sprintf("ref property %q — value %q did not match any node of type %q", propName, value, targetType),
		}
	case 1:
		return &RefEdge{EdgeType: propName, TargetID: candidates[0], Ordinal: ordinal}, nil
	default:
		return nil, &RefError{
			Kind:       RefErrAmbiguous,
			Property:   propName,
			Value:      value,
			To:         targetType,
			Candidates: candidates,
			Reason:     fmt.Sprintf("ref property %q — value %q matched %d nodes: %s", propName, value, len(candidates), strings.Join(candidates, ", ")),
		}
	}
}

// RefValidationError wraps ref resolution HardErrors so callers can type-assert
// to access the structured slice (MCP) while the human message works for CLI.
type RefValidationError struct {
	Op       string // "create" | "modify"
	NodeID   string
	NodeType string
	Errors   []RefError
}

// Error returns a joined human-readable summary of all ref validation failures.
func (refErr *RefValidationError) Error() string {
	count := len(refErr.Errors)

	noun := "error"
	if count != 1 {
		noun = "errors"
	}

	var sb strings.Builder

	fmt.Fprintf(&sb, "node-types: rejected %s: %s %q has %d ref %s:\n",
		refErr.Op, refErr.NodeType, refErr.NodeID, count, noun)

	for _, re := range refErr.Errors {
		fmt.Fprintf(&sb, "  - %s\n", re.Reason)
	}

	return strings.TrimRight(sb.String(), "\n")
}

// indexRefLookup is the production RefLookup backed by *index.NodeRepo.
type indexRefLookup struct {
	repo *index.NodeRepo
}

// NewIndexRefLookup returns a RefLookup that queries the SQLite index.
func NewIndexRefLookup(repo *index.NodeRepo) RefLookup {
	return &indexRefLookup{repo: repo}
}

// FindByID looks up a node by its ID and returns its type.
func (lookup *indexRefLookup) FindByID(nodeID string) (string, bool) {
	row, getErr := lookup.repo.Get(nodeID)

	if getErr != nil || row == nil {
		return "", false
	}

	return row.Type, true
}

// FindByTitle returns all node IDs whose title matches and (optionally) whose
// type matches targetType. Pass "*" for targetType to skip the type filter.
// Results are ordered by id for stable doctor candidate lists.
func (lookup *indexRefLookup) FindByTitle(targetType, title string) ([]string, error) {
	return lookup.repo.FindByTitle(targetType, title)
}
