package embed

import "math"

// CosineSimilarity computes the cosine of the angle between two vectors.
//
// Returns 0 when the vectors have mismatched dimensions or either is the
// zero vector.
func CosineSimilarity(left, right []float32) float64 {
	if len(left) != len(right) || len(left) == 0 {
		return 0
	}

	var dot, leftMag, rightMag float64

	for index := 0; index < len(left); index++ {
		leftValue := float64(left[index])
		rightValue := float64(right[index])

		dot += leftValue * rightValue
		leftMag += leftValue * leftValue
		rightMag += rightValue * rightValue
	}

	if leftMag == 0 || rightMag == 0 {
		return 0
	}

	return dot / (math.Sqrt(leftMag) * math.Sqrt(rightMag))
}
