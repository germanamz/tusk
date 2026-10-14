package main

import (
	"math"
	"math/rand/v2"
	"slices"
)

// bootstrapResamples and bootstrapSeed fix the bootstrap so a report
// recomputed from the same runs prints the same intervals.
const (
	bootstrapResamples = 10000
	bootstrapSeed      = 792
)

// quantile is the linearly interpolated q-quantile of values, or NaN for no
// values.
func quantile(values []float64, fraction float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}

	sorted := slices.Clone(values)
	slices.Sort(sorted)

	position := fraction * float64(len(sorted)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))

	return sorted[lower] + (sorted[upper]-sorted[lower])*(position-float64(lower))
}

func median(values []float64) float64 {
	return quantile(values, 0.5)
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}

	total := 0.0

	for _, value := range values {
		total += value
	}

	return total / float64(len(values))
}

// bootstrapCI is the 95% percentile bootstrap interval of statistic over
// values, resampling values with replacement. values are per-question
// figures, so the interval reflects which questions were asked.
func bootstrapCI(values []float64, statistic func([]float64) float64) (float64, float64) {
	if len(values) == 0 {
		return math.NaN(), math.NaN()
	}

	random := rand.New(rand.NewPCG(bootstrapSeed, bootstrapSeed))
	estimates := make([]float64, bootstrapResamples)
	sample := make([]float64, len(values))

	for index := range estimates {
		for slot := range sample {
			sample[slot] = values[random.IntN(len(values))]
		}

		estimates[index] = statistic(sample)
	}

	return quantile(estimates, 0.025), quantile(estimates, 0.975)
}
