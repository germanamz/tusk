package manifest

import (
	"errors"
	"fmt"

	"github.com/BurntSushi/toml"
)

// GraphCluster captures the resolved [graph.cluster] configuration block.
// Later phases extend the struct with Huddle, CommunityEdges, and Resolution.
type GraphCluster struct {
	// By names the active group producer. Default is "type" (today's
	// behavior). Phase 2 accepts "type" and "property"; Phase 3 adds
	// "ancestor"; Phase 6 adds "community".
	By string

	// Property is the frontmatter field whose value becomes the group key.
	// Required when By == "property"; ignored otherwise.
	Property string

	// Edge is the hierarchy edge-type name to walk when By == "ancestor".
	// Default: edge TARGET is the parent (child→parent edges such as
	// "parent"); set ParentIsSource = true for parent→child edges instead.
	// Required when By == "ancestor".
	Edge string

	// Depth is the ancestor depth for the walk when By == "ancestor".
	// 0 (and any negative value) walks to the topmost reachable ancestor
	// (root). Negative values are treated as 0 by the walker; no validation
	// error is produced for negative Depth.
	Depth int

	// ParentIsSource controls edge-direction during the ancestor walk.
	// false (default): the edge TARGET is the parent (child→parent edges,
	// e.g. the built-in "parent" ref property where Source=child,
	// Target=parent). Set to true only for parent→child edges where the
	// Source is the parent (e.g. "contains" / "children" style edges).
	ParentIsSource bool

	// Huddle engages the layout cluster force (Phase 4). When true,
	// same-group nodes are pulled toward a fixed per-group anchor on a
	// Fibonacci sphere; forceCollide keeps them from overlapping; the
	// default charge is softened so the group pull dominates. Default false.
	Huddle bool
}

// DefaultGraphCluster returns the spec-mandated defaults. Callers can copy
// the result and mutate fields to construct effective configurations.
func DefaultGraphCluster() GraphCluster {
	return GraphCluster{By: "type"}
}

// Validate enforces the hard rules for GraphCluster. It accumulates every
// rule violation so callers (doctor, loader) can report the full set.
//
// Phase 2 accepts "type" and "property"; Phase 3 adds "ancestor". Using an
// unsupported value produces a clear error instead of being silently accepted
// then ignored. Phase 6 will add "community".
func (cluster GraphCluster) Validate() []error {
	var errs []error

	switch cluster.By {
	case "type", "property", "ancestor":
		// valid producers
	default:
		errs = append(errs, fmt.Errorf("graph.cluster: by must be one of type, property, ancestor (got %q)", cluster.By))
	}

	if cluster.By == "property" && cluster.Property == "" {
		errs = append(errs, fmt.Errorf("graph.cluster: property must be non-empty when by = \"property\""))
	}

	if cluster.By == "ancestor" && cluster.Edge == "" {
		errs = append(errs, fmt.Errorf("graph.cluster: by = \"ancestor\" requires a non-empty edge"))
	}

	return errs
}

// graphClusterTOML is the on-disk shape decoded from [graph.cluster].
// Each scalar field uses a toml.Primitive so the resolver can distinguish
// "absent" (fill default) from "explicit zero" (use the literal value).
type graphClusterTOML struct {
	By             toml.Primitive `toml:"by"`
	Property       toml.Primitive `toml:"property"`
	Edge           toml.Primitive `toml:"edge"`
	Depth          toml.Primitive `toml:"depth"`
	ParentIsSource toml.Primitive `toml:"parent-is-source"`
	Huddle         toml.Primitive `toml:"huddle"`
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

		if meta.IsDefined("graph", "cluster", "edge") {
			if decodeErr := meta.PrimitiveDecode(raw.Edge, &resolved.Edge); decodeErr != nil {
				return fmt.Errorf("graph.cluster: edge: %w", decodeErr)
			}
		}

		if meta.IsDefined("graph", "cluster", "depth") {
			if decodeErr := meta.PrimitiveDecode(raw.Depth, &resolved.Depth); decodeErr != nil {
				return fmt.Errorf("graph.cluster: depth: %w", decodeErr)
			}
		}

		if meta.IsDefined("graph", "cluster", "parent-is-source") {
			if decodeErr := meta.PrimitiveDecode(raw.ParentIsSource, &resolved.ParentIsSource); decodeErr != nil {
				return fmt.Errorf("graph.cluster: parent-is-source: %w", decodeErr)
			}
		}

		if meta.IsDefined("graph", "cluster", "huddle") {
			if decodeErr := meta.PrimitiveDecode(raw.Huddle, &resolved.Huddle); decodeErr != nil {
				return fmt.Errorf("graph.cluster: huddle: %w", decodeErr)
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
