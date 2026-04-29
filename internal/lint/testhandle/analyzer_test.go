package testhandle_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/lint/testhandle"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestTesthandle(test *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(test, testdata, testhandle.Analyzer, "a")
}
