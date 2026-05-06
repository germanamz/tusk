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

	engine, buildErr := registry.BuildEngine(loaded)

	if buildErr != nil {
		return nil, buildErr
	}

	return engine, nil
}
