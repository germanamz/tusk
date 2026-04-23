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
	var te *domain.TaxonomyError
	if !errors.As(err, &te) {
		return "", false
	}
	pName := projectName
	if pName == "" {
		pName = "project"
	}
	switch te.Reason {
	case "missing":
		peers := topRankPeers(te.Taxonomy)
		return fmt.Sprintf(
			"project %s requires a level; supply level=%s (or any rank on modify)",
			pName, peers,
		), true
	case "unknown_level":
		inline := FormatTaxonomyInline(te.Taxonomy)
		return fmt.Sprintf(
			"level %s is not in the taxonomy for %s: %s",
			te.Level, pName, inline,
		), true
	case "root_requires_top_rank":
		peers := topRankPeers(te.Taxonomy)
		return fmt.Sprintf(
			"root tasks must use the top-rank level (%s); got %s",
			peers, te.Level,
		), true
	case "parent_rank_not_lower":
		return fmt.Sprintf(
			"%s cannot sit under %s — parent rank must be strictly lower",
			te.Level, te.ParentLevel,
		), true
	}
	return te.Error(), true
}

// topRankPeers returns a comma-separated list of the top-rank peer level
// names for a taxonomy, used in the `missing` and `root_requires_top_rank`
// CLI messages.
func topRankPeers(t domain.Taxonomy) string {
	if len(t) == 0 || len(t[0]) == 0 {
		return ""
	}
	return strings.Join(t[0], ",")
}
