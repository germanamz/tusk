package embed_test

import (
	"math"
	"testing"

	"github.com/germanamz/tusk/internal/embed"
)

func TestCosineSimilarity_OrthogonalVectorsZero(test *testing.T) {
	left := []float32{1, 0}
	right := []float32{0, 1}

	score := embed.CosineSimilarity(left, right)

	if math.Abs(score) > 1e-6 {
		test.Errorf("score = %v, want 0", score)
	}
}

func TestCosineSimilarity_IdenticalVectorsOne(test *testing.T) {
	vector := []float32{0.5, 0.5, 0.5}

	score := embed.CosineSimilarity(vector, vector)

	if math.Abs(score-1.0) > 1e-6 {
		test.Errorf("score = %v, want 1", score)
	}
}

func TestCosineSimilarity_OppositeVectorsNegativeOne(test *testing.T) {
	left := []float32{1, 0}
	right := []float32{-1, 0}

	score := embed.CosineSimilarity(left, right)

	if math.Abs(score+1.0) > 1e-6 {
		test.Errorf("score = %v, want -1", score)
	}
}

func TestCosineSimilarity_DimMismatchReturnsZero(test *testing.T) {
	left := []float32{1, 0, 0}
	right := []float32{0, 1}

	score := embed.CosineSimilarity(left, right)

	if score != 0 {
		test.Errorf("score = %v, want 0 (dim mismatch falls back to 0)", score)
	}
}

func TestCosineSimilarity_ZeroVectorReturnsZero(test *testing.T) {
	left := []float32{0, 0, 0}
	right := []float32{1, 1, 1}

	score := embed.CosineSimilarity(left, right)

	if score != 0 {
		test.Errorf("score = %v, want 0 (zero vector → 0)", score)
	}
}
