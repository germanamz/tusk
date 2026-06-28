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
			want:  "---\ntype: ticket\nblocks: [ tickets/new, tickets/keep]\n---\n\nbody\n",
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
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			got := string(rewriteFrontmatterEdgeValues([]byte(testCase.input), "tickets/old", "tickets/new", edgeTypes))

			if got != testCase.want {
				subtest.Errorf("rewrite:\n got: %q\nwant: %q", got, testCase.want)
			}
		})
	}
}
