package behavior_test

import (
	"errors"
	"testing"

	"github.com/germanamz/tusk/internal/behavior"
	"github.com/germanamz/tusk/internal/node"
)

func TestNewEngine_ReservedPropertiesIndexedByNodeType(test *testing.T) {
	workflowPack := &fakePack{
		name: "kanban",
		kind: "workflow",
		reserved: []behavior.ReservedKey{
			{NodeType: "ticket", Property: "status"},
			{NodeType: "epic", Property: "status"},
		},
	}

	engine, newErr := behavior.NewEngine([]behavior.Instance{workflowPack}, nil)

	if newErr != nil {
		test.Fatalf("NewEngine: %v", newErr)
	}

	reserved := engine.ReservedProperties()

	if _, ok := reserved["ticket"]["status"]; !ok {
		test.Errorf("expected ticket.status reserved, got %+v", reserved)
	}

	if _, ok := reserved["epic"]["status"]; !ok {
		test.Errorf("expected epic.status reserved, got %+v", reserved)
	}

	if _, ok := reserved["ticket"]["assignee"]; ok {
		test.Errorf("did not expect ticket.assignee reserved, got %+v", reserved)
	}
}

func TestNewEngine_SimpleNodeWriteValidate_ChainOrder(test *testing.T) {
	var calls []string

	first := &fakePack{
		name: "first",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteValidate: func(ctx behavior.HookContext, before, after *node.Node) error {
				calls = append(calls, "first")
				return nil
			},
		},
	}

	second := &fakePack{
		name: "second",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteValidate: func(ctx behavior.HookContext, before, after *node.Node) error {
				calls = append(calls, "second")
				return nil
			},
		},
	}

	engine, newErr := behavior.NewEngine([]behavior.Instance{first, second}, nil)

	if newErr != nil {
		test.Fatalf("NewEngine: %v", newErr)
	}

	rejector, fireErr := engine.FireNodeWriteValidate(nil, &node.Node{Type: "ticket"})

	if fireErr != nil {
		test.Fatalf("FireNodeWriteValidate: %v", fireErr)
	}

	if rejector != "" {
		test.Errorf("rejector = %q, want empty", rejector)
	}

	if len(calls) != 2 || calls[0] != "first" || calls[1] != "second" {
		test.Errorf("calls = %v, want [first second]", calls)
	}
}

func TestNewEngine_NodeWriteValidate_ShortCircuitsOnFirstRejection(test *testing.T) {
	var calls []string
	rejection := errors.New("boom")

	first := &fakePack{
		name: "first",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteValidate: func(ctx behavior.HookContext, before, after *node.Node) error {
				calls = append(calls, "first")
				return rejection
			},
		},
	}

	second := &fakePack{
		name: "second",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteValidate: func(ctx behavior.HookContext, before, after *node.Node) error {
				calls = append(calls, "second")
				return nil
			},
		},
	}

	engine, _ := behavior.NewEngine([]behavior.Instance{first, second}, nil)

	rejector, fireErr := engine.FireNodeWriteValidate(nil, &node.Node{Type: "ticket"})

	if !errors.Is(fireErr, rejection) {
		test.Errorf("fireErr = %v, want wrapped %v", fireErr, rejection)
	}

	if rejector != "fake.first" {
		test.Errorf("rejector = %q, want %q", rejector, "fake.first")
	}

	if len(calls) != 1 || calls[0] != "first" {
		test.Errorf("calls = %v, want [first] only (short-circuit)", calls)
	}
}

