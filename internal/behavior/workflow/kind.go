package workflow

import (
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"

	"github.com/germanamz/tusk/internal/behavior"
)

// Kind is the workflow pack constructor. v1 registers exactly one
// behavior.Kind with the registry: workflow.Kind{}.
type Kind struct{}

// Name satisfies behavior.Kind.
func (Kind) Name() string { return "workflow" }

// NewInstance decodes raw into a workflowConfig, validates the schema,
// and produces a runtime *instance. Schema errors carry the (kind,
// instance) qualifier in their message via behavior.Registry.BuildEngine.
func (kind Kind) NewInstance(instanceName string, raw toml.Primitive, meta *toml.MetaData) (behavior.Instance, error) {
	if instanceName == "" {
		return nil, errors.New("instance name: empty")
	}

	if meta == nil {
		return nil, errors.New("manifest meta: nil — workflow pack requires a TOML-loaded manifest")
	}

	var cfg workflowConfig

	if decodeErr := meta.PrimitiveDecode(raw, &cfg); decodeErr != nil {
		return nil, fmt.Errorf("decode: %w", decodeErr)
	}

	cfg.normalize()

	if validateErr := cfg.validate(); validateErr != nil {
		return nil, validateErr
	}

	if cfg.AutoCompleteParent || cfg.AutoRevertParent {
		fmt.Fprintln(os.Stderr, "workflow: auto-* directives accepted but not yet active")
	}

	return newInstance(instanceName, cfg), nil
}
