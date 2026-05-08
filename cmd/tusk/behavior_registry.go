package main

import (
	"fmt"

	"github.com/germanamz/tusk/internal/behavior"
	"github.com/germanamz/tusk/internal/behavior/workflow"
	"github.com/germanamz/tusk/internal/manifest"
)

// newBehaviorEngine constructs a *behavior.Engine from loaded by
// registering every built-in pack kind and resolving the manifest's
// [behaviors] section. v1 registers exactly one kind: workflow.
func newBehaviorEngine(loaded *manifest.Manifest) (*behavior.Engine, error) {
	registry := behavior.NewRegistry()

	if registerErr := registry.Register(workflow.Kind{}); registerErr != nil {
		return nil, fmt.Errorf("behavior_registry: register workflow: %w", registerErr)
	}

	declaredKeys := declaredKeysFrom(loaded)

	engine, buildErr := registry.BuildEngineWithDeclaredKeys(loaded, declaredKeys)

	if buildErr != nil {
		return nil, buildErr
	}

	return engine, nil
}

// declaredKeysFrom converts the manifest's NodeTypes map into a slice of
// behavior.DeclaredKey so the collision detector can check for overlaps
// between declared properties and behavior-pack reservations.
func declaredKeysFrom(loaded *manifest.Manifest) []behavior.DeclaredKey {
	var keys []behavior.DeclaredKey

	for typeName, nt := range loaded.NodeTypes {
		for _, prop := range nt.Properties {
			keys = append(keys, behavior.DeclaredKey{
				NodeType: typeName,
				Property: prop.Name,
				Source:   fmt.Sprintf("node-types.%s.properties[%s]", typeName, prop.Name),
			})
		}
	}

	return keys
}
