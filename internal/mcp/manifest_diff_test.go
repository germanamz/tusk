package mcp_test

import (
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/mcp"
)

func TestDiffManifests_IdenticalReturnsEmpty(test *testing.T) {
	old := &manifest.Manifest{
		NodeTypes: make(map[string]manifest.NodeType),
		EdgeTypes: make(map[string]manifest.EdgeType),
		Behaviors: make(map[string]map[string]toml.Primitive),
	}
	fresh := &manifest.Manifest{
		NodeTypes: make(map[string]manifest.NodeType),
		EdgeTypes: make(map[string]manifest.EdgeType),
		Behaviors: make(map[string]map[string]toml.Primitive),
	}

	diff := mcp.DiffManifests(old, fresh)

	if len(diff.NodeTypes.Added) != 0 {
		test.Errorf("expected 0 added NodeTypes, got %d", len(diff.NodeTypes.Added))
	}
	if len(diff.NodeTypes.Removed) != 0 {
		test.Errorf("expected 0 removed NodeTypes, got %d", len(diff.NodeTypes.Removed))
	}
	if len(diff.EdgeTypes.Added) != 0 {
		test.Errorf("expected 0 added EdgeTypes, got %d", len(diff.EdgeTypes.Added))
	}
	if len(diff.EdgeTypes.Removed) != 0 {
		test.Errorf("expected 0 removed EdgeTypes, got %d", len(diff.EdgeTypes.Removed))
	}
	if len(diff.Behaviors.Added) != 0 {
		test.Errorf("expected 0 added Behaviors, got %d", len(diff.Behaviors.Added))
	}
	if len(diff.Behaviors.Removed) != 0 {
		test.Errorf("expected 0 removed Behaviors, got %d", len(diff.Behaviors.Removed))
	}
}

func TestDiffManifests_NodeTypesAdded(test *testing.T) {
	old := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{"task": {}},
		EdgeTypes: make(map[string]manifest.EdgeType),
		Behaviors: make(map[string]map[string]toml.Primitive),
	}
	fresh := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{"task": {}, "decision": {}},
		EdgeTypes: make(map[string]manifest.EdgeType),
		Behaviors: make(map[string]map[string]toml.Primitive),
	}

	diff := mcp.DiffManifests(old, fresh)

	if len(diff.NodeTypes.Added) != 1 || diff.NodeTypes.Added[0] != "decision" {
		test.Errorf("expected 1 added NodeType 'decision', got %v", diff.NodeTypes.Added)
	}
	if len(diff.NodeTypes.Removed) != 0 {
		test.Errorf("expected 0 removed NodeTypes, got %d", len(diff.NodeTypes.Removed))
	}
}

func TestDiffManifests_NodeTypesRemoved(test *testing.T) {
	old := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{"task": {}, "decision": {}},
		EdgeTypes: make(map[string]manifest.EdgeType),
		Behaviors: make(map[string]map[string]toml.Primitive),
	}
	fresh := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{"task": {}},
		EdgeTypes: make(map[string]manifest.EdgeType),
		Behaviors: make(map[string]map[string]toml.Primitive),
	}

	diff := mcp.DiffManifests(old, fresh)

	if len(diff.NodeTypes.Added) != 0 {
		test.Errorf("expected 0 added NodeTypes, got %d", len(diff.NodeTypes.Added))
	}
	if len(diff.NodeTypes.Removed) != 1 || diff.NodeTypes.Removed[0] != "decision" {
		test.Errorf("expected 1 removed NodeType 'decision', got %v", diff.NodeTypes.Removed)
	}
}

func TestDiffManifests_EdgeTypesAdded(test *testing.T) {
	old := &manifest.Manifest{
		NodeTypes: make(map[string]manifest.NodeType),
		EdgeTypes: make(map[string]manifest.EdgeType),
		Behaviors: make(map[string]map[string]toml.Primitive),
	}
	fresh := &manifest.Manifest{
		NodeTypes: make(map[string]manifest.NodeType),
		EdgeTypes: map[string]manifest.EdgeType{"depends": {}},
		Behaviors: make(map[string]map[string]toml.Primitive),
	}

	diff := mcp.DiffManifests(old, fresh)

	if len(diff.EdgeTypes.Added) != 1 || diff.EdgeTypes.Added[0] != "depends" {
		test.Errorf("expected 1 added EdgeType 'depends', got %v", diff.EdgeTypes.Added)
	}
	if len(diff.EdgeTypes.Removed) != 0 {
		test.Errorf("expected 0 removed EdgeTypes, got %d", len(diff.EdgeTypes.Removed))
	}
}

