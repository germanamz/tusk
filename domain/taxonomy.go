package domain

import (
	"fmt"
	"regexp"
	"slices"
)

// Taxonomy is an ordered list of rank groups. Index 0 is the top rank and
// the only rank whose members may appear as root tasks. Each inner slice is
// a peer set of level names sharing that rank.
type Taxonomy [][]string

// levelNamePattern matches valid level names: starts with letter or underscore,
// followed by alphanumerics, hyphens, or underscores. Mirrors udaKeyPattern
// to keep the naming rules consistent without coupling the two.
var levelNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*$`)

// IsEmpty reports whether the taxonomy has no ranks (levels disabled).
func (t Taxonomy) IsEmpty() bool {
	return len(t) == 0
}

// Contains reports whether level appears anywhere in the taxonomy.
func (t Taxonomy) Contains(level string) bool {
	_, ok := t.RankOf(level)
	return ok
}

// RankOf returns the rank index for level and true, or 0/false when level
// is not declared.
func (t Taxonomy) RankOf(level string) (int, bool) {
	for i, peers := range t {
		if slices.Contains(peers, level) {
			return i, true
		}
	}
	return 0, false
}

// IsTopRank reports whether level sits at rank 0.
func (t Taxonomy) IsTopRank(level string) bool {
	rank, ok := t.RankOf(level)
	return ok && rank == 0
}

// Clone returns a deep copy so callers can safely mutate the result.
func (t Taxonomy) Clone() Taxonomy {
	if t == nil {
		return nil
	}
	out := make(Taxonomy, len(t))
	for i, peers := range t {
		dup := make([]string, len(peers))
		copy(dup, peers)
		out[i] = dup
	}
	return out
}

// Validate rejects malformed taxonomies:
//   - zero ranks
//   - an empty peer group
//   - a level name that doesn't match [a-zA-Z_][a-zA-Z0-9_-]*
//   - a duplicate level name anywhere in the taxonomy
func (t Taxonomy) Validate() error {
	if len(t) == 0 {
		return fmt.Errorf("taxonomy must declare at least one rank")
	}
	seen := make(map[string]struct{})
	for rankIdx, peers := range t {
		if len(peers) == 0 {
			return fmt.Errorf("taxonomy rank %d has no levels", rankIdx)
		}
		for _, name := range peers {
			if !levelNamePattern.MatchString(name) {
				return fmt.Errorf("invalid level name %q: must match %s", name, levelNamePattern.String())
			}
			if _, dup := seen[name]; dup {
				return fmt.Errorf("duplicate level name %q in taxonomy", name)
			}
			seen[name] = struct{}{}
		}
	}
	return nil
}
