package manifest

import (
	"errors"
	"fmt"

	"github.com/BurntSushi/toml"
)

// GraphExpansion captures the resolved [query.graph-expansion] configuration
// block. Subsequent tasks (2-4) of Phase 3 read this struct from the query
// service; Task 1 only plumbs it through the loader and per-call overrides.
type GraphExpansion struct {
	// Enabled toggles graph-expanded retrieval. Defaults to false so existing
	// workspaces keep their current ranking behavior until they opt in.
	Enabled bool

	// Hops is the BFS depth applied when expansion is enabled. Permitted
	// values are {1, 2}. The loader rejects anything else.
	Hops int

	// EdgeTypes lists the edge-type names the expander follows. Unknown
	// names are not a load error — doctor warns about them in Task 4.
	EdgeTypes []string

	// Weight is the per-hop attenuation applied to neighbor scores; must lie
	// in [0.0, 1.0].
	Weight float64

	// CandidateMultiplier caps the number of structural candidates the
	// expander considers (effective take * multiplier). Must be >= 1.
	CandidateMultiplier int
}

// DefaultGraphExpansion returns the spec-mandated defaults. Callers can copy
// the result and mutate fields to construct effective configurations.
func DefaultGraphExpansion() GraphExpansion {
	return GraphExpansion{
		Enabled:             false,
		Hops:                1,
		EdgeTypes:           defaultGraphExpansionEdgeTypes(),
		Weight:              0.2,
		CandidateMultiplier: 5,
	}
}

// defaultGraphExpansionEdgeTypes returns a freshly allocated slice of the
// default edge-type names. Allocating per call so callers can mutate
// without contaminating the shared default.
func defaultGraphExpansionEdgeTypes() []string {
	return []string{"references", "parent", "tagged", "contains"}
}

// Validate enforces the hard rules for GraphExpansion. It accumulates every
// rule violation so callers (doctor, loader) can report the full set.
func (config GraphExpansion) Validate() []error {
	var errs []error

	if config.Hops != 1 && config.Hops != 2 {
		errs = append(errs, fmt.Errorf("query.graph-expansion: hops must be 1 or 2 (got %d)", config.Hops))
	}

	if config.Weight < 0 || config.Weight > 1 {
		errs = append(errs, fmt.Errorf("query.graph-expansion: weight must be in [0.0, 1.0] (got %v)", config.Weight))
	}

	if config.CandidateMultiplier < 1 {
		errs = append(errs, fmt.Errorf("query.graph-expansion: candidate-multiplier must be >= 1 (got %d)", config.CandidateMultiplier))
	}

	return errs
}

// graphExpansionTOML is the on-disk shape decoded from [query.graph-expansion].
// Each scalar field uses a toml.Primitive so the resolver can distinguish
// "absent" (fill default) from "explicit zero" (use the literal value).
type graphExpansionTOML struct {
	Enabled             toml.Primitive `toml:"enabled"`
	Hops                toml.Primitive `toml:"hops"`
	EdgeTypes           toml.Primitive `toml:"edge-types"`
	Weight              toml.Primitive `toml:"weight"`
	CandidateMultiplier toml.Primitive `toml:"candidate-multiplier"`
}

// querySection wraps [query] and exposes the [query.graph-expansion] subtable.
// Kept package-private; consumers read Manifest.GraphExpansion instead.
type querySection struct {
	GraphExpansion graphExpansionTOML `toml:"graph-expansion"`
}

// resolveGraphExpansion finalises Manifest.GraphExpansion after the primary
// decode. It populates defaults for absent or partially-set fields and
// returns a hard error when any rule in Validate is broken.
//
// Idempotent: callers can re-run it on a Manifest in steady state.
func resolveGraphExpansion(loaded *Manifest) error {
	resolved := DefaultGraphExpansion()

	meta := loaded.queryGraphExpansionMeta
	raw := loaded.queryGraphExpansion

	if meta != nil {
		if meta.IsDefined("query", "graph-expansion", "enabled") {
			if decodeErr := meta.PrimitiveDecode(raw.Enabled, &resolved.Enabled); decodeErr != nil {
				return fmt.Errorf("query.graph-expansion: enabled: %w", decodeErr)
			}
		}

		if meta.IsDefined("query", "graph-expansion", "hops") {
			if decodeErr := meta.PrimitiveDecode(raw.Hops, &resolved.Hops); decodeErr != nil {
				return fmt.Errorf("query.graph-expansion: hops: %w", decodeErr)
			}
		}

		if meta.IsDefined("query", "graph-expansion", "edge-types") {
			var edges []string

			if decodeErr := meta.PrimitiveDecode(raw.EdgeTypes, &edges); decodeErr != nil {
				return fmt.Errorf("query.graph-expansion: edge-types: %w", decodeErr)
			}

			resolved.EdgeTypes = edges
		}

		if meta.IsDefined("query", "graph-expansion", "weight") {
			if decodeErr := meta.PrimitiveDecode(raw.Weight, &resolved.Weight); decodeErr != nil {
				return fmt.Errorf("query.graph-expansion: weight: %w", decodeErr)
			}
		}

		if meta.IsDefined("query", "graph-expansion", "candidate-multiplier") {
			if decodeErr := meta.PrimitiveDecode(raw.CandidateMultiplier, &resolved.CandidateMultiplier); decodeErr != nil {
				return fmt.Errorf("query.graph-expansion: candidate-multiplier: %w", decodeErr)
			}
		}
	}

	if errs := resolved.Validate(); len(errs) > 0 {
		// Validate accumulates rule violations; surface every one so users
		// see all problems in a single load attempt rather than fixing them
		// one round-trip at a time.
		return errors.Join(errs...)
	}

	loaded.GraphExpansion = resolved
	loaded.queryGraphExpansion = graphExpansionTOML{} // release primitives.
	loaded.queryGraphExpansionMeta = nil

	return nil
}
