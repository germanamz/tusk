package behavior

import (
	"fmt"

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
	if loaded == nil {
		return NewEngine(nil)
	}

	var instances []Instance

	for kindName, perInstance := range loaded.Behaviors {
		kind, found := registry.Lookup(kindName)

		if !found {
			return nil, fmt.Errorf("behavior: manifest references unknown kind %q (registered: %v)", kindName, registry.knownKinds())
		}

		for instanceName, raw := range perInstance {
			instance, newErr := kind.NewInstance(instanceName, raw, loaded.Meta)

			if newErr != nil {
				return nil, fmt.Errorf("behavior: %s.%s: %w", kindName, instanceName, newErr)
			}

			instances = append(instances, instance)
		}
	}

	return NewEngine(instances)
}

func (registry *Registry) knownKinds() []string {
	names := make([]string, 0, len(registry.kinds))

	for name := range registry.kinds {
		names = append(names, name)
	}

	return names
}
