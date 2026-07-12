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
			// #690: an aliased wikilink now resolves to its id (the display
			// suffix is dropped for resolution), so a move retargets the id and
			// preserves the "|Old Ticket" label. Re-quoted to double like every
			// wikilink branch (the value carries a leading `[` indicator).
			name:  "aliased wikilink retargets id and keeps alias",
			input: "---\nparent: '[[tickets/old|Old Ticket]]'\n---\n\nbody\n",
			want:  "---\nparent: \"[[tickets/new|Old Ticket]]\"\n---\n\nbody\n",
		},
		{
			// #690: an aliased deep link keeps both its sub-unit fragment and
			// its display suffix; only the id ahead of them is retargeted.
			name:  "aliased wikilink sub-unit deep link",
			input: "---\nparent: \"[[tickets/old#S1|Old Ticket]]\"\n---\n\nbody\n",
			want:  "---\nparent: \"[[tickets/new#S1|Old Ticket]]\"\n---\n\nbody\n",
		},
		{
			// #692: an unquoted wikilink `parent: [[tickets/old]]` parses as a
			// nested flow sequence, but the `[[` `]]` brackets are the sequence
			// delimiters on disk — so retargeting only the inner scalar keeps
			// them intact and the link stays unquoted.
			name:  "bare unquoted wikilink sequence",
			input: "---\nparent: [[tickets/old]]\n---\n\nbody\n",
			want:  "---\nparent: [[tickets/new]]\n---\n\nbody\n",
		},
		{
			// #692: the alias suffix rides along on the inner scalar, so an
			// unquoted aliased wikilink keeps its label under a move.
			name:  "aliased unquoted wikilink sequence",
			input: "---\nparent: [[tickets/old|Old Ticket]]\n---\n\nbody\n",
			want:  "---\nparent: [[tickets/new|Old Ticket]]\n---\n\nbody\n",
		},
		{
			// #692: a sub-unit fragment on an unquoted wikilink is preserved
			// like every other wikilink branch.
			name:  "unquoted wikilink sequence sub-unit deep link",
			input: "---\nparent: [[tickets/old#S1]]\n---\n\nbody\n",
			want:  "---\nparent: [[tickets/new#S1]]\n---\n\nbody\n",
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

// #692: on a REF property the bare frontmatter value is a title, so a bare
// scalar that merely equals the old id is left alone (bareIsID=false). An
// unquoted wikilink is still an id though — the nested-sequence shape must be
// retargeted on move regardless, or a ref like `assignee: [[people/jane]]`
// would be orphaned to a dead id after `tusk node move`.
func TestRewriteFrontmatterEdgeValues_RefPropertyWikilinkSequence(test *testing.T) {
	edgeTypes := manifest.EdgeTypes{
		"assignee": manifest.EdgeType{To: []string{"person"}},
	}
	nodeTypes := map[string]manifest.NodeType{
		"ticket": {
			Properties: []manifest.PropertyDecl{
				{Name: "assignee", Type: "ref", To: "person"},
			},
		},
	}

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "bare unquoted ref wikilink retargets",
			input: "---\ntype: ticket\nassignee: [[people/old]]\n---\n\nbody\n",
			want:  "---\ntype: ticket\nassignee: [[people/new]]\n---\n\nbody\n",
		},
		{
			name:  "aliased unquoted ref wikilink keeps label",
			input: "---\ntype: ticket\nassignee: [[people/old|Janey]]\n---\n\nbody\n",
			want:  "---\ntype: ticket\nassignee: [[people/new|Janey]]\n---\n\nbody\n",
		},
		{
			// A bare ref title that merely coincides with the old id is NOT a
			// wikilink and must stay put (#680 invariant preserved).
			name:  "bare ref title coinciding with id is untouched",
			input: "---\ntype: ticket\nassignee: people/old\n---\n\nbody\n",
			want:  "---\ntype: ticket\nassignee: people/old\n---\n\nbody\n",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			got := string(rewriteFrontmatterEdgeValues([]byte(testCase.input), "people/old", "people/new", edgeTypes, nodeTypes))

			if got != testCase.want {
				subtest.Errorf("rewrite:\n got: %q\nwant: %q", got, testCase.want)
			}
		})
	}
}
