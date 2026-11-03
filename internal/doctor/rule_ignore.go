package doctor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// nodeIgnore is one file node's tusk-ignore value: the rules it lists, or the
// raw value when it is not a rule name or a list of them.
type nodeIgnore struct {
	nodeID    string
	rules     []string
	malformed string
}

// ruleIgnores is every tusk-ignore value in the vault, in node id order, and
// the record of which entries exempted a violation this run.
type ruleIgnores struct {
	nodes  []nodeIgnore
	byNode map[string]map[string]struct{}
	used   map[string]map[string]struct{}
}

// loadRuleIgnores reads the tusk-ignore value of every file node that sets
// one. A nil repo means no exemptions.
func loadRuleIgnores(nodes *index.NodeRepo) (*ruleIgnores, error) {
	ignores := &ruleIgnores{byNode: map[string]map[string]struct{}{}, used: map[string]map[string]struct{}{}}

	if nodes == nil {
		return ignores, nil
	}

	rows, listErr := nodes.ListFileNodesWithProperty(manifest.RuleIgnoreKey)

	if listErr != nil {
		return nil, fmt.Errorf("doctor: list %s: %w", manifest.RuleIgnoreKey, listErr)
	}

	for _, row := range rows {
		var properties map[string]json.RawMessage

		if decodeErr := json.Unmarshal([]byte(row.PropertiesJSON), &properties); decodeErr != nil {
			continue
		}

		entry := nodeIgnore{nodeID: row.ID}
		raw := properties[manifest.RuleIgnoreKey]
		rules, parsed := parseRuleIgnore(raw)

		if !parsed {
			entry.malformed = string(raw)
			ignores.nodes = append(ignores.nodes, entry)

			continue
		}

		listed := map[string]struct{}{}

		for _, rule := range rules {
			if _, seen := listed[rule]; seen {
				continue
			}

			listed[rule] = struct{}{}
			entry.rules = append(entry.rules, rule)
		}

		ignores.byNode[row.ID] = listed
		ignores.nodes = append(ignores.nodes, entry)
	}

	return ignores, nil
}

// parseRuleIgnore accepts one rule name or a list of them, each non-empty.
func parseRuleIgnore(raw json.RawMessage) ([]string, bool) {
	var single string

	if json.Unmarshal(raw, &single) == nil {
		return []string{single}, single != ""
	}

	var list []string

	if json.Unmarshal(raw, &list) != nil {
		return nil, false
	}

	for _, rule := range list {
		if rule == "" {
			return nil, false
		}
	}

	return list, true
}

// exempts reports whether the node behind a violation lists the rule, and
// records the entry as used. A sub-unit is covered by its file's value, since
// a section carries no frontmatter of its own.
func (ignores *ruleIgnores) exempts(violationID, ruleName string) bool {
	fileID, _, _ := strings.Cut(violationID, index.SubUnitIDSeparator)

	if _, listed := ignores.byNode[fileID][ruleName]; !listed {
		return false
	}

	if ignores.used[fileID] == nil {
		ignores.used[fileID] = map[string]struct{}{}
	}

	ignores.used[fileID][ruleName] = struct{}{}

	return true
}

// unusedIssues reports every entry that exempted nothing. An entry naming a
// rule that failed to compile is skipped: the rule never ran, so whether the
// node breaks it is unknown, and rule-invalid already reports the rule.
func (ignores *ruleIgnores) unusedIssues(loaded *manifest.Manifest, compiled []CompiledRule) []Issue {
	ran := make(map[string]struct{}, len(compiled))

	for _, rule := range compiled {
		ran[rule.Name] = struct{}{}
	}

	var issues []Issue

	for _, entry := range ignores.nodes {
		if entry.malformed != "" {
			issues = append(issues, Issue{
				Kind:    IssueRuleIgnoreUnused,
				NodeID:  entry.nodeID,
				Message: fmt.Sprintf("%s takes a rule name or a list of rule names; %s ignores nothing", manifest.RuleIgnoreKey, entry.malformed),
			})

			continue
		}

		for _, rule := range entry.rules {
			var message string

			_, declared := loaded.Rules[rule]
			_, didRun := ran[rule]
			_, wasUsed := ignores.used[entry.nodeID][rule]

			switch {
			case !declared:
				message = fmt.Sprintf("%s lists %q, but tusk.toml declares no [rule.%s]", manifest.RuleIgnoreKey, rule, rule)
			case didRun && !wasUsed:
				message = fmt.Sprintf("%s lists %q, but this node no longer breaks that rule; remove the entry", manifest.RuleIgnoreKey, rule)
			default:
				continue
			}

			issues = append(issues, Issue{Kind: IssueRuleIgnoreUnused, NodeID: entry.nodeID, Message: message})
		}
	}

	return issues
}
