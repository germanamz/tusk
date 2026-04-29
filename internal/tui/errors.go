package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/germanamz/tusk/domain"
)

// formatTaxonomyError translates a *domain.TaxonomyError into the user-facing
// CLI message defined in the Phase 5 design spec (§ 12). Returns (message, true)
// when err wraps a TaxonomyError; otherwise ("", false) so callers can fall
// through to the generic error path. projectName may be empty — in that case
// the message uses "project" as a placeholder.
func formatTaxonomyError(err error, projectName string) (string, bool) {
	var taxonomyErr *domain.TaxonomyError
	if !errors.As(err, &taxonomyErr) {
		return "", false
	}
	pName := projectName
	if pName == "" {
		pName = "project"
	}
	switch taxonomyErr.Reason {
	case "missing":
		peers := topRankPeers(taxonomyErr.Taxonomy)
		return fmt.Sprintf(
			"project %s requires a level; supply level=%s (or any rank on modify)",
			pName, peers,
		), true
	case "unknown_level":
		inline := FormatTaxonomyInline(taxonomyErr.Taxonomy)
		return fmt.Sprintf(
			"level %s is not in the taxonomy for %s: %s",
			taxonomyErr.Level, pName, inline,
		), true
	case "root_requires_top_rank":
		peers := topRankPeers(taxonomyErr.Taxonomy)
		return fmt.Sprintf(
			"root tasks must use the top-rank level (%s); got %s",
			peers, taxonomyErr.Level,
		), true
	case "parent_rank_not_lower":
		return fmt.Sprintf(
			"%s cannot sit under %s — parent rank must be strictly lower",
			taxonomyErr.Level, taxonomyErr.ParentLevel,
		), true
	}
	return taxonomyErr.Error(), true
}

// topRankPeers returns a comma-separated list of the top-rank peer level
// names for a taxonomy, used in the `missing` and `root_requires_top_rank`
// CLI messages.
func topRankPeers(taxonomy domain.Taxonomy) string {
	if len(taxonomy) == 0 || len(taxonomy[0]) == 0 {
		return ""
	}
	return strings.Join(taxonomy[0], ",")
}
