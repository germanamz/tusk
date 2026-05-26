package subdocument_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/typepacks/subdocument"
)

func TestSubdocumentSourceIsMarkdown(test *testing.T) {
	test.Parallel()

	if subdocument.Source() != "markdown" {
		test.Errorf("Source() = %q, want %q", subdocument.Source(), "markdown")
	}
}
