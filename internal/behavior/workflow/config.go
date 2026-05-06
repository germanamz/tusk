package workflow

import (
	"errors"
	"fmt"
)

// workflowConfig is the decoded TOML form of one [behaviors.workflow.<name>]
// table. Field tags match the manifest's snake-cased keys.
type workflowConfig struct {
	AppliesTo          []string         `toml:"applies-to"`
	StatusProperty     string           `toml:"status-property"`
	States             []stateDecl      `toml:"states"`
	Transitions        []transitionDecl `toml:"transitions"`
	AutoCompleteParent bool             `toml:"auto-complete-parent"`
	AutoRevertParent   bool             `toml:"auto-revert-parent"`
}

type stateDecl struct {
	Name     string `toml:"name"`
	Initial  bool   `toml:"initial"`
	Start    bool   `toml:"start"`
	Terminal bool   `toml:"terminal"`
	Done     bool   `toml:"done"`
}

type transitionDecl struct {
	From string `toml:"from"`
	To   string `toml:"to"`
}

// normalize applies defaults. Call after decode, before validate.
func (cfg *workflowConfig) normalize() {
	if cfg.StatusProperty == "" {
		cfg.StatusProperty = "status"
	}
}

// validate rejects schema violations per spec §4.3. Call after normalize.
func (cfg *workflowConfig) validate() error {
	if len(cfg.AppliesTo) == 0 {
		return errors.New("applies-to: required, must list at least one node type")
	}

	for index, typeName := range cfg.AppliesTo {
		if typeName == "" {
			return fmt.Errorf("applies-to[%d]: empty string is not a valid type name", index)
		}
	}

	if len(cfg.States) == 0 {
		return errors.New("states: required, must declare at least one state")
	}

	stateNames := map[string]struct{}{}

	var initialCount, startCount int

	for index, state := range cfg.States {
		if state.Name == "" {
			return fmt.Errorf("states[%d]: empty name", index)
		}

		if _, taken := stateNames[state.Name]; taken {
			return fmt.Errorf("states: duplicate state name %q", state.Name)
		}

		stateNames[state.Name] = struct{}{}

		if state.Initial {
			initialCount++
		}

		if state.Start {
			startCount++
		}

		if state.Done && !state.Terminal {
			return fmt.Errorf("states[%q]: done = true requires terminal = true (done implies terminal in v1)", state.Name)
		}
	}

	if initialCount > 1 {
		return errors.New("states: at most one state may set initial = true")
	}

	if startCount > 1 {
		return errors.New("states: at most one state may set start = true")
	}

	transitionPairs := map[transitionDecl]struct{}{}

	for index, trans := range cfg.Transitions {
		if _, ok := stateNames[trans.From]; !ok {
			return fmt.Errorf("transitions[%d]: from references undeclared state %q", index, trans.From)
		}

		if _, ok := stateNames[trans.To]; !ok {
			return fmt.Errorf("transitions[%d]: to references undeclared state %q", index, trans.To)
		}

		if _, taken := transitionPairs[trans]; taken {
			return fmt.Errorf("transitions: duplicate (from=%q, to=%q)", trans.From, trans.To)
		}

		transitionPairs[trans] = struct{}{}
	}

	return nil
}
