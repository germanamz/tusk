package aliasdispatch_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/aliasdispatch"
	"github.com/germanamz/tusk/internal/doctor"
)

// Neither tusk_doctor nor a doctor alias runs project checks (#852); the
// payload counts the skipped ones only when there are some, so a workspace
// without checks keeps its exact shape.
func TestDoctorIssuesPayload_CountsSkippedChecksOnlyWhenThereAreSome(test *testing.T) {
	if _, present := aliasdispatch.DoctorIssuesPayload(&doctor.Report{})["checks_skipped"]; present {
		test.Fatal("checks_skipped present with no checks declared")
	}

	if got := aliasdispatch.DoctorIssuesPayload(&doctor.Report{ChecksSkipped: 2})["checks_skipped"]; got != 2 {
		test.Fatalf("checks_skipped = %v, want 2", got)
	}
}
