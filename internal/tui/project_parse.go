package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/syntax"
)

// projectCreateFields is the parser output for `tusk project create`. The
// handler resolves Workflow (a name) to a UUID and builds a CreateProjectInput.
type projectCreateFields struct {
	Workflow string
	Settings domain.ProjectSettings
}

// projectModifyFields is the parser output for `tusk project modify`. The
// handler resolves Workflow (a name) to a UUID and builds a ModifyProjectInput.
type projectModifyFields struct {
	Workflow     *string
	AutoComplete *domain.AutoCompleteConfig
	AutoRevert   *domain.AutoRevertConfig
	UrgencySet   map[string]float64
	UrgencyDelta map[string]float64
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

func urgencyOverrideFieldPtr(o *domain.UrgencyOverrides, key string) **float64 {
	switch key {
	case "priority_weight":
		return &o.PriorityWeight
	case "due_weight":
		return &o.DueWeight
	case "age_weight":
		return &o.AgeWeight
	case "active_weight":
		return &o.ActiveWeight
	case "blocking_weight":
		return &o.BlockingWeight
	case "blocked_weight":
		return &o.BlockedWeight
	case "tags_weight":
		return &o.TagsWeight
	case "project_weight":
		return &o.ProjectWeight
	case "annotations_weight":
		return &o.AnnotationsWeight
	case "waiting_weight":
		return &o.WaitingWeight
	}
	return nil
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
		fp := urgencyOverrideFieldPtr(out.Settings.Urgency, cfgKey)
		if fp == nil {
			return fmt.Errorf("unknown urgency key %q", cfgKey)
		}
		val := f
		*fp = &val
	}
	return nil
}

func parseProjectModify(args []string) (projectModifyFields, error) {
	input := strings.Join(args, " ")
	fs, parseErrs := syntax.ParseFields(input)
	if len(parseErrs) > 0 {
		return projectModifyFields{}, fmt.Errorf("parse error: %s", parseErrs[0].Message)
	}
	mut := projectModifyFields{
		UrgencySet:   map[string]float64{},
		UrgencyDelta: map[string]float64{},
	}

	for _, f := range fs.Fields {
		if f.Modifier != 0 {
			cfgKey, ok := urgencyCLIToConfigKey(f.Key)
			if !ok {
				return projectModifyFields{}, fmt.Errorf("modifier %q not supported on %q (only urgency weights)", f.Modifier, f.Key)
			}
			v, err := parseFloatField(f.Key, f.Value)
			if err != nil {
				return projectModifyFields{}, err
			}
			switch f.Modifier {
			case '+':
				mut.UrgencyDelta[cfgKey] = v
			case '-':
				mut.UrgencyDelta[cfgKey] = -v
			default:
				return projectModifyFields{}, fmt.Errorf("unsupported modifier %q", f.Modifier)
			}
			continue
		}

		switch f.Key {
		case "workflow":
			v := f.Value
			mut.Workflow = &v
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
		default:
			cfgKey, ok := urgencyCLIToConfigKey(f.Key)
			if !ok {
				return projectModifyFields{}, fmt.Errorf("unknown field %q", f.Key)
			}
			v, err := parseFloatField(f.Key, f.Value)
			if err != nil {
				return projectModifyFields{}, err
			}
			mut.UrgencySet[cfgKey] = v
		}
	}
	return mut, nil
}
