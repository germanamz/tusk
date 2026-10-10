package doctor

import "sort"

// Issue severities. Every Issue carries exactly one, stamped from
// severityByKind when the report is finalized.
const (
	// SeverityError means the vault or the index is wrong: something the
	// author broke and should fix. `tusk doctor` exits 1 when one is present.
	SeverityError = "error"

	// SeverityWarning means things are correct but degraded: a node missing
	// from semantic results, an unvalidated type, a no-op setting.
	SeverityWarning = "warning"

	// SeverityAdvice is a hint that changes nothing about correctness.
	SeverityAdvice = "advice"
)

// severityByKind is the single classification of every issue kind.
// TestSeverityFor_CoversEveryDeclaredKind fails when a declared kind is
// missing, so a new kind cannot ship unclassified.
var severityByKind = map[string]string{
	IssueDanglingEdge:              SeverityError,
	IssueEdgeTypeViolation:         SeverityError,
	IssueEdgeCardinalityViolation:  SeverityError,
	IssueRefDangling:               SeverityError,
	IssueRefAmbiguous:              SeverityError,
	IssueRefTypeMismatch:           SeverityError,
	IssueRefCycle:                  SeverityError,
	IssueRequiredMissing:           SeverityError,
	IssueEnumViolation:             SeverityError,
	IssueTypeMismatch:              SeverityError,
	IssueWorkflowViolation:         SeverityError,
	IssueSkippedFile:               SeverityError,
	IssueGraphExpansionInvalidEdge: SeverityError, // breaks every --semantic query
	IssueAliasInvalid:              SeverityError,
	IssueContextInvalid:            SeverityError,
	IssueContextPinnedMissing:      SeverityError,
	IssueRuleInvalid:               SeverityError,

	IssueUndeclaredProperty:        SeverityWarning,
	IssueUndeclaredType:            SeverityWarning,
	IssueEmbedRetry:                SeverityWarning,
	IssueEmbedNoChunks:             SeverityWarning, // the node drops out of semantic results
	IssueEmbeddingDrift:            SeverityWarning,
	IssueSubUnitsDisabledDirty:     SeverityWarning,
	IssueLegacyCLIEdge:             SeverityWarning,
	IssueLegacyMCPEdge:             SeverityWarning,
	IssueGraphExpansionUnknownEdge: SeverityWarning,
	IssueGraphExpansionNoEdges:     SeverityWarning,
	IssueGraphExpansionWeightZero:  SeverityWarning,
	IssuePathMissing:               SeverityWarning, // a plan may name a file still to be written

	IssueEmbedLargeChunk:     SeverityAdvice,
	IssueEmbeddingPrefixHint: SeverityAdvice,
}

// SeverityFor returns the severity of an issue kind and whether the kind is
// classified. An unclassified kind (a property-drift row written under a kind
// this binary does not know) reports as a warning.
func SeverityFor(kind string) (string, bool) {
	severity, ok := severityByKind[kind]

	if !ok {
		return SeverityWarning, false
	}

	return severity, true
}

// severityRank orders severities for the report: errors first, advice last.
func severityRank(severity string) int {
	switch severity {
	case SeverityError:
		return 0
	case SeverityWarning:
		return 1
	default:
		return 2
	}
}

// finalizeIssues stamps the Severity of every issue a check left unset from
// its Kind, and orders the list errors first, then warnings, then advice. A
// check sets Severity itself only when it is not a property of the kind: a
// rule violation carries its rule's severity. The sort is stable, so the check
// order holds within a severity. Idempotent: RunWithMigration calls it again
// after appending migration issues.
func finalizeIssues(issues []Issue) {
	for position := range issues {
		if issues[position].Severity == "" {
			issues[position].Severity, _ = SeverityFor(issues[position].Kind)
		}
	}

	sort.SliceStable(issues, func(left, right int) bool {
		return severityRank(issues[left].Severity) < severityRank(issues[right].Severity)
	})
}

// Counts is the per-severity total of a report's issues.
type Counts struct {
	Errors   int
	Warnings int
	Advice   int
}

// Counts returns the per-severity totals of report.Issues. Every surface
// builds its summary line and its exit decision from it.
func (report *Report) Counts() Counts {
	var counts Counts

	for _, issue := range report.Issues {
		switch issue.Severity {
		case SeverityError:
			counts.Errors++
		case SeverityWarning:
			counts.Warnings++
		default:
			counts.Advice++
		}
	}

	return counts
}
