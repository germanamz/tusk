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

// GraphExpansionLabels names the caller's knobs in validation messages, so the
// CLI can say "--hops" where MCP says "hops:". An empty field falls back to the
// bare key name.
type GraphExpansionLabels struct {
	Hops   string
	Weight string
}

// hopsLabel returns the caller's name for the hops knob, defaulting to the bare
// key when the caller left it unset.
func (labels GraphExpansionLabels) hopsLabel() string {
	if labels.Hops == "" {
		return "hops"
	}

	return labels.Hops
}

// weightLabel returns the caller's name for the weight knob, defaulting to the
// bare key when the caller left it unset.
func (labels GraphExpansionLabels) weightLabel() string {
	if labels.Weight == "" {
		return "weight"
	}

	return labels.Weight
}

// GraphExpansionOverrides carries per-call overrides for MergeGraphExpansion.
// A nil pointer means "not specified — inherit the base". This is what makes an
// explicit hops=0 / weight=0 distinguishable from an absent one; callers do
// their own presence detection (raw-args lookup, cobra Changed, query-param
// presence) and hand over an already-resolved optional.
type GraphExpansionOverrides struct {
	Enabled   *bool
	Hops      *int
	Weight    *float64
	EdgeTypes *[]string

	Labels GraphExpansionLabels
}

// cloneEdgeTypes returns a fresh backing array for edges. A len-0 input yields a
// non-nil empty slice, which is a legitimate "no edge types" value rather than
// an absent one.
func cloneEdgeTypes(edges []string) []string {
	cloned := make([]string, len(edges))
	copy(cloned, edges)

	return cloned
}

// MergeGraphExpansion folds per-call overrides onto a workspace base. The
// returned pointer is never nil on success. EdgeTypes is always cloned: a bare
// struct copy aliases the base's slice header, and the MCP server fans requests
// out to goroutines sharing the runtime manifest.
//
// It deliberately does NOT call Validate. The per-field guards below enforce the
// same hops/weight rules on the only values a caller can override, using the
// caller's own wording. Validating the whole struct would reject a hand-built
// (zero-value) base that callers legitimately merge onto today.
func MergeGraphExpansion(base GraphExpansion, over GraphExpansionOverrides) (*GraphExpansion, error) {
	resolved := base

	// Struct copy aliases the EdgeTypes slice header; clone the backing array so
	// per-call mutations cannot leak into the shared manifest configuration.
	if len(base.EdgeTypes) > 0 {
		resolved.EdgeTypes = cloneEdgeTypes(base.EdgeTypes)
	}

	if over.Enabled != nil {
		resolved.Enabled = *over.Enabled
	}

	// Hops is validated before Weight: a call passing both out of range must
	// surface the hops error, as both callers did before this was shared.
	if over.Hops != nil {
		if *over.Hops != 1 && *over.Hops != 2 {
			return nil, fmt.Errorf("%s must be 1 or 2 (got %d)", over.Labels.hopsLabel(), *over.Hops)
		}

		resolved.Hops = *over.Hops
	}

	if over.Weight != nil {
		if *over.Weight < 0 || *over.Weight > 1 {
			return nil, fmt.Errorf("%s must be in [0.0, 1.0] (got %v)", over.Labels.weightLabel(), *over.Weight)
		}

		resolved.Weight = *over.Weight
	}

	if over.EdgeTypes != nil {
		resolved.EdgeTypes = cloneEdgeTypes(*over.EdgeTypes)
	}

	return &resolved, nil
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

// decodeGraphExpansionField decodes a single [query.graph-expansion] scalar key
// into dst when it is present in the manifest, wrapping a decode failure with
// the key name. It captures the four scalar branches verbatim; the slice-valued
// edge-types key keeps its decode-into-local-then-assign shape and is not routed
// through here.
func decodeGraphExpansionField[T any](meta *toml.MetaData, prim toml.Primitive, dst *T, key string) error {
	if !meta.IsDefined("query", "graph-expansion", key) {
		return nil
	}

	if decodeErr := meta.PrimitiveDecode(prim, dst); decodeErr != nil {
		return fmt.Errorf("query.graph-expansion: %s: %w", key, decodeErr)
	}

	return nil
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
		if decodeErr := decodeGraphExpansionField(meta, raw.Enabled, &resolved.Enabled, "enabled"); decodeErr != nil {
			return decodeErr
		}

		if decodeErr := decodeGraphExpansionField(meta, raw.Hops, &resolved.Hops, "hops"); decodeErr != nil {
			return decodeErr
		}

		// edge-types decodes into a local and assigns only on success, so it
		// keeps its own shape rather than routing through the scalar helper.
		if meta.IsDefined("query", "graph-expansion", "edge-types") {
			var edges []string

			if decodeErr := meta.PrimitiveDecode(raw.EdgeTypes, &edges); decodeErr != nil {
				return fmt.Errorf("query.graph-expansion: edge-types: %w", decodeErr)
			}

			resolved.EdgeTypes = edges
		}

		if decodeErr := decodeGraphExpansionField(meta, raw.Weight, &resolved.Weight, "weight"); decodeErr != nil {
			return decodeErr
		}

		if decodeErr := decodeGraphExpansionField(meta, raw.CandidateMultiplier, &resolved.CandidateMultiplier, "candidate-multiplier"); decodeErr != nil {
			return decodeErr
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