func TestNewEngine_NodeWriteAfter_FansOutUnconditionally(test *testing.T) {
	var calls []string

	first := &fakePack{
		name: "first",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteAfter: func(ctx behavior.HookContext, before, after *node.Node) error {
				calls = append(calls, "first")
				return errors.New("first error")
			},
		},
	}

	second := &fakePack{
		name: "second",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteAfter: func(ctx behavior.HookContext, before, after *node.Node) error {
				calls = append(calls, "second")
				return nil
			},
		},
	}

	engine, _ := behavior.NewEngine([]behavior.Instance{first, second}, nil)

	if fireErr := engine.FireNodeWriteAfter(nil, &node.Node{Type: "ticket"}); fireErr == nil {
		test.Fatalf("FireNodeWriteAfter: expected aggregated error")
	}

	if len(calls) != 2 || calls[0] != "first" || calls[1] != "second" {
		test.Errorf("calls = %v, want [first second]", calls)
	}
}

// recoverableErr is a test-only node.Recoverable used to exercise the
// recovery-aware Fire variant.
type recoverableErr struct {
	property string
	from     string
	to       string
	message  string
}

func (err *recoverableErr) Error() string { return err.message }

func (err *recoverableErr) AsRecoveredEvent(packKind, packInstance string) node.RecoveredEvent {
	return node.RecoveredEvent{
		PackKind:     packKind,
		PackInstance: packInstance,
		Property:     err.property,
		From:         err.from,
		To:           err.to,
		Message:      err.message,
	}
}

func TestFireNodeWriteValidateWithRecovery_RecoverableContinuesChain(test *testing.T) {
	var calls []string

	first := &fakePack{
		name: "first",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteValidate: func(ctx behavior.HookContext, before, after *node.Node) error {
				calls = append(calls, "first")
				return &recoverableErr{property: "status", from: "blocked", to: "active", message: "recovered"}
			},
		},
	}

	second := &fakePack{
		name: "second",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteValidate: func(ctx behavior.HookContext, before, after *node.Node) error {
				calls = append(calls, "second")
				return nil
			},
		},
	}

	engine, _ := behavior.NewEngine([]behavior.Instance{first, second}, nil)

	result, fireErr := engine.FireNodeWriteValidateWithRecovery(nil, &node.Node{Type: "ticket"})

	if fireErr != nil {
		test.Fatalf("FireNodeWriteValidateWithRecovery: %v", fireErr)
	}

	if result.Rejector != "" {
		test.Errorf("Rejector = %q, want empty", result.Rejector)
	}

	if len(result.Recovered) != 1 {
		test.Fatalf("Recovered = %v, want 1 event", result.Recovered)
	}

	got := result.Recovered[0]

	if got.PackKind != "fake" || got.PackInstance != "first" {
		test.Errorf("Recovered[0] qualifier = %q.%q, want fake.first", got.PackKind, got.PackInstance)
	}

	if got.Property != "status" || got.From != "blocked" || got.To != "active" {
		test.Errorf("Recovered[0] payload = %+v", got)
	}

	if len(calls) != 2 || calls[0] != "first" || calls[1] != "second" {
		test.Errorf("calls = %v, want [first second]", calls)
	}
}

func TestFireNodeWriteValidateWithRecovery_NonRecoverableShortCircuits(test *testing.T) {
	var calls []string
	rejection := errors.New("hard reject")

	first := &fakePack{
		name: "first",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteValidate: func(ctx behavior.HookContext, before, after *node.Node) error {
				calls = append(calls, "first")
				return rejection
			},
		},
	}

	second := &fakePack{
		name: "second",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteValidate: func(ctx behavior.HookContext, before, after *node.Node) error {
				calls = append(calls, "second")
				return nil
			},
		},
	}

	engine, _ := behavior.NewEngine([]behavior.Instance{first, second}, nil)

	result, fireErr := engine.FireNodeWriteValidateWithRecovery(nil, &node.Node{Type: "ticket"})

	if !errors.Is(fireErr, rejection) {
		test.Errorf("fireErr = %v, want wrapped %v", fireErr, rejection)
	}

	if result.Rejector != "fake.first" {
		test.Errorf("Rejector = %q, want fake.first", result.Rejector)
	}

	if len(calls) != 1 {
		test.Errorf("calls = %v, want [first] only (short-circuit)", calls)
	}
}
