package node

import "testing"

// rewriteBodyWikilinks retargets a moved node's id inside body [[wikilinks]].
// It must handle the bare, deep-link, and Obsidian aliased forms, preserve the
// `#fragment` and `|display` suffix verbatim, and — critically — never touch a
// longer id that merely shares oldID as a prefix (#690).
func TestRewriteBodyWikilinks(test *testing.T) {
	const body = "---\ntitle: x\n---\n"

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "bare link",
			input: body + "see [[notes/old]] here\n",
			want:  body + "see [[notes/new]] here\n",
		},
		{
			name:  "sub-unit deep link",
			input: body + "jump to [[notes/old#S1]]\n",
			want:  body + "jump to [[notes/new#S1]]\n",
		},
		{
			name:  "aliased link keeps display text",
			input: body + "see [[notes/old|the target]] here\n",
			want:  body + "see [[notes/new|the target]] here\n",
		},
		{
			name:  "aliased deep link keeps fragment and display",
			input: body + "see [[notes/old#S1|that section]] here\n",
			want:  body + "see [[notes/new#S1|that section]] here\n",
		},
		{
			// A longer id that merely starts with oldID must be left alone,
			// with or without an alias — the regex is anchored on the full id
			// followed by a fragment/alias/close, never a bare continuation.
			name:  "prefix-collision id is untouched",
			input: body + "unrelated [[notes/oldbar]] and [[notes/oldbar|x]]\n",
			want:  body + "unrelated [[notes/oldbar]] and [[notes/oldbar|x]]\n",
		},
		{
			// Frontmatter is not the body region: a wikilink-shaped value in
			// the frontmatter block is left to the YAML rewriter, not this one.
			name:  "frontmatter region is not rewritten here",
			input: "---\nrel: [[notes/old]]\n---\nbody with no link\n",
			want:  "---\nrel: [[notes/old]]\n---\nbody with no link\n",
		},
		{
			name:  "surrounding whitespace normalizes",
			input: body + "spaced [[ notes/old ]] out\n",
			want:  body + "spaced [[notes/new]] out\n",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			got := string(rewriteBodyWikilinks([]byte(testCase.input), "notes/old", "notes/new"))

			if got != testCase.want {
				subtest.Errorf("rewrite:\n got: %q\nwant: %q", got, testCase.want)
			}
		})
	}
}