func TestDiffManifests_EdgeTypesRemoved(test *testing.T) {
	old := &manifest.Manifest{
		NodeTypes: make(map[string]manifest.NodeType),
		EdgeTypes: map[string]manifest.EdgeType{"depends": {}, "blocks": {}},
		Behaviors: make(map[string]map[string]toml.Primitive),
	}
	fresh := &manifest.Manifest{
		NodeTypes: make(map[string]manifest.NodeType),
		EdgeTypes: make(map[string]manifest.EdgeType),
		Behaviors: make(map[string]map[string]toml.Primitive),
	}

	diff := mcp.DiffManifests(old, fresh)

	if len(diff.EdgeTypes.Added) != 0 {
		test.Errorf("expected 0 added EdgeTypes, got %d", len(diff.EdgeTypes.Added))
	}
	if len(diff.EdgeTypes.Removed) != 2 {
		test.Errorf("expected 2 removed EdgeTypes, got %d", len(diff.EdgeTypes.Removed))
	}
}

func TestDiffManifests_BehaviorsAdded(test *testing.T) {
	old := &manifest.Manifest{
		NodeTypes: make(map[string]manifest.NodeType),
		EdgeTypes: make(map[string]manifest.EdgeType),
		Behaviors: make(map[string]map[string]toml.Primitive),
	}
	fresh := &manifest.Manifest{
		NodeTypes: make(map[string]manifest.NodeType),
		EdgeTypes: make(map[string]manifest.EdgeType),
		Behaviors: map[string]map[string]toml.Primitive{
			"workflow": {"review": toml.Primitive{}},
		},
	}

	diff := mcp.DiffManifests(old, fresh)

	if len(diff.Behaviors.Added) != 1 {
		test.Errorf("expected 1 added Behavior, got %d", len(diff.Behaviors.Added))
	}
	if len(diff.Behaviors.Added) > 0 {
		if diff.Behaviors.Added[0].Kind != "workflow" || diff.Behaviors.Added[0].Instance != "review" {
			test.Errorf("expected BehaviorRef{Kind: 'workflow', Instance: 'review'}, got %+v", diff.Behaviors.Added[0])
		}
	}
	if len(diff.Behaviors.Removed) != 0 {
		test.Errorf("expected 0 removed Behaviors, got %d", len(diff.Behaviors.Removed))
	}
}

func TestDiffManifests_BehaviorsRemoved(test *testing.T) {
	old := &manifest.Manifest{
		NodeTypes: make(map[string]manifest.NodeType),
		EdgeTypes: make(map[string]manifest.EdgeType),
		Behaviors: map[string]map[string]toml.Primitive{
			"workflow": {"review": toml.Primitive{}, "approve": toml.Primitive{}},
		},
	}
	fresh := &manifest.Manifest{
		NodeTypes: make(map[string]manifest.NodeType),
		EdgeTypes: make(map[string]manifest.EdgeType),
		Behaviors: make(map[string]map[string]toml.Primitive),
	}

	diff := mcp.DiffManifests(old, fresh)

	if len(diff.Behaviors.Added) != 0 {
		test.Errorf("expected 0 added Behaviors, got %d", len(diff.Behaviors.Added))
	}
	if len(diff.Behaviors.Removed) != 2 {
		test.Errorf("expected 2 removed Behaviors, got %d", len(diff.Behaviors.Removed))
	}
}

func TestDiffManifests_MixedChanges(test *testing.T) {
	old := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{"task": {}},
		EdgeTypes: map[string]manifest.EdgeType{"depends": {}},
		Behaviors: map[string]map[string]toml.Primitive{
			"workflow": {"review": toml.Primitive{}},
		},
	}
	fresh := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{"task": {}, "decision": {}},
		EdgeTypes: map[string]manifest.EdgeType{"blocks": {}},
		Behaviors: map[string]map[string]toml.Primitive{
			"workflow": {"review": toml.Primitive{}, "approve": toml.Primitive{}},
		},
	}

	diff := mcp.DiffManifests(old, fresh)

	if len(diff.NodeTypes.Added) != 1 || diff.NodeTypes.Added[0] != "decision" {
		test.Errorf("expected 1 added NodeType 'decision', got %v", diff.NodeTypes.Added)
	}
	if len(diff.EdgeTypes.Added) != 1 || diff.EdgeTypes.Added[0] != "blocks" {
		test.Errorf("expected 1 added EdgeType 'blocks', got %v", diff.EdgeTypes.Added)
	}
	if len(diff.EdgeTypes.Removed) != 1 || diff.EdgeTypes.Removed[0] != "depends" {
		test.Errorf("expected 1 removed EdgeType 'depends', got %v", diff.EdgeTypes.Removed)
	}
	if len(diff.Behaviors.Added) != 1 {
		test.Errorf("expected 1 added Behavior, got %d", len(diff.Behaviors.Added))
	}
	if len(diff.Behaviors.Added) > 0 && (diff.Behaviors.Added[0].Kind != "workflow" || diff.Behaviors.Added[0].Instance != "approve") {
		test.Errorf("expected BehaviorRef{Kind: 'workflow', Instance: 'approve'}, got %+v", diff.Behaviors.Added[0])
	}
	if len(diff.Behaviors.Removed) != 0 {
		test.Errorf("expected 0 removed Behaviors, got %d", len(diff.Behaviors.Removed))
	}
}
