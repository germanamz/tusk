package doctor

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/manifest"
)

// RuleKindPrefix starts the issue kind of a rule violation: a node breaking
// [rule.domain-matches-directory] is reported as rule:domain-matches-directory.
// Not an Issue* constant: the kind is per rule, and so is its severity.
const RuleKindPrefix = "rule:"

// RuleError is a [rule.<name>] declaration CompileRules rejected. Doctor
// reports it as rule-invalid; reload lists it under warnings.
type RuleError struct {
	Name    string
	Message string
}

// Error renders the problem as reload lists it under warnings.
func (ruleErr RuleError) Error() string {
	return fmt.Sprintf("rule %q: %s", ruleErr.Name, ruleErr.Message)
}

// CompiledRule is a valid [rule.<name>] declaration, ready to check.
type CompiledRule struct {
	Name        string
	Description string
	Severity    string
	matcher     ruleMatcher
}

// ruleKind is one kind of check a rule can run. A declaration selects a kind
// by setting its key, and must set exactly one. Adding a kind means a field on
// manifest.Rule, an entry in ruleKinds, and a matcher; the shared layer
// (severity, description, issue shaping, rule-invalid) is unchanged.
type ruleKind struct {
	// key is the manifest key that selects this kind.
	key string

	// selected reports whether the declaration sets key.
	selected func(rule manifest.Rule) bool

	// compile validates the declaration and builds its matcher. The error
	// message becomes the rule-invalid issue's message.
	compile func(rule manifest.Rule, loaded *manifest.Manifest) (ruleMatcher, error)
}

// ruleMatcher finds the nodes that break a compiled rule.
type ruleMatcher interface {
	violations(config Config) ([]ruleViolation, error)

	// summary is the issue message for a rule without a description.
	summary() string
}

// ruleViolation is one node breaking a rule. A struct so a later kind can
// attach detail without changing the matcher signature.
type ruleViolation struct {
	NodeID string
}

var ruleKinds = []ruleKind{
	{
		key:      "filter",
		selected: func(rule manifest.Rule) bool { return rule.Filter != "" },
		compile:  compileFilterRule,
	},
}

// ruleSeverities are the values `severity` accepts; empty means error.
var ruleSeverities = []string{SeverityError, SeverityWarning, SeverityAdvice}

// CompileRules validates every [rule.<name>] declaration in loaded and returns
// the valid ones ready to check, plus one RuleError per problem with the rest,
// both sorted by rule name. Pure: it reads the manifest and never fails, so
// doctor and reload can both call it.
func CompileRules(loaded *manifest.Manifest) ([]CompiledRule, []RuleError) {
	if loaded == nil || len(loaded.Rules) == 0 {
		return nil, nil
	}

	names := make([]string, 0, len(loaded.Rules))

	for name := range loaded.Rules {
		names = append(names, name)
	}

	sort.Strings(names)

	var (
		compiled []CompiledRule
		ruleErrs []RuleError
	)

	for _, name := range names {
		rule, problems := compileRule(name, loaded.Rules[name], loaded)

		if len(problems) > 0 {
			for _, problem := range problems {
				ruleErrs = append(ruleErrs, RuleError{Name: name, Message: problem})
			}

			continue
		}

		compiled = append(compiled, rule)
	}

	return compiled, ruleErrs
}

// compileRule checks the parts every kind shares, then compiles the selected
// kind. It returns every problem it finds, so one fix doesn't uncover the next.
func compileRule(name string, rule manifest.Rule, loaded *manifest.Manifest) (CompiledRule, []string) {
	if rule.DecodeError != "" {
		return CompiledRule{}, []string{rule.DecodeError}
	}

	var problems []string

	for _, key := range rule.UnknownKeys {
		problems = append(problems, fmt.Sprintf("unknown key %q (a rule takes %s)", key, strings.Join(knownRuleKeys(), ", ")))
	}

	severity := rule.Severity

	if severity == "" {
		severity = SeverityError
	}

	if !slices.Contains(ruleSeverities, severity) {
		problems = append(problems, fmt.Sprintf("severity %q is not one of %s", rule.Severity, strings.Join(ruleSeverities, ", ")))
	}

	var selected []ruleKind

	for _, kind := range ruleKinds {
		if kind.selected(rule) {
			selected = append(selected, kind)
		}
	}

	switch len(selected) {
	case 0:
		return CompiledRule{}, append(problems, "declares no check; set "+strings.Join(kindKeys(ruleKinds), " or "))
	case 1:
	default:
		return CompiledRule{}, append(problems, fmt.Sprintf("sets more than one check (%s); a rule runs exactly one", strings.Join(kindKeys(selected), ", ")))
	}

	matcher, compileErr := selected[0].compile(rule, loaded)

	if compileErr != nil {
		problems = append(problems, compileErr.Error())
	}

	if len(problems) > 0 {
		return CompiledRule{}, problems
	}

	return CompiledRule{Name: name, Description: rule.Description, Severity: severity, matcher: matcher}, nil
}

