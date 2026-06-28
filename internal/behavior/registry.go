package behavior

import (
	"fmt"
	"sort"

	"github.com/germanamz/tusk/internal/manifest"
)

// Registry maps kind names to constructors. Populated explicitly — no
// init() side effects — so tests can build a Registry from scratch.
type Registry struct {
	kinds map[string]Kind
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{kinds: map[string]Kind{}}
}

// Register adds a Kind to the Registry. Duplicate names return an error.
func (registry *Registry) Register(kind Kind) error {
	name := kind.Name()

	if _, taken := registry.kinds[name]; taken {
		return fmt.Errorf("behavior: kind %q already registered", name)
	}

	registry.kinds[name] = kind

	return nil
}

// Lookup returns (kind, true) when name is registered.
func (registry *Registry) Lookup(name string) (Kind, bool) {
	kind, found := registry.kinds[name]

	return kind, found
}

// BuildEngine resolves loaded.Behaviors into Instances by calling each
// Kind.NewInstance, then constructs and returns an *Engine. Reservation
// collisions surface here via NewEngine.
func (registry *Registry) BuildEngine(loaded *manifest.Manifest) (*Engine, error) {
	return registry.BuildEngineWithDeclaredKeys(loaded, nil)
}

// BuildEngineWithDeclaredKeys is the production path: it resolves
// loaded.Behaviors into Instances and passes declaredKeys through to
// NewEngine so the collision detector covers node-type declarations.
func (registry *Registry) BuildEngineWithDeclaredKeys(loaded *manifest.Manifest, declaredKeys []DeclaredKey) (*Engine, error) {
	if loaded == nil {
		return NewEngine(nil, declaredKeys)
	}

	var instances []Instance

	// Resolve kinds and their instances in sorted order. Map iteration is
	// randomized, and FireNodeWriteValidate short-circuits on the first
	// rejection, so unsorted order made the reported Rejector (and error
	// message) flip across tusk reload / restart when two instances govern
	// the same type.
	for _, kindName := range sortedKeys(loaded.Behaviors) {
		perInstance := loaded.Behaviors[kindName]

		kind, found := registry.Lookup(kindName)

		if !found {
			return nil, fmt.Errorf("behavior: manifest references unknown kind %q (registered: %v)", kindName, registry.knownKinds())
		}

		for _, instanceName := range sortedKeys(perInstance) {
			instance, newErr := kind.NewInstance(instanceName, perInstance[instanceName], loaded.Meta)

			if newErr != nil {
				return nil, fmt.Errorf("behavior: %s.%s: %w", kindName, instanceName, newErr)
			}

			instances = append(instances, instance)
		}
	}

	return NewEngine(instances, declaredKeys)
}

// sortedKeys returns a map's string keys in ascending order, so behavior
// resolution is deterministic regardless of Go's randomized map iteration.
func sortedKeys[Value any](mapping map[string]Value) []string {
	keys := make([]string, 0, len(mapping))

	for key := range mapping {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

func (registry *Registry) knownKinds() []string {
	names := make([]string, 0, len(registry.kinds))

	for name := range registry.kinds {
		names = append(names, name)
	}

	return names
}
