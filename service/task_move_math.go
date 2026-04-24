package service

import (
	"math"

	"github.com/germanamz/tusk/domain"
)

// computeMidpoint returns the arithmetic mean of low and high. It returns
// domain.ErrOrderGapExhausted when the mean is indistinguishable from either
// endpoint under float64 comparison — that is, when the gap has collapsed
// below the representation's resolution at that magnitude.
func computeMidpoint(low, high float64) (float64, error) {
	if !(low < high) {
		return 0, domain.ErrOrderGapExhausted
	}
	mid := low + (high-low)/2
	if mid == low || mid == high {
		return 0, domain.ErrOrderGapExhausted
	}
	if math.IsNaN(mid) || math.IsInf(mid, 0) {
		return 0, domain.ErrOrderGapExhausted
	}
	return mid, nil
}
