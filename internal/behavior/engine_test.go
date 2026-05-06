package behavior_test

import (
	"errors"
	"testing"

	"github.com/germanamz/tusk/internal/behavior"
	"github.com/germanamz/tusk/internal/node"
)

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

	engine, newErr := behavior.NewEngine([]behavior.Instance{first, second})

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

	engine, _ := behavior.NewEngine([]behavior.Instance{first, second})

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

	engine, _ := behavior.NewEngine([]behavior.Instance{first, second})

	if fireErr := engine.FireNodeWriteAfter(nil, &node.Node{Type: "ticket"}); fireErr == nil {
		test.Fatalf("FireNodeWriteAfter: expected aggregated error")
	}

	if len(calls) != 2 || calls[0] != "first" || calls[1] != "second" {
		test.Errorf("calls = %v, want [first second]", calls)
	}
}
