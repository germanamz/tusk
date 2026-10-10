package wikilink_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/germanamz/tusk/internal/wikilink"
)

func TestExtract_FindsBracketedTargets(test *testing.T) {
	body := []byte(`# Body

See [[notes/auth-rfc]] for context.

Also relates to [[tickets/refactor-storage]].

A second mention of [[notes/auth-rfc]] is deduped.
`)

	links := wikilink.Extract(body)

	sort.Strings(links)

	want := []string{"notes/auth-rfc", "tickets/refactor-storage"}

	if !reflect.DeepEqual(links, want) {
		test.Errorf("got %v, want %v", links, want)
	}
}

func TestExtract_IgnoresEscapedAndCodeFence(test *testing.T) {
	body := []byte("Real link [[real]].\n\n```\nfenced [[notreal]]\n```\n")

	links := wikilink.Extract(body)

	if len(links) != 1 || links[0] != "real" {
		test.Errorf("got %v, want [real]", links)
	}
}

func TestExtract_ReturnsEmptyWhenNoLinks(test *testing.T) {
	links := wikilink.Extract([]byte("plain body, no brackets at all\n"))

	if len(links) != 0 {
		test.Errorf("got %v, want empty", links)
	}
}

// #690: an Obsidian aliased wikilink `[[id|display]]` links to id — the
// display text after the pipe is presentation only. Extraction must return the
// target (before the pipe), trimmed, exactly as a bare `[[id]]` would.
func TestExtract_AliasedLinkExtractsTarget(test *testing.T) {
	body := []byte("see [[notes/target|the target]] and [[design/x | Design X]] and bare [[notes/target]]\n")

	links := wikilink.Extract(body)

	// The aliased and bare links to notes/target dedupe to one target.
	want := []string{"notes/target", "design/x"}

	if !reflect.DeepEqual(links, want) {
		test.Errorf("got %v, want %v", links, want)
	}
}

// #690: an aliased deep link `[[id#S1|display]]` keeps its sub-unit fragment in
// the target and drops only the display suffix.
func TestExtract_AliasedDeepLinkKeepsFragment(test *testing.T) {
	body := []byte("jump to [[notes/target#S1|that section]]\n")

	links := wikilink.Extract(body)

	if len(links) != 1 || links[0] != "notes/target#S1" {
		test.Errorf("got %v, want [notes/target#S1]", links)
	}
}
