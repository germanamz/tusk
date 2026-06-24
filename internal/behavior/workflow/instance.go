package workflow

import (
	"sort"

	"github.com/germanamz/tusk/internal/behavior"
	"github.com/germanamz/tusk/internal/node"
)

// instance is the runtime form of one workflow configuration.
type instance struct {
	name           string
	appliesTo      map[string]struct{}
	statusProperty string
	// states maps each declared state name to whether it is an initial
	// state. The start/terminal/done role flags from the config are not
	// consulted by the v1 engine, so they are not retained here.
	states       map[string]bool
	transitions  map[transitionDecl]struct{}
	hasInitial   bool
	initialNames []string
}

func newInstance(name string, cfg workflowConfig) *instance {
	inst := &instance{
		name:           name,
		appliesTo:      map[string]struct{}{},
		statusProperty: cfg.StatusProperty,
		states:         map[string]bool{},
		transitions:    map[transitionDecl]struct{}{},
	}

	for _, typeName := range cfg.AppliesTo {
		inst.appliesTo[typeName] = struct{}{}
	}

	for _, state := range cfg.States {
		inst.states[state.Name] = state.Initial

		if state.Initial {
			inst.hasInitial = true
			inst.initialNames = append(inst.initialNames, state.Name)
		}
	}

	for _, trans := range cfg.Transitions {
		inst.transitions[trans] = struct{}{}
	}

	return inst
}

// Name satisfies behavior.Instance.
func (inst *instance) Name() string { return inst.name }

// Kind satisfies behavior.Instance.
func (inst *instance) Kind() string { return "workflow" }

// Hooks satisfies behavior.Instance — fills only OnNodeWriteValidate.
func (inst *instance) Hooks() behavior.Hooks {
	return behavior.Hooks{
		OnNodeWriteValidate: inst.validate,
	}
}

// ReservedKeys satisfies behavior.Instance — one key per type in
// applies-to, all reserving the configured status-property.
func (inst *instance) ReservedKeys() []behavior.ReservedKey {
	keys := make([]behavior.ReservedKey, 0, len(inst.appliesTo))

	for typeName := range inst.appliesTo {
		keys = append(keys, behavior.ReservedKey{NodeType: typeName, Property: inst.statusProperty})
	}

	sort.Slice(keys, func(aa, bb int) bool { return keys[aa].NodeType < keys[bb].NodeType })

	return keys
}

// validate is the OnNodeWriteValidate handler. Implements the algorithm
// in spec §5.2.
func (inst *instance) validate(ctx behavior.HookContext, before, after *node.Node) error {
	if after == nil {
		return nil
	}

	if _, governs := inst.appliesTo[after.Type]; !governs {
		return nil
	}

	beforeStatus := readStatus(before, inst.statusProperty)
	afterStatus := readStatus(after, inst.statusProperty)

	if beforeStatus == "" && afterStatus == "" {
		return nil
	}

	if beforeStatus == "" {
		// Setting status for the first time.
		if !inst.isKnownState(afterStatus) {
			return &Error{
				Code:         ErrUnknownTargetState,
				Property:     inst.statusProperty,
				From:         "",
				To:           afterStatus,
				KnownStates:  inst.knownStateNames(),
				PackInstance: inst.name,
			}
		}

		if inst.hasInitial && !inst.states[afterStatus] {
			return &Error{
				Code:         ErrNonInitialOnCreate,
				Property:     inst.statusProperty,
				From:         "",
				To:           afterStatus,
				KnownStates:  inst.initialNames,
				PackInstance: inst.name,
			}
		}

		return nil
	}

	if afterStatus == "" {
		// Unsetting a managed status.
		return &Error{
			Code:         ErrCannotUnsetStatus,
			Property:     inst.statusProperty,
			From:         beforeStatus,
			To:           "",
			PackInstance: inst.name,
		}
	}

	if !inst.isKnownState(beforeStatus) {
		// Orphan-state recovery.
		if !inst.isKnownState(afterStatus) {
			return &Error{
				Code:         ErrUnknownTargetState,
				Property:     inst.statusProperty,
				From:         beforeStatus,
				To:           afterStatus,
				KnownStates:  inst.knownStateNames(),
				PackInstance: inst.name,
			}
		}

		return &RecoveredError{
			Property:     inst.statusProperty,
			From:         beforeStatus,
			To:           afterStatus,
			PackInstance: inst.name,
		}
	}

	if beforeStatus == afterStatus {
		return nil
	}

	if !inst.isKnownState(afterStatus) {
		return &Error{
			Code:         ErrUnknownTargetState,
			Property:     inst.statusProperty,
			From:         beforeStatus,
			To:           afterStatus,
			KnownStates:  inst.knownStateNames(),
			PackInstance: inst.name,
		}
	}

	if _, ok := inst.transitions[transitionDecl{From: beforeStatus, To: afterStatus}]; !ok {
		return &Error{
			Code:         ErrIllegalTransition,
			Property:     inst.statusProperty,
			From:         beforeStatus,
			To:           afterStatus,
			ValidTargets: inst.validTargetsFrom(beforeStatus),
			PackInstance: inst.name,
		}
	}

	return nil
}

func (inst *instance) isKnownState(name string) bool {
	_, ok := inst.states[name]
	return ok
}

func (inst *instance) knownStateNames() []string {
	names := make([]string, 0, len(inst.states))

	for name := range inst.states {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

func (inst *instance) validTargetsFrom(from string) []string {
	var targets []string

	for trans := range inst.transitions {
		if trans.From == from {
			targets = append(targets, trans.To)
		}
	}

	sort.Strings(targets)

	return targets
}

func readStatus(nd *node.Node, property string) string {
	if nd == nil || nd.Properties == nil {
		return ""
	}

	value, found := nd.Properties[property]

	if !found {
		return ""
	}

	stringValue, ok := value.(string)

	if !ok {
		return ""
	}

	return stringValue
}
