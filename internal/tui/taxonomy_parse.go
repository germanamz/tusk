package tui

import (
	"fmt"
	"strings"

	"github.com/germanamz/tusk/domain"
)

// ParseTaxonomyInline parses "milestone:initiative:story:(task,spike)" into a
// domain.Taxonomy. Splits on ':' at top level (outside parens); a segment
// wrapped in "(...)" splits its body by ',' to form a peer group; a bare
// segment becomes a single-element group.
//
// Whitespace inside groups is trimmed. Empty input (after trimming) returns
// an empty domain.Taxonomy. Structural validation is delegated to
// domain.Taxonomy.Validate.
func ParseTaxonomyInline(s string) (domain.Taxonomy, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return domain.Taxonomy{}, nil
	}

	segments, err := splitTaxonomyRanks(trimmed)
	if err != nil {
		return nil, err
	}

	result := make(domain.Taxonomy, 0, len(segments))
	for _, seg := range segments {
		peers, err := parseRankSegment(seg)
		if err != nil {
			return nil, err
		}
		result = append(result, peers)
	}

	if err := result.Validate(); err != nil {
		return nil, err
	}
	return result, nil
}

// FormatTaxonomyInline renders a domain.Taxonomy as its inline form. Single-
// peer ranks emit the bare level name; multi-peer ranks emit "(a,b,c)".
// Ranks are joined by ':'. Returns "" for an empty taxonomy.
func FormatTaxonomyInline(t domain.Taxonomy) string {
	if len(t) == 0 {
		return ""
	}
	parts := make([]string, len(t))
	for i, peers := range t {
		if len(peers) == 1 {
			parts[i] = peers[0]
		} else {
			parts[i] = "(" + strings.Join(peers, ",") + ")"
		}
	}
	return strings.Join(parts, ":")
}

// splitTaxonomyRanks splits s on top-level ':' characters, respecting
// parenthesized groups. Unmatched parens return an error.
func splitTaxonomyRanks(s string) ([]string, error) {
	var segments []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return nil, fmt.Errorf("taxonomy: unmatched ')' at position %d", i)
			}
			depth--
		case ':':
			if depth == 0 {
				segments = append(segments, s[start:i])
				start = i + 1
			}
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("taxonomy: unmatched '(' in %q", s)
	}
	segments = append(segments, s[start:])
	return segments, nil
}

// parseRankSegment returns the peer names for a single rank segment. A bare
// segment yields a single-name peer group; a "(a,b,c)" segment yields
// multiple peers with whitespace trimmed from each name.
func parseRankSegment(seg string) ([]string, error) {
	trimmed := strings.TrimSpace(seg)
	if strings.HasPrefix(trimmed, "(") {
		if !strings.HasSuffix(trimmed, ")") {
			return nil, fmt.Errorf("taxonomy: expected ')' to close group in %q", seg)
		}
		body := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
		if body == "" {
			return nil, fmt.Errorf("taxonomy: empty group ()")
		}
		rawPeers := strings.Split(body, ",")
		peers := make([]string, 0, len(rawPeers))
		for _, p := range rawPeers {
			name := strings.TrimSpace(p)
			if name == "" {
				return nil, fmt.Errorf("taxonomy: empty peer name in group %q", seg)
			}
			peers = append(peers, name)
		}
		return peers, nil
	}
	if trimmed == "" {
		return nil, fmt.Errorf("taxonomy: empty rank segment")
	}
	// Bare segment must not contain any group delimiters.
	if strings.ContainsAny(trimmed, "(),") {
		return nil, fmt.Errorf("taxonomy: unexpected character in rank %q", seg)
	}
	return []string{trimmed}, nil
}
