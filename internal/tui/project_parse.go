package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/syntax"
)

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

func parseProjectCreate(args []string) (config.ProjectConfig, error) {
	input := strings.Join(args, " ")
	fs, parseErrs := syntax.ParseFields(input)
	if len(parseErrs) > 0 {
		return config.ProjectConfig{}, fmt.Errorf("parse error: %s", parseErrs[0].Message)
	}
	proj := config.ProjectConfig{}
	for _, f := range fs.Fields {
		if f.Modifier != 0 {
			return config.ProjectConfig{}, fmt.Errorf("project create does not accept modifier %q on %q", f.Modifier, f.Key)
		}
		if err := applyProjectField(&proj, f.Key, f.Value); err != nil {
			return config.ProjectConfig{}, err
		}
	}
	return proj, nil
}

func applyProjectField(proj *config.ProjectConfig, key, value string) error {
	switch key {
	case "workflow":
		proj.Workflow = value
	case "auto-complete.trigger":
		if proj.Settings.AutoCompleteParent == nil {
			proj.Settings.AutoCompleteParent = &config.AutoCompleteParentConfig{}
		}
		proj.Settings.AutoCompleteParent.TriggerStatus = value
	case "auto-complete.target":
		if proj.Settings.AutoCompleteParent == nil {
			proj.Settings.AutoCompleteParent = &config.AutoCompleteParentConfig{}
		}
		proj.Settings.AutoCompleteParent.TargetStatus = value
	case "auto-revert.trigger":
		if proj.Settings.AutoRevertParent == nil {
			proj.Settings.AutoRevertParent = &config.AutoRevertParentConfig{}
		}
		proj.Settings.AutoRevertParent.TriggerStatus = value
	case "auto-revert.target":
		if proj.Settings.AutoRevertParent == nil {
			proj.Settings.AutoRevertParent = &config.AutoRevertParentConfig{}
		}
		proj.Settings.AutoRevertParent.TargetStatus = value
	default:
		cfgKey, ok := urgencyCLIToConfigKey(key)
		if !ok {
			return fmt.Errorf("unknown field %q", key)
		}
		f, err := parseFloatField(key, value)
		if err != nil {
			return err
		}
		if proj.Settings.Urgency == nil {
			proj.Settings.Urgency = &config.ProjectUrgencyConfig{}
		}
		fp := config.UrgencyFieldPtr(proj.Settings.Urgency, cfgKey)
		if fp == nil {
			return fmt.Errorf("unknown urgency key %q", cfgKey)
		}
		val := f
		*fp = &val
	}
	return nil
}

func parseProjectModify(args []string) (config.ProjectMutation, error) {
	input := strings.Join(args, " ")
	fs, parseErrs := syntax.ParseFields(input)
	if len(parseErrs) > 0 {
		return config.ProjectMutation{}, fmt.Errorf("parse error: %s", parseErrs[0].Message)
	}
	mut := config.ProjectMutation{
		UrgencySet:   map[string]float64{},
		UrgencyDelta: map[string]float64{},
	}

	for _, f := range fs.Fields {
		if f.Modifier != 0 {
			cfgKey, ok := urgencyCLIToConfigKey(f.Key)
			if !ok {
				return config.ProjectMutation{}, fmt.Errorf("modifier %q not supported on %q (only urgency weights)", f.Modifier, f.Key)
			}
			v, err := parseFloatField(f.Key, f.Value)
			if err != nil {
				return config.ProjectMutation{}, err
			}
			switch f.Modifier {
			case '+':
				mut.UrgencyDelta[cfgKey] = v
			case '-':
				mut.UrgencyDelta[cfgKey] = -v
			default:
				return config.ProjectMutation{}, fmt.Errorf("unsupported modifier %q", f.Modifier)
			}
			continue
		}

		switch f.Key {
		case "workflow":
			v := f.Value
			mut.Workflow = &v
		case "auto-complete.trigger", "auto-complete.target":
			if mut.AutoCompleteSet == nil {
				mut.AutoCompleteSet = &config.AutoCompleteParentConfig{}
			}
			if f.Key == "auto-complete.trigger" {
				mut.AutoCompleteSet.TriggerStatus = f.Value
			} else {
				mut.AutoCompleteSet.TargetStatus = f.Value
			}
		case "auto-revert.trigger", "auto-revert.target":
			if mut.AutoRevertSet == nil {
				mut.AutoRevertSet = &config.AutoRevertParentConfig{}
			}
			if f.Key == "auto-revert.trigger" {
				mut.AutoRevertSet.TriggerStatus = f.Value
			} else {
				mut.AutoRevertSet.TargetStatus = f.Value
			}
		default:
			cfgKey, ok := urgencyCLIToConfigKey(f.Key)
			if !ok {
				return config.ProjectMutation{}, fmt.Errorf("unknown field %q", f.Key)
			}
			v, err := parseFloatField(f.Key, f.Value)
			if err != nil {
				return config.ProjectMutation{}, err
			}
			mut.UrgencySet[cfgKey] = v
		}
	}
	return mut, nil
}
