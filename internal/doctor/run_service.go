package doctor

// Request configures a RunWithMigration call. Cfg carries the repos and
// manifest; NoMigrate toggles the diagnostic-only mode (legacy edge rows are
// surfaced as issues instead of being rewritten into source frontmatter).
type Request struct {
	Cfg       Config
	NoMigrate bool
}

// Result is the typed payload returned by RunWithMigration. Migration is nil
// when NoMigrate is true.
type Result struct {
	Report    *Report
	Migration *MigrationReport
}

// RunWithMigration orchestrates the full doctor sequence used by both the CLI
// (`tusk doctor`) and the MCP handler (`tusk_doctor`):
//
//  1. Unless NoMigrate is set, call Migrate to rewrite legacy __cli__/__mcp__
//     edge rows back into source frontmatter.
//  2. Call Run to produce the diagnostic Report.
//  3. Append the legacy rows migration left in place (Migration.Unmigrated)
//     or, when NoMigrate is set, LegacyDrift issues so the user still sees
//     pending migration work without it happening implicitly.
//  4. Re-finalize so the appended issues carry severities and the report stays
//     ordered errors first.
//
// Callers MUST hold the workspace lock — Migrate mutates source files.
func RunWithMigration(req Request) (*Result, error) {
	result := &Result{}

	if !req.NoMigrate {
		migration, migrateErr := Migrate(req.Cfg)

		if migrateErr != nil {
			return nil, migrateErr
		}

		result.Migration = migration
	}

	report, runErr := Run(req.Cfg)

	if runErr != nil {
		return nil, runErr
	}

	if result.Migration != nil {
		report.Issues = append(report.Issues, result.Migration.Unmigrated...)
	}

	if req.NoMigrate {
		legacyIssues, legacyErr := LegacyDrift(req.Cfg)

		if legacyErr != nil {
			return nil, legacyErr
		}

		report.Issues = append(report.Issues, legacyIssues...)
	}

	finalizeIssues(report.Issues)

	result.Report = report

	return result, nil
}
