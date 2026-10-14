package main

import (
	"math"
	"testing"
)

func TestQuantile(test *testing.T) {
	values := []float64{4, 1, 3, 2}

	cases := map[float64]float64{0: 1, 0.25: 1.75, 0.5: 2.5, 0.75: 3.25, 1: 4}

	for fraction, want := range cases {
		if got := quantile(values, fraction); got != want {
			test.Errorf("quantile(%v) = %v, want %v", fraction, got, want)
		}
	}

	if !math.IsNaN(median(nil)) || !math.IsNaN(mean(nil)) {
		test.Error("empty input should give NaN")
	}

	if values[0] != 4 {
		test.Error("quantile sorted its input in place")
	}
}

func TestBootstrapCI(test *testing.T) {
	values := []float64{0.5, 0.6, 0.55, 0.7, 0.4, 0.65, 0.5, 0.6, 0.45, 0.58}

	low, high := bootstrapCI(values, median)

	if !(low <= median(values) && median(values) <= high) {
		test.Errorf("interval [%v, %v] misses the median %v", low, high, median(values))
	}

	againLow, againHigh := bootstrapCI(values, median)

	if low != againLow || high != againHigh {
		test.Error("bootstrap is not reproducible")
	}

	constantLow, constantHigh := bootstrapCI([]float64{2, 2, 2}, mean)

	if constantLow != 2 || constantHigh != 2 {
		test.Errorf("constant input gave [%v, %v]", constantLow, constantHigh)
	}
}
