package manifest

import (
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

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

// decodeRules decodes each [rule.<name>] block on its own, so a value of the
// wrong type fails only that rule (recorded on Rule.DecodeError) instead of
// the whole manifest. Returns an error only when `rule` itself is not a table
// of blocks.
func decodeRules(body string, loaded *Manifest) error {
	var wrapper struct {
		Rule map[string]toml.Primitive `toml:"rule"`
	}

	meta, decodeErr := toml.Decode(body, &wrapper)

	if decodeErr != nil {
		return decodeErr
	}

	if len(wrapper.Rule) == 0 {
		return nil
	}

	loaded.Rules = make(map[string]Rule, len(wrapper.Rule))

	for name, primitive := range wrapper.Rule {
		var rule Rule

		if ruleErr := meta.PrimitiveDecode(primitive, &rule); ruleErr != nil {
			rule = Rule{DecodeError: strings.TrimPrefix(ruleErr.Error(), "toml: ")}
		}

		loaded.Rules[name] = rule
	}

	recordUnknownRuleKeys(loaded, meta)

	return nil
}

// recordUnknownRuleKeys stamps each rule with the keys the decode left
// undecoded under its [rule.<name>] block. A nested table counts as one key.
// A rule that failed to decode is skipped: its valid keys are undecoded too.
func recordUnknownRuleKeys(loaded *Manifest, meta toml.MetaData) {
	unknownByRule := map[string]map[string]struct{}{}

	for _, key := range meta.Undecoded() {
		if len(key) < 3 || key[0] != "rule" {
			continue
		}

		if unknownByRule[key[1]] == nil {
			unknownByRule[key[1]] = map[string]struct{}{}
		}

		unknownByRule[key[1]][key[2]] = struct{}{}
	}

	for name, unknown := range unknownByRule {
		rule, declared := loaded.Rules[name]

		if !declared || rule.DecodeError != "" {
			continue
		}

		rule.UnknownKeys = make([]string, 0, len(unknown))

		for key := range unknown {
			rule.UnknownKeys = append(rule.UnknownKeys, key)
		}

		sort.Strings(rule.UnknownKeys)
		loaded.Rules[name] = rule
	}
}

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
