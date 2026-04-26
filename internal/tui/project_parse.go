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
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("field %q: %v", key, err)
	}
	return f, nil
}

func parseProjectCreate(args []string) (projectCreateFields, error) {
	input := strings.Join(args, " ")
	fs, parseErrs := syntax.ParseFields(input)
	if len(parseErrs) > 0 {
		return projectCreateFields{}, fmt.Errorf("parse error: %s", parseErrs[0].Message)
	}
	out := projectCreateFields{}
	for _, f := range fs.Fields {
		if f.Modifier != 0 {
			return projectCreateFields{}, fmt.Errorf("project create does not accept modifier %q on %q", f.Modifier, f.Key)
		}
		if err := applyProjectCreateField(&out, f.Key, f.Value); err != nil {
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
		f, err := parseFloatField(key, value)
		if err != nil {
			return err
		}
		if out.Settings.Urgency == nil {
			out.Settings.Urgency = &domain.UrgencyOverrides{}
		}
		fp := domain.UrgencyOverrideFieldPtr(out.Settings.Urgency, cfgKey)
		if fp == nil {
			return fmt.Errorf("unknown urgency key %q", cfgKey)
		}
		val := f
		*fp = &val
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
			tax, action, err := decodeTaxonomyJSON(value)
			if err != nil {
				return projectModifyFields{}, fmt.Errorf("taxonomy: %w", err)
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
	for i, f := range fs.Fields {
		urgencyInputs[i] = urgencyFieldInput{Key: f.Key, Value: f.Value, Modifier: f.Modifier}
	}
	urgencyResult, notConsumed, err := parseUrgencyFields(urgencyInputs)
	if err != nil {
		return projectModifyFields{}, err
	}
	if urgencyResult.ClearAll || len(urgencyResult.Clear) > 0 {
		return projectModifyFields{}, fmt.Errorf("urgency.clear=true and urgency.<weight>= (empty-value clear) are not supported on project modify; use tusk task modify for task-level overrides")
	}
	maps.Copy(mut.UrgencySet, urgencyResult.Set)
	maps.Copy(mut.UrgencyDelta, urgencyResult.Delta)

	for _, idx := range notConsumed {
		f := fs.Fields[idx]
		if f.Modifier != 0 {
			if strings.HasPrefix(f.Key, "taxonomy") {
				return projectModifyFields{}, fmt.Errorf("modifier %q not supported on %q", string(f.Modifier), f.Key)
			}
			return projectModifyFields{}, fmt.Errorf("modifier %q not supported on %q (only urgency weights)", f.Modifier, f.Key)
		}

		switch f.Key {
		case "workflow":
			v := f.Value
			mut.Workflow = &v
		case "description":
			if f.Value == "" {
				var inner *string
				mut.Description = &inner
			} else {
				v := f.Value
				inner := &v
				mut.Description = &inner
			}
		case "auto-complete.trigger", "auto-complete.target":
			if mut.AutoComplete == nil {
				mut.AutoComplete = &domain.AutoCompleteConfig{}
			}
			if f.Key == "auto-complete.trigger" {
				mut.AutoComplete.TriggerStatus = f.Value
			} else {
				mut.AutoComplete.TargetStatus = f.Value
			}
		case "auto-revert.trigger", "auto-revert.target":
			if mut.AutoRevert == nil {
				mut.AutoRevert = &domain.AutoRevertConfig{}
			}
			if f.Key == "auto-revert.trigger" {
				mut.AutoRevert.TriggerStatus = f.Value
			} else {
				mut.AutoRevert.TargetStatus = f.Value
			}
		case "taxonomy.levels":
			sawTaxLevels = true
			if f.Value == "" {
				mut.TaxonomyAction = taxonomyActionClear
				mut.TaxonomyValue = nil
				continue
			}
			tax, err := ParseTaxonomyInline(f.Value)
			if err != nil {
				return projectModifyFields{}, fmt.Errorf("taxonomy.levels: %w", err)
			}
			mut.TaxonomyAction = taxonomyActionSet
			mut.TaxonomyValue = tax
		case "taxonomy.disable":
			sawTaxDisable = true
			switch f.Value {
			case "true":
				mut.TaxonomyAction = taxonomyActionEmpty
				mut.TaxonomyValue = domain.Taxonomy{}
			case "false":
				mut.TaxonomyAction = taxonomyActionClear
				mut.TaxonomyValue = nil
			default:
				return projectModifyFields{}, fmt.Errorf("taxonomy.disable expects true or false, got %q", f.Value)
			}
		case "taxonomy":
			// Bare `taxonomy=<json>` is intercepted before the lexer pass above.
			// If the lexer still produces one it means the original argument
			// came through split by whitespace or quoted in a way that emitted
			// `taxonomy=` — reject so we do not silently ignore it.
			return projectModifyFields{}, fmt.Errorf("taxonomy= must be supplied as a single argument with a JSON object value")
		default:
			return projectModifyFields{}, fmt.Errorf("unknown field %q", f.Key)
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
	if err := json.Unmarshal([]byte(value), &payload); err != nil {
		return nil, taxonomyActionNone, fmt.Errorf("decoding JSON: %w", err)
	}
	tax := domain.Taxonomy(payload.Ranks)
	if tax.IsEmpty() {
		return domain.Taxonomy{}, taxonomyActionEmpty, nil
	}
	if err := tax.Validate(); err != nil {
		return nil, taxonomyActionNone, err
	}
	return tax, taxonomyActionSet, nil
}
