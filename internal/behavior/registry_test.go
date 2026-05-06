// internal/behavior/registry_test.go
package behavior_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/germanamz/tusk/internal/behavior"
	"github.com/germanamz/tusk/internal/manifest"
)

// fakeKind constructs fakePack instances. Used by registry tests.
type fakeKind struct {
	name           string
	failOnInstance string // when non-empty, NewInstance for that name returns an error
	produced       func(instanceName string) *fakePack
}

func (kind *fakeKind) Name() string { return kind.name }

func (kind *fakeKind) NewInstance(instanceName string, raw toml.Primitive, meta *toml.MetaData) (behavior.Instance, error) {
	if kind.failOnInstance == instanceName {
		return nil, errors.New("decode error")
	}

	if kind.produced != nil {
		return kind.produced(instanceName), nil
	}

	return &fakePack{name: instanceName, kind: kind.name}, nil
}

func TestRegistry_RegisterDuplicateRejected(test *testing.T) {
	reg := behavior.NewRegistry()

	if registerErr := reg.Register(&fakeKind{name: "workflow"}); registerErr != nil {
		test.Fatalf("first Register: %v", registerErr)
	}

	registerErr := reg.Register(&fakeKind{name: "workflow"})

	if registerErr == nil {
		test.Errorf("second Register: expected duplicate-name error")
	}
}

func TestRegistry_BuildEngine_UnknownKindRejected(test *testing.T) {
	reg := behavior.NewRegistry()

	loaded := &manifest.Manifest{
		Behaviors: map[string]map[string]toml.Primitive{
			"missing": {"any": toml.Primitive{}},
		},
	}

	_, buildErr := reg.BuildEngine(loaded)

	if buildErr == nil {
		test.Fatalf("BuildEngine: expected error for unknown kind")
	}

	if !strings.Contains(buildErr.Error(), "missing") {
		test.Errorf("BuildEngine error = %v, want mention of unknown kind", buildErr)
	}
}

func TestRegistry_BuildEngine_PropagatesNewInstanceError(test *testing.T) {
	reg := behavior.NewRegistry()

	if registerErr := reg.Register(&fakeKind{name: "workflow", failOnInstance: "broken"}); registerErr != nil {
		test.Fatalf("Register: %v", registerErr)
	}

	loaded := &manifest.Manifest{
		Behaviors: map[string]map[string]toml.Primitive{
			"workflow": {"broken": toml.Primitive{}},
		},
	}

	_, buildErr := reg.BuildEngine(loaded)

	if buildErr == nil {
		test.Fatalf("BuildEngine: expected NewInstance error to surface")
	}
}

func TestRegistry_BuildEngine_CollisionDetected(test *testing.T) {
	reg := behavior.NewRegistry()

	colliding := func(instanceName string) *fakePack {
		return &fakePack{
			name: instanceName,
			kind: "workflow",
			reserved: []behavior.ReservedKey{
				{NodeType: "ticket", Property: "status"},
			},
		}
	}

	if registerErr := reg.Register(&fakeKind{name: "workflow", produced: colliding}); registerErr != nil {
		test.Fatalf("Register: %v", registerErr)
	}

	loaded := &manifest.Manifest{
		Behaviors: map[string]map[string]toml.Primitive{
			"workflow": {
				"a": toml.Primitive{},
				"b": toml.Primitive{},
			},
		},
	}

	_, buildErr := reg.BuildEngine(loaded)

	if buildErr == nil {
		test.Fatalf("BuildEngine: expected collision error")
	}

	if !strings.Contains(buildErr.Error(), "ticket") || !strings.Contains(buildErr.Error(), "status") {
		test.Errorf("BuildEngine error = %v, want collision mentioning ticket/status", buildErr)
	}
}
