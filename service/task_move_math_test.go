package service

import (
	"errors"
	"math"
	"testing"

	"github.com/germanamz/tusk/domain"
)

func TestComputeMidpoint(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		low     float64
		high    float64
		want    float64
		wantErr error
	}{
		{name: "one_to_two", low: 1, high: 2, want: 1.5},
		{name: "zero_to_one", low: 0, high: 1, want: 0.5},
		{name: "reversed", low: 2, high: 1, wantErr: domain.ErrOrderGapExhausted},
		{name: "equal", low: 1, high: 1, wantErr: domain.ErrOrderGapExhausted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := computeMidpoint(tc.low, tc.high)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err: got %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("mid: got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestComputeMidpoint_AdjacentFloats exercises the endpoint-equality branch.
// With low = 1.0 and high = math.Nextafter(1.0, 2.0) there is no representable
// float64 strictly between the two, so the midpoint equals one endpoint and
// computeMidpoint must flag an exhausted gap.
func TestComputeMidpoint_AdjacentFloats(t *testing.T) {
	t.Parallel()
	low := 1.0
	high := math.Nextafter(1.0, 2.0)
	if low >= high {
		t.Fatalf("test setup invalid: Nextafter produced %v which is not > %v", high, low)
	}
	_, err := computeMidpoint(low, high)
	if !errors.Is(err, domain.ErrOrderGapExhausted) {
		t.Fatalf("err: got %v, want %v", err, domain.ErrOrderGapExhausted)
	}
}
