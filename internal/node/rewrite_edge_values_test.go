package node

import (
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

// rewriteFrontmatterEdgeValues must handle every value shape the writer
// (renderMarkdown / setEdgeTargets) can emit: a scalar, an inline flow
// sequence, and — the case that previously corrupted multi-target edges on
// rename — a YAML block sequence.
func TestRewriteFrontmatterEdgeValues(test *testing.T) {
	edgeTypes := manifest.EdgeTypes{
		"parent": manifest.EdgeType{},
		"blocks": manifest.EdgeType{},
	}

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "scalar",
			input: "---\ntype: ticket\nparent: tickets/old\n---\n\nbody\n",
			want:  "---\ntype: ticket\nparent: tickets/new\n---\n\nbody\n",
		},
		{
			name:  "inline flow",
			input: "---\ntype: ticket\nblocks: [tickets/old, tickets/keep]\n---\n\nbody\n",
			want:  "---\ntype: ticket\nblocks: [tickets/new, tickets/keep]\n---\n\nbody\n",
		},
		{
			name:  "block sequence",
			input: "---\ntype: ticket\nblocks:\n  - tickets/old\n  - tickets/keep\n---\n\nbody\n",
			want:  "---\ntype: ticket\nblocks:\n  - tickets/new\n  - tickets/keep\n---\n\nbody\n",
		},
		{
			name:  "block sequence quoted element",
			input: "---\ntype: ticket\nblocks:\n  - \"tickets/old\"\n  - tickets/keep\n---\n\nbody\n",
			want:  "---\ntype: ticket\nblocks:\n  - tickets/new\n  - tickets/keep\n---\n\nbody\n",
		},
		{
			name:  "empty edge value is not a sequence",
			input: "---\ntype: ticket\nparent:\ntitle: keep me\n---\n\nbody\n",
			want:  "---\ntype: ticket\nparent:\ntitle: keep me\n---\n\nbody\n",
		},
		{
			name:  "block sequence ends at next key",
			input: "---\nblocks:\n  - tickets/old\ntitle: tickets/old\n---\n\nbody\n",
			want:  "---\nblocks:\n  - tickets/new\ntitle: tickets/old\n---\n\nbody\n",
		},
		{
			// #680 F4: a comment line between items no longer terminates the
			// rewrite — the item after it still retargets.
			name:  "block sequence with interleaved comment",
			input: "---\nblocks:\n  - tickets/old\n  # keep this comment\n  - tickets/old\n---\n\nbody\n",
			want:  "---\nblocks:\n  - tickets/new\n  # keep this comment\n  - tickets/new\n---\n\nbody\n",
		},
		{
			// #680 F4: a blank line between items no longer terminates it.
			name:  "block sequence with blank line",
			input: "---\nblocks:\n  - tickets/old\n\n  - tickets/old\n---\n\nbody\n",
			want:  "---\nblocks:\n  - tickets/new\n\n  - tickets/new\n---\n\nbody\n",
		},
		{
			// #680 F4: an inline trailing comment on the key line no longer
			// hides the whole sequence.
			name:  "block sequence with comment after key",
			input: "---\nblocks:  # note\n  - tickets/old\n---\n\nbody\n",
			want:  "---\nblocks:  # note\n  - tickets/new\n---\n\nbody\n",
		},
		{
			// #680 F4: an inline trailing comment on a scalar is preserved and
			// only the value ahead of it is rewritten.
			name:  "scalar with inline comment",
			input: "---\nparent: tickets/old  # owner\n---\n\nbody\n",
			want:  "---\nparent: tickets/new  # owner\n---\n\nbody\n",
		},
		{
			// #680 F2: a wikilink-form ref value (the shape renderMarkdown
			// emits single-quoted) is rewritten in place. The rewriter
			// re-quotes via yamlQuoteString like every other branch, so a
			// leading-indicator value normalizes to double quotes — the value
			// still round-trips to the same "[[tickets/new]]" string.
			name:  "wikilink-form scalar",
			input: "---\nparent: '[[tickets/old]]'\n---\n\nbody\n",
			want:  "---\nparent: \"[[tickets/new]]\"\n---\n\nbody\n",
		},
		{
			// #680 F2: a double-quoted wikilink deep-link preserves its suffix.
			name:  "wikilink-form sub-unit deep link",
			input: "---\nparent: \"[[tickets/old#S1]]\"\n---\n\nbody\n",
			want:  "---\nparent: \"[[tickets/new#S1]]\"\n---\n\nbody\n",
		},
		{
			// #680: a multi-line flow sequence is rewritten (the old
			// line-oriented rewriter never entered it).
			name:  "multi-line flow sequence",
			input: "---\nblocks: [\n  tickets/old,\n  tickets/keep,\n]\n---\n\nbody\n",
			want:  "---\nblocks: [\n  tickets/new,\n  tickets/keep,\n]\n---\n\nbody\n",
		},
		{
			// An aliased wikilink resolves nowhere, so a move leaves it alone.
			name:  "aliased wikilink is left untouched",
			input: "---\nparent: '[[tickets/old|Old Ticket]]'\n---\n\nbody\n",
			want:  "---\nparent: '[[tickets/old|Old Ticket]]'\n---\n\nbody\n",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			got := string(rewriteFrontmatterEdgeValues([]byte(testCase.input), "tickets/old", "tickets/new", edgeTypes, nil))

			if got != testCase.want {
				subtest.Errorf("rewrite:\n got: %q\nwant: %q", got, testCase.want)
			}
		})
	}
}