// knownRuleKeys lists the keys a [rule.<name>] block accepts: the shared
// ones, then each kind's.
func knownRuleKeys() []string {
	return append([]string{"description", "severity"}, kindKeys(ruleKinds)...)
}

func kindKeys(kinds []ruleKind) []string {
	keys := make([]string, len(kinds))

	for position, kind := range kinds {
		keys[position] = kind.key
	}

	return keys
}

// checkRules reports a rule-invalid issue per problem CompileRules finds and
// one rule:<name> issue per node breaking a valid rule, carrying the rule's own
// severity. No-op without a manifest; valid rules only run with a database.
func checkRules(config Config) ([]Issue, error) {
	if config.Manifest == nil {
		return nil, nil
	}

	compiled, ruleErrs := CompileRules(config.Manifest)

	issues := make([]Issue, 0, len(ruleErrs))

	for _, ruleErr := range ruleErrs {
		issues = append(issues, Issue{Kind: IssueRuleInvalid, NodeID: ruleErr.Name, Message: ruleErr.Message})
	}

	if config.DB == nil {
		return issues, nil
	}

	for _, rule := range compiled {
		violations, violationsErr := rule.matcher.violations(config)

		if violationsErr != nil {
			return nil, fmt.Errorf("doctor: rule %q: %w", rule.Name, violationsErr)
		}

		message := rule.Description

		if message == "" {
			message = rule.matcher.summary()
		}

		for _, violation := range violations {
			issues = append(issues, Issue{
				Kind:     RuleKindPrefix + rule.Name,
				Severity: rule.Severity,
				NodeID:   violation.NodeID,
				Message:  message,
			})
		}
	}

	return issues, nil
}

// filterMatcher is the filter kind: every node the compiled filter matches
// breaks the rule.
type filterMatcher struct {
	source string
	query  string
	params []any
}

// compileFilterRule parses, validates and compiles a rule's filter. Validation
// is strict when the manifest declares node types to check names against (a
// typo would otherwise match nothing forever), and query's own validation in a
// schemaless vault. The rule matches file nodes only unless the filter
// selects a sub-unit type, because sub-units share their file's path.
func compileFilterRule(rule manifest.Rule, loaded *manifest.Manifest) (ruleMatcher, error) {
	source := strings.TrimSpace(rule.Filter)

	if source == "" {
		return nil, errors.New("filter is empty; it would match every node")
	}

	expr, parseErrs := filter.NewParser(source).Parse()

	if len(parseErrs) > 0 {
		return nil, fmt.Errorf("filter: %s", joinMessages(parseErrs))
	}

	validate := filter.Validate

	if loaded.DeclaresUserNodeTypes() {
		validate = filter.ValidateStrict
	}

	if validationErrs := validate(expr, *loaded); len(validationErrs) > 0 {
		return nil, fmt.Errorf("filter: %s", joinMessages(validationErrs))
	}

	query, params, compileErr := filter.Compile(expr, filter.CompileOptions{FilesOnly: !selectsSubUnits(expr)})

	if compileErr != nil {
		return nil, fmt.Errorf("filter: %w", compileErr)
	}

	return filterMatcher{source: source, query: query, params: params}, nil
}

// selectsSubUnits reports whether the filter's outer level names a built-in
// sub-unit type, which opts a rule into matching sub-units.
func selectsSubUnits(expr filter.Expr) bool {
	subUnitTypes := manifest.SubdocumentNodeTypes()

	for _, typeName := range filter.OuterTypes(expr) {
		if _, isSubUnit := subUnitTypes[typeName]; isSubUnit {
			return true
		}
	}

	return false
}

// joinMessages renders filter errors on one line, the shape a doctor issue and
// a reload warning both print.
func joinMessages[Err error](errs []Err) string {
	messages := make([]string, len(errs))

	for position, err := range errs {
		messages[position] = err.Error()
	}

	return strings.Join(messages, "; ")
}

func (matcher filterMatcher) violations(config Config) ([]ruleViolation, error) {
	rows, queryErr := config.DB.Query(matcher.query, matcher.params...)

	if queryErr != nil {
		return nil, queryErr
	}

	defer rows.Close()

	var violations []ruleViolation

	for rows.Next() {
		var (
			id, nodeType, path, title, properties, checksum string
			mtime, size                                     int64
			parentID                                        sql.NullString
		)

		if scanErr := rows.Scan(&id, &nodeType, &path, &title, &properties, &mtime, &size, &checksum, &parentID); scanErr != nil {
			return nil, scanErr
		}

		violations = append(violations, ruleViolation{NodeID: id})
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}

	sort.Slice(violations, func(left, right int) bool {
		return violations[left].NodeID < violations[right].NodeID
	})

	return violations, nil
}

func (matcher filterMatcher) summary() string {
	return fmt.Sprintf("matches filter %q", matcher.source)
}
