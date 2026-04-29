package blankline_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/lint/blankline"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestBlankline(test *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(test, testdata, blankline.Analyzer, "fixtures")
}
