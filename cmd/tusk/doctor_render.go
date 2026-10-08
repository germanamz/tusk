package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/germanamz/tusk/internal/doctor"
)

// --fail-on values: the lowest severity that makes `tusk doctor` exit 1.
const (
	doctorFailOnError   = "error"
	doctorFailOnWarning = "warning"
	doctorFailOnNever   = "never"
)

// doctorLocationsShown caps how many Locations an issue line lists; the JSON
// surfaces carry every one.
const doctorLocationsShown = 5

// validateDoctorFailOn rejects an unknown --fail-on value before doctor does
// any work.
func validateDoctorFailOn(failOn string) error {
	switch failOn {
	case doctorFailOnError, doctorFailOnWarning, doctorFailOnNever:
		return nil
	default:
		return fmt.Errorf("invalid --fail-on %q: want error, warning, or never", failOn)
	}
}

// renderDoctorIssues prints the summary line and one severity-led line per
// issue. A clean report prints "doctor: no issues". Shared by `tusk doctor`
// and `tusk run` on a doctor alias.
func renderDoctorIssues(out io.Writer, report *doctor.Report) {
	if len(report.Issues) == 0 {
		_, _ = fmt.Fprintln(out, "doctor: no issues")

		return
	}

	_, _ = fmt.Fprintf(out, "doctor: %s\n", formatDoctorCounts(report.Counts()))

	for _, issue := range report.Issues {
		_, _ = fmt.Fprintf(out, "  %-8s [%s] %s\n", issue.Severity, issue.Kind, describeDoctorIssue(issue))
	}
}

// describeDoctorIssue renders the part of an issue line after the [kind]
// token: "<node id>: <message>", then any Locations. A location under the
// issue's own node id shortens to its sub-unit address (#S1P1).
func describeDoctorIssue(issue doctor.Issue) string {
	text := issue.Message

	if issue.NodeID != "" {
		text = issue.NodeID + ": " + text
	}

	if len(issue.Locations) == 0 {
		return text
	}

	shown := issue.Locations

	if len(shown) > doctorLocationsShown {
		shown = shown[:doctorLocationsShown]
	}

	labels := make([]string, 0, len(shown))

	for _, location := range shown {
		if issue.NodeID != "" {
			location = strings.TrimPrefix(location, issue.NodeID)
		}

		labels = append(labels, location)
	}

	list := strings.Join(labels, ", ")

	if hidden := len(issue.Locations) - len(shown); hidden > 0 {
		list += fmt.Sprintf(", and %d more", hidden)
	}

	if issue.NodeID != "" {
		return text + "; also in " + list
	}

	return text + "; in " + list
}

// formatDoctorCounts renders "1 error, 2 warnings, 0 advice".
func formatDoctorCounts(counts doctor.Counts) string {
	return fmt.Sprintf("%s, %s, %d advice",
		pluralCount(counts.Errors, "error"), pluralCount(counts.Warnings, "warning"), counts.Advice)
}

func pluralCount(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, noun)
	}

	return fmt.Sprintf("%d %ss", count, noun)
}

// doctorFailure returns the error that makes doctor exit 1 when the report
// holds an issue at or above failOn, or nil. Advice never fails the run.
func doctorFailure(report *doctor.Report, failOn string) error {
	counts := report.Counts()

	switch failOn {
	case doctorFailOnNever:
		return nil
	case doctorFailOnWarning:
		if counts.Errors+counts.Warnings > 0 {
			return fmt.Errorf("doctor: %s, %s (--fail-on=warning)",
				pluralCount(counts.Errors, "error"), pluralCount(counts.Warnings, "warning"))
		}
	default:
		if counts.Errors > 0 {
			return fmt.Errorf("doctor: %s (--fail-on=error)", pluralCount(counts.Errors, "error"))
		}
	}

	return nil
}
