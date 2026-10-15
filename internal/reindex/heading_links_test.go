package reindex_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
)

// rewriteLater writes content to relPath with an mtime a minute past the
// last one, so the incremental walk re-reads it whatever the filesystem's
// mtime resolution.
func rewriteLater(test *testing.T, root, relPath string, content []byte) {
	test.Helper()

	abs := filepath.Join(root, relPath)
	info, statErr := os.Stat(abs)

	if statErr != nil {
		test.Fatalf("stat %s: %v", relPath, statErr)
	}

	writeRaw(test, root, relPath, content)

	later := info.ModTime().Add(time.Minute)

	if chErr := os.Chtimes(abs, later, later); chErr != nil {
		test.Fatalf("chtimes %s: %v", relPath, chErr)
	}
}

// The RFC's sections, as the sub-unit walker addresses them:
//
//	# Overview                     S1
//	## Tokens                      S1.1
//	# Open Questions: Phase 2      S2
//	### Tokens                     S2.1
const headingRFC = "# Overview\n\nIntro.\n\n## Tokens\n\nFirst.\n\n# Open Questions: Phase 2\n\n### Tokens\n\nNested.\n"

func TestRun_HeadingLinksResolveToSectionsAndFollowEdits(test *testing.T) {
	root, store := nameLinkFixture(test)

	writeRaw(test, root, "notes/auth-rfc.md", []byte(headingRFC))
	writeRaw(test, root, "inbox/headings.md", []byte(
		"See [[auth-rfc#open questions phase 2]] and [[auth-rfc#Tokens]].\n\n"+
			"Nested: [[notes/auth-rfc#Open Questions: Phase 2#Tokens|deep]].\n\n"+
			"# Agenda\n\nBack to [[#agenda]].\n",
	))
	writeRaw(test, root, "inbox/misses.md", []byte("Read [[auth-rfc#Gone]] and [[auth-rfc#^block-1]].\n"))

	runNameLinks(test, root, store, "")

	wantEdgeTargets(test, store, "inbox/headings.md", "references",
		"inbox/headings#S1", "notes/auth-rfc#S1.1", "notes/auth-rfc#S2", "notes/auth-rfc#S2.1")

	// Neither fragment names anything, so both links land on the note; the
	// note is linked once, and the younger row waits at its fragment.
	wantEdgeTargets(test, store, "inbox/misses.md", "references", "notes/auth-rfc", "notes/auth-rfc#^block-1")

	// The linking files never change, yet every link follows the RFC: a new
	// section ahead renumbers the rest, the first Tokens is renamed so the
	// name finds the second, and a Gone heading appears.
	rewriteLater(test, root, "notes/auth-rfc.md", []byte(
		"# Preface\n\nNew.\n\n"+
			"# Overview\n\nIntro.\n\n## Secrets\n\nFirst.\n\n"+
			"# Open Questions: Phase 2\n\n### Tokens\n\nNested.\n\n## Gone\n\nBack.\n",
	))

	runNameLinks(test, root, store, "")

	wantEdgeTargets(test, store, "inbox/headings.md", "references",
		"inbox/headings#S1", "notes/auth-rfc#S3", "notes/auth-rfc#S3.1")

	// Gone resolves, and the block reference takes the note it now has to
	// itself, in the same pass.
	wantEdgeTargets(test, store, "inbox/misses.md", "references", "notes/auth-rfc", "notes/auth-rfc#S3.2")

	// Section rows carry the key a heading link matches.
	row, getErr := index.NewNodeRepo(store).Get("notes/auth-rfc#S3")

	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if row.NameKey.String != "open questions phase 2" {
		test.Errorf("section name_key = %q, want %q", row.NameKey.String, "open questions phase 2")
	}
}
