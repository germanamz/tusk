package manifest

import (
	"errors"
	"fmt"

	"github.com/BurntSushi/toml"
)

// GraphCluster captures the resolved [graph.cluster] configuration block.
// Later phases extend the struct with Edge, Depth, Huddle, CommunityEdges,
// and Resolution — leave space by keeping scalar fields only for Phase 2.
type GraphCluster struct {
	// By names the active group producer. Default is "type" (today's
	// behavior). Phase 2 accepts "type" and "property"; later phases widen
	// the accepted set.
	By string

	// Property is the frontmatter field whose value becomes the group key.
	// Required when By == "property"; ignored otherwise.
	Property string
}

// DefaultGraphCluster returns the spec-mandated defaults. Callers can copy
// the result and mutate fields to construct effective configurations.
func DefaultGraphCluster() GraphCluster {
	return GraphCluster{By: "type"}
}

// Validate enforces the hard rules for GraphCluster. It accumulates every
// rule violation so callers (doctor, loader) can report the full set.
//
// Phase 2 accepts only "type" and "property". Phases 3 and 6 widen the
// accepted set; using an unsupported value here produces a clear error
// instead of being silently accepted then ignored.
func (cluster GraphCluster) Validate() []error {
	var errs []error

	switch cluster.By {
	case "type", "property":
		// valid Phase 2 producers
	default:
		errs = append(errs, fmt.Errorf("graph.cluster: by must be one of type, property (got %q)", cluster.By))
	}

	if cluster.By == "property" && cluster.Property == "" {
		errs = append(errs, fmt.Errorf("graph.cluster: property must be non-empty when by = \"property\""))
	}

	return errs
}

// graphClusterTOML is the on-disk shape decoded from [graph.cluster].
// Each scalar field uses a toml.Primitive so the resolver can distinguish
// "absent" (fill default) from "explicit zero" (use the literal value).
type graphClusterTOML struct {
	By       toml.Primitive `toml:"by"`
	Property toml.Primitive `toml:"property"`
}

// graphSection wraps [graph] and exposes the [graph.cluster] subtable.
// Kept package-private; consumers read Manifest.GraphCluster instead.
type graphSection struct {
	Cluster graphClusterTOML `toml:"cluster"`
}

// graphDecode is the BurntSushi/toml wrapper used by the loader to decode
// the [graph] section into a graphClusterTOML primitive table. Defined here
// so the loader can hand the decoded subtable to resolveGraphCluster.
type graphDecode struct {
	Graph graphSection `toml:"graph"`
}

// resolveGraphCluster finalises Manifest.GraphCluster after the primary
// decode. It populates defaults for absent or partially-set fields and
// returns a hard error when any rule in Validate is broken.
//
// Idempotent: callers can re-run it on a Manifest in steady state.
func resolveGraphCluster(loaded *Manifest) error {
	resolved := DefaultGraphCluster()

	meta := loaded.graphClusterMeta
	raw := loaded.graphCluster

	if meta != nil {
		if meta.IsDefined("graph", "cluster", "by") {
			if decodeErr := meta.PrimitiveDecode(raw.By, &resolved.By); decodeErr != nil {
				return fmt.Errorf("graph.cluster: by: %w", decodeErr)
			}
		}

		if meta.IsDefined("graph", "cluster", "property") {
			if decodeErr := meta.PrimitiveDecode(raw.Property, &resolved.Property); decodeErr != nil {
				return fmt.Errorf("graph.cluster: property: %w", decodeErr)
			}
		}
	}

	if errs := resolved.Validate(); len(errs) > 0 {
		// Validate accumulates rule violations; surface every one so users
		// see all problems in a single load attempt rather than fixing them
		// one round-trip at a time.
		return errors.Join(errs...)
	}

	loaded.GraphCluster = resolved
	loaded.graphCluster = graphClusterTOML{} // release primitives.
	loaded.graphClusterMeta = nil

	return nil
}
