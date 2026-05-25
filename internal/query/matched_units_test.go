package query_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/query"
)

// TestHeadingWeight pins the §5.7 section-aggregation multipliers and the
// out-of-range boundary behavior. Levels outside [1,6] return 0 so callers
// never multiply a leaf score by an undefined weight.
func TestHeadingWeight(test *testing.T) {
	cases := []struct {
		level int
		want  float64
	}{
		{level: 0, want: 0},
		{level: 1, want: 1.00},
		{level: 2, want: 0.85},
		{level: 6, want: 0.25},
		{level: 7, want: 0},
	}

	for _, scenario := range cases {
		got := query.HeadingWeight(scenario.level)

		if got != scenario.want {
			test.Errorf("HeadingWeight(%d) = %f, want %f", scenario.level, got, scenario.want)
		}
	}
}
