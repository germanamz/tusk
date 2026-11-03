package manifest

import (
	"github.com/BurntSushi/toml"
)

// RuleIgnoreKey is the frontmatter key that exempts a node from rules: its
// value names one [rule.<name>] or lists several, and doctor drops the node's
// violations of those rules (#852). Every node type accepts it without a
// declaration, and no node type may declare a property by this name.
const RuleIgnoreKey = "tusk-ignore"

// Rule is one [rule.<name>] declaration: an invariant doctor checks on every
// run, written as a check that names the nodes breaking it. Load only decodes
// rules; doctor validates them, so a bad rule is reported by doctor instead of
// failing the load.
//
// Each check kind owns one key (today only `filter`). Description and
// Severity are shared by every kind.
type Rule struct {
	// Description is the message doctor reports for each violation.
	Description string `toml:"description"`

	// Severity is "error", "warning" or "advice"; empty means "error".
	Severity string `toml:"severity"`

	// Filter selects the filter kind: a filter expression whose every match
	// is a violation.
	Filter string `toml:"filter"`

	// UnknownKeys lists the keys inside the block that no field decodes,
	// sorted and deduplicated, so a misspelled key is reported instead of
	// silently ignored. Set by Load.
	UnknownKeys []string `toml:"-"`

	// DecodeError is set by Load when the block does not decode into a Rule:
	// a value of the wrong type, or `rule.<name>` that is not a table. It
	// names the key and line. The other fields are then unreliable, so doctor
	// reports the rule invalid without looking further.
	DecodeError string `toml:"-"`
}

// decodeRules decodes each [rule.<name>] block on its own (see
// decodeNamedBlocks). Returns an error only when the manifest does not parse.
func decodeRules(body string, loaded *Manifest) error {
	var wrapper struct {
		Rule map[string]toml.Primitive `toml:"rule"`
	}

	meta, decodeErr := toml.Decode(body, &wrapper)

	if decodeErr != nil {
		return decodeErr
	}

	loaded.decodeMetas = append(loaded.decodeMetas, &meta)
	loaded.Rules = decodeNamedBlocks[Rule](&meta, "rule", wrapper.Rule)

	return nil
}

func (rule *Rule) setDecodeError(message string) { rule.DecodeError = message }

func (rule *Rule) setUnknownKeys(keys []string) { rule.UnknownKeys = keys }

func (rule *Rule) decodeFailed() bool { return rule.DecodeError != "" }

// DeclaresUserNodeTypes reports whether the manifest declares any node type
// beyond the built-in sub-document types, which MergeBuiltinPacks adds
// whenever sub-units are enabled. A vault that declares none is schemaless by
// choice, so checks that validate against declared types skip it.
func (loaded *Manifest) DeclaresUserNodeTypes() bool {
	if loaded == nil {
		return false
	}

	builtin := SubdocumentNodeTypes()

	for typeName := range loaded.NodeTypes {
		if _, isBuiltin := builtin[typeName]; !isBuiltin {
			return true
		}
	}

	return false
}
