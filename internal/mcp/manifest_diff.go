package mcp

import "github.com/germanamz/tusk/internal/manifest"

// TypeDelta reports the added and removed type names (for NodeTypes or EdgeTypes).
type TypeDelta struct {
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
}

// BehaviorRef identifies a single behavior instance by its kind and instance name.
type BehaviorRef struct {
	Kind     string `json:"kind"`
	Instance string `json:"instance"`
}

// BehaviorDelta reports the added and removed behavior instances.
type BehaviorDelta struct {
	Added   []BehaviorRef `json:"added"`
	Removed []BehaviorRef `json:"removed"`
}

// ManifestDiff captures the schema changes between an old and new manifest.
type ManifestDiff struct {
	NodeTypes TypeDelta     `json:"node_types"`
	EdgeTypes TypeDelta     `json:"edge_types"`
	Behaviors BehaviorDelta `json:"behaviors"`
}

// DiffManifests compares old and new manifests and returns a ManifestDiff capturing
// the added/removed node-types, edge-types, and behaviors (two-level: kind to instance).
// Flat lists (no pagination); order matches manifest map iteration.
func DiffManifests(old, fresh *manifest.Manifest) ManifestDiff {
	diff := ManifestDiff{
		NodeTypes: TypeDelta{Added: []string{}, Removed: []string{}},
		EdgeTypes: TypeDelta{Added: []string{}, Removed: []string{}},
		Behaviors: BehaviorDelta{Added: []BehaviorRef{}, Removed: []BehaviorRef{}},
	}

	for typeName := range fresh.NodeTypes {
		if _, exists := old.NodeTypes[typeName]; !exists {
			diff.NodeTypes.Added = append(diff.NodeTypes.Added, typeName)
		}
	}
	for typeName := range old.NodeTypes {
		if _, exists := fresh.NodeTypes[typeName]; !exists {
			diff.NodeTypes.Removed = append(diff.NodeTypes.Removed, typeName)
		}
	}

	for typeName := range fresh.EdgeTypes {
		if _, exists := old.EdgeTypes[typeName]; !exists {
			diff.EdgeTypes.Added = append(diff.EdgeTypes.Added, typeName)
		}
	}
	for typeName := range old.EdgeTypes {
		if _, exists := fresh.EdgeTypes[typeName]; !exists {
			diff.EdgeTypes.Removed = append(diff.EdgeTypes.Removed, typeName)
		}
	}

	for kindName, freshInstances := range fresh.Behaviors {
		oldInstances := old.Behaviors[kindName]
		for instanceName := range freshInstances {
			if _, exists := oldInstances[instanceName]; !exists {
				diff.Behaviors.Added = append(diff.Behaviors.Added, BehaviorRef{Kind: kindName, Instance: instanceName})
			}
		}
	}
	for kindName, oldInstances := range old.Behaviors {
		freshInstances := fresh.Behaviors[kindName]
		for instanceName := range oldInstances {
			if _, exists := freshInstances[instanceName]; !exists {
				diff.Behaviors.Removed = append(diff.Behaviors.Removed, BehaviorRef{Kind: kindName, Instance: instanceName})
			}
		}
	}

	return diff
}
