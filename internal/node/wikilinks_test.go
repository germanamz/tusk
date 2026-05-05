package node_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/germanamz/tusk/internal/node"
)

func TestExtractWikilinks_FindsBracketedTargets(test *testing.T) {
	body := []byte(`# Body

See [[notes/auth-rfc]] for context.

Also relates to [[tickets/refactor-storage]].

A second mention of [[notes/auth-rfc]] is deduped.
`)

	links := node.ExtractWikilinks(body)

	sort.Strings(links)

	want := []string{"notes/auth-rfc", "tickets/refactor-storage"}

	if !reflect.DeepEqual(links, want) {
		test.Errorf("got %v, want %v", links, want)
	}
}

func TestExtractWikilinks_IgnoresEscapedAndCodeFence(test *testing.T) {
	body := []byte("Real link [[real]].\n\n```\nfenced [[notreal]]\n```\n")

	links := node.ExtractWikilinks(body)

	if len(links) != 1 || links[0] != "real" {
		test.Errorf("got %v, want [real]", links)
	}
}

func TestExtractWikilinks_ReturnsEmptyWhenNoLinks(test *testing.T) {
	links := node.ExtractWikilinks([]byte("plain body, no brackets at all\n"))

	if len(links) != 0 {
		test.Errorf("got %v, want empty", links)
	}
}
