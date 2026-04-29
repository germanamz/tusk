package namederr_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/lint/namederr"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestNamederr(test *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(test, testdata, namederr.Analyzer, "a")
}
