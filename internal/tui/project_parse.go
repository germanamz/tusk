package tui

import (
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/syntax"
)

// projectCreateFields is the parser output for `tusk project create`. The
// handler resolves Workflow (a name) to a UUID and builds a CreateProjectInput.
type projectCreateFields struct {
	Workflow    string
	Description string
	Settings    domain.ProjectSettings
}

// taxonomyAction captures how `tusk project modify` should update
// project.Settings.Taxonomy. None = leave unchanged; Clear = drop the project
// override and inherit the workspace default; Empty = explicit opt-out (no
// levels enforced on this project); Set = replace with TaxonomyValue.
type taxonomyAction int

const (
	taxonomyActionNone  taxonomyAction = iota
	taxonomyActionClear                // clear override → inherit workspace default
	taxonomyActionEmpty                // explicit opt-out
	taxonomyActionSet                  // replace with TaxonomyValue
)

// projectModifyFields is the parser output for `tusk project modify`. The
// handler resolves Workflow (a name) to a UUID and builds a ModifyProjectInput.
type projectModifyFields struct {
	Workflow       *string
	Description    **string
	AutoComplete   *domain.AutoCompleteConfig
	AutoRevert     *domain.AutoRevertConfig
	UrgencySet     map[string]float64
	UrgencyDelta   map[string]float64
	TaxonomyAction taxonomyAction
	TaxonomyValue  domain.Taxonomy
}

func urgencyCLIToConfigKey(cliKey string) (string, bool) {
	if !strings.HasPrefix(cliKey, "urgency.") {
		return "", false
	}
	rest := strings.TrimPrefix(cliKey, "urgency.")
	key := strings.ReplaceAll(rest, "-", "_")
	switch key {
	case "priority_weight", "due_weight", "age_weight", "active_weight",
		"blocking_weight", "blocked_weight", "tags_weight", "project_weight",
		"annotations_weight", "waiting_weight":
		return key, true
	}
	return "", false
}

func parseFloatField(key, value string) (float64, error) {
	floatVal, parseErr := strconv.ParseFloat(value, 64)

	if parseErr != nil {
		return 0, fmt.Errorf("field %q: %v", key, parseErr)
	}

	return floatVal, nil
}

func parseProjectCreate(args []string) (projectCreateFields, error) {
	input := strings.Join(args, " ")
	fs, parseErrs := syntax.ParseFields(input)
	if len(parseErrs) > 0 {
		return projectCreateFields{}, fmt.Errorf("parse error: %s", parseErrs[0].Message)
	}
	out := projectCreateFields{}
	for _, field := range fs.Fields {
		if field.Modifier != 0 {
			return projectCreateFields{}, fmt.Errorf("project create does not accept modifier %q on %q", field.Modifier, field.Key)
		}
		if err := applyProjectCreateField(&out, field.Key, field.Value); err != nil {
			return projectCreateFields{}, err
		}
	}
	return out, nil
}

func applyProjectCreateField(out *projectCreateFields, key, value string) error {
	switch key {
	case "workflow":
		out.Workflow = value
	case "description":
		out.Description = value
	case "auto-complete.trigger":
		if out.Settings.AutoCompleteParent == nil {
			out.Settings.AutoCompleteParent = &domain.AutoCompleteConfig{}
		}
		out.Settings.AutoCompleteParent.TriggerStatus = value
	case "auto-complete.target":
		if out.Settings.AutoCompleteParent == nil {
			out.Settings.AutoCompleteParent = &domain.AutoCompleteConfig{}
		}
		out.Settings.AutoCompleteParent.TargetStatus = value
	case "auto-revert.trigger":
		if out.Settings.AutoRevertParent == nil {
			out.Settings.AutoRevertParent = &domain.AutoRevertConfig{}
		}
		out.Settings.AutoRevertParent.TriggerStatus = value
	case "auto-revert.target":
		if out.Settings.AutoRevertParent == nil {
			out.Settings.AutoRevertParent = &domain.AutoRevertConfig{}
		}
		out.Settings.AutoRevertParent.TargetStatus = value
	default:
		cfgKey, ok := urgencyCLIToConfigKey(key)
		if !ok {
			return fmt.Errorf("unknown field %q", key)
		}
		floatVal, floatErr := parseFloatField(key, value)

		if floatErr != nil {
			return floatErr
		}

		if out.Settings.Urgency == nil {
			out.Settings.Urgency = &domain.UrgencyOverrides{}
		}
		fieldPtr := domain.UrgencyOverrideFieldPtr(out.Settings.Urgency, cfgKey)
		if fieldPtr == nil {
			return fmt.Errorf("unknown urgency key %q", cfgKey)
		}
		val := floatVal
		*fieldPtr = &val
	}
	return nil
}

