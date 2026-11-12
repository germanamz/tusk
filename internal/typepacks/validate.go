package typepacks

import (
	"fmt"
	"sort"

	"github.com/BurntSushi/toml"

	"github.com/germanamz/tusk/internal/manifest"
)

// allowedTopLevelKeys is the closed set of section names a pack file
// may declare. Any other top-level key rejects. [check] stays out on
// purpose: a check is shell text, and a pack fetched from a URL must not
// be able to add a command for `tusk doctor --checks` to run.
var allowedTopLevelKeys = map[string]struct{}{
	"node-types": {},
	"edge-types": {},
	"behaviors":  {},
	"rule":       {},
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
			return nil, fmt.Errorf("pack add: pack contains disallowed top-level section %q (packs may only contain [node-types], [edge-types], [behaviors], [rule])", topLevel)
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

	// (c) Rules. Load leaves a bad rule for doctor to report, but a pack is
	// written into the manifest whole, so a block that does not decode is
	// refused here. Whether a filter names declared types depends on the
	// workspace the pack lands in, so that check stays with doctor.
	rules, ruleErr := manifest.DecodeRules(string(packBytes))

	if ruleErr != nil {
		return nil, fmt.Errorf("pack add: decode pack: %w", ruleErr)
	}

	if problem := firstRuleProblem(rules); problem != "" {
		return nil, fmt.Errorf("pack add: decode pack: %s", problem)
	}

	loaded.Rules = rules

	return loaded, nil
}

// firstRuleProblem names the first rule, in name order, whose block did not
// decode or carries a key a rule does not take. Empty when every block is
// well formed.
func firstRuleProblem(rules map[string]manifest.Rule) string {
	names := make([]string, 0, len(rules))

	for name := range rules {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		rule := rules[name]

		if rule.DecodeError != "" {
			return fmt.Sprintf("rule %q: %s", name, rule.DecodeError)
		}

		if len(rule.UnknownKeys) > 0 {
			return fmt.Sprintf("rule %q: unknown key %q", name, rule.UnknownKeys[0])
		}
	}

	return ""
}