func parseProjectModify(args []string) (projectModifyFields, error) {
	mut := projectModifyFields{
		UrgencySet:   map[string]float64{},
		UrgencyDelta: map[string]float64{},
	}

	// Track which taxonomy keys appeared so we can enforce mutual exclusion.
	var sawTaxLevels, sawTaxDisable, sawTaxBare bool

	// Bare `taxonomy=<json>` values must bypass the regular lexer because the
	// JSON contains quote and bracket characters the field lexer rewrites. We
	// extract those args first and handle them inline, then pass the rest
	// through the standard parser.
	var lexable []string
	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if ok && key == "taxonomy" {
			sawTaxBare = true

			tax, action, taxErr := decodeTaxonomyJSON(value)

			if taxErr != nil {
				return projectModifyFields{}, fmt.Errorf("taxonomy: %w", taxErr)
			}

			mut.TaxonomyAction = action
			mut.TaxonomyValue = tax
			continue
		}
		// Reject any modifier on a bare taxonomy key up front (e.g. +taxonomy=...).
		if ok && (key == "+taxonomy" || key == "-taxonomy") {
			return projectModifyFields{}, fmt.Errorf("modifier %q not supported on %q", string(key[0]), "taxonomy")
		}
		lexable = append(lexable, arg)
	}

	input := strings.Join(lexable, " ")
	fs, parseErrs := syntax.ParseFields(input)
	if len(parseErrs) > 0 {
		return projectModifyFields{}, fmt.Errorf("parse error: %s", parseErrs[0].Message)
	}

	urgencyInputs := make([]urgencyFieldInput, len(fs.Fields))
	for index, field := range fs.Fields {
		urgencyInputs[index] = urgencyFieldInput{Key: field.Key, Value: field.Value, Modifier: field.Modifier}
	}

	urgencyResult, notConsumed, urgencyErr := parseUrgencyFields(urgencyInputs)

	if urgencyErr != nil {
		return projectModifyFields{}, urgencyErr
	}

	if urgencyResult.ClearAll || len(urgencyResult.Clear) > 0 {
		return projectModifyFields{}, fmt.Errorf("urgency.clear=true and urgency.<weight>= (empty-value clear) are not supported on project modify; use tusk task modify for task-level overrides")
	}
	maps.Copy(mut.UrgencySet, urgencyResult.Set)
	maps.Copy(mut.UrgencyDelta, urgencyResult.Delta)

	for _, idx := range notConsumed {
		field := fs.Fields[idx]
		if field.Modifier != 0 {
			if strings.HasPrefix(field.Key, "taxonomy") {
				return projectModifyFields{}, fmt.Errorf("modifier %q not supported on %q", string(field.Modifier), field.Key)
			}
			return projectModifyFields{}, fmt.Errorf("modifier %q not supported on %q (only urgency weights)", field.Modifier, field.Key)
		}

		switch field.Key {
		case "workflow":
			value := field.Value
			mut.Workflow = &value
		case "description":
			if field.Value == "" {
				var inner *string
				mut.Description = &inner
			} else {
				value := field.Value
				inner := &value
				mut.Description = &inner
			}
		case "auto-complete.trigger", "auto-complete.target":
			if mut.AutoComplete == nil {
				mut.AutoComplete = &domain.AutoCompleteConfig{}
			}
			if field.Key == "auto-complete.trigger" {
				mut.AutoComplete.TriggerStatus = field.Value
			} else {
				mut.AutoComplete.TargetStatus = field.Value
			}
		case "auto-revert.trigger", "auto-revert.target":
			if mut.AutoRevert == nil {
				mut.AutoRevert = &domain.AutoRevertConfig{}
			}
			if field.Key == "auto-revert.trigger" {
				mut.AutoRevert.TriggerStatus = field.Value
			} else {
				mut.AutoRevert.TargetStatus = field.Value
			}
		case "taxonomy.levels":
			sawTaxLevels = true
			if field.Value == "" {
				mut.TaxonomyAction = taxonomyActionClear
				mut.TaxonomyValue = nil
				continue
			}
			tax, taxonomyErr := ParseTaxonomyInline(field.Value)

			if taxonomyErr != nil {
				return projectModifyFields{}, fmt.Errorf("taxonomy.levels: %w", taxonomyErr)
			}

			mut.TaxonomyAction = taxonomyActionSet
			mut.TaxonomyValue = tax
		case "taxonomy.disable":
			sawTaxDisable = true
			switch field.Value {
			case "true":
				mut.TaxonomyAction = taxonomyActionEmpty
				mut.TaxonomyValue = domain.Taxonomy{}
			case "false":
				mut.TaxonomyAction = taxonomyActionClear
				mut.TaxonomyValue = nil
			default:
				return projectModifyFields{}, fmt.Errorf("taxonomy.disable expects true or false, got %q", field.Value)
			}
		case "taxonomy":
			// Bare `taxonomy=<json>` is intercepted before the lexer pass above.
			// If the lexer still produces one it means the original argument
			// came through split by whitespace or quoted in a way that emitted
			// `taxonomy=` — reject so we do not silently ignore it.
			return projectModifyFields{}, fmt.Errorf("taxonomy= must be supplied as a single argument with a JSON object value")
		default:
			return projectModifyFields{}, fmt.Errorf("unknown field %q", field.Key)
		}
	}

	// Mutual exclusion across the three taxonomy input forms.
	taxCount := 0
	if sawTaxLevels {
		taxCount++
	}
	if sawTaxDisable {
		taxCount++
	}
	if sawTaxBare {
		taxCount++
	}
	if taxCount > 1 {
		return projectModifyFields{}, fmt.Errorf("taxonomy.levels, taxonomy.disable, and taxonomy=<json> are mutually exclusive in a single call")
	}

	return mut, nil
}

// decodeTaxonomyJSON decodes the value of a bare `taxonomy=` field. The shape
// is {"ranks": [[...], ...]}. An empty ranks list yields Empty (explicit
// opt-out); a populated ranks list yields Set with the parsed taxonomy after
// structural validation.
func decodeTaxonomyJSON(value string) (domain.Taxonomy, taxonomyAction, error) {
	var payload struct {
		Ranks [][]string `json:"ranks"`
	}

	if unmarshalErr := json.Unmarshal([]byte(value), &payload); unmarshalErr != nil {
		return nil, taxonomyActionNone, fmt.Errorf("decoding JSON: %w", unmarshalErr)
	}

	tax := domain.Taxonomy(payload.Ranks)
	if tax.IsEmpty() {
		return domain.Taxonomy{}, taxonomyActionEmpty, nil
	}

	if validateErr := tax.Validate(); validateErr != nil {
		return nil, taxonomyActionNone, validateErr
	}

	return tax, taxonomyActionSet, nil
}
