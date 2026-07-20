package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/germanamz/tusk/internal/selfupdate"
)

func newUpdateCmd() *cobra.Command {
	var (
		checkOnly bool
		force     bool
		asJSON    bool
	)

	updateCmd := &cobra.Command{
		Use:   "update [version]",
		Short: "Replace the running tusk binary with a published release",
		Long: `Replace the running tusk binary with a build from GitHub Releases.

With no argument, or with "latest", the newest published release is
installed. Pass a tag to pin a specific version; a bare "2.3.0" is
normalized to "v2.3.0". Installing an older version than the one running
is allowed and reported as a downgrade.

The archive is verified against the release checksums file before anything
is extracted, and the previous binary is kept aside until the replacement
is in place, so a failed swap rolls back rather than leaving no binary.

Man pages ship in the release archive and are installed alongside the
binary, following the same layout as install.sh: a binary in <prefix>/bin
puts its man pages in <prefix>/share/man. Set MAN_DIR to override. Man-page
installation is best-effort — a read-only directory prints a note and does
not fail an update that already succeeded.

INSTALL METHOD
  A binary managed by Homebrew, installed with "go install", or built from
  source is owned by that tool, and replacing it in place would leave the
  tool's records inconsistent. Those are refused with the correct upgrade
  command for that method; pass --force to replace the binary anyway.
  "--check" predicts the refusal rather than reporting an update that the
  real run would reject.

  --force also permits reinstalling the version already running, which
  repairs a corrupted binary in place.

  Your workspace and its .tusk/ index are never touched by an update.

EXIT CODES
  0  success, including --check whatever it found
  1  generic failure
  2  network, release-resolution, or invalid-version failure
  3  checksum verification failure
  4  install-method refusal
  5  permission or swap failure`,
		Example: `  # Update to the latest release
  tusk update

  # Pin a specific version (or roll back to one)
  tusk update v2.3.0

  # Report what an update would do, without changing anything
  tusk update --check
  tusk update --check --json

  # Replace a Homebrew- or go-install-managed binary anyway
  tusk update --force`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requested := selfupdate.LatestVersion

			if len(args) == 1 {
				requested = args[0]
			}

			updater := &selfupdate.Updater{Force: force}

			runErr := runUpdate(cmd, updater, requested, checkOnly, asJSON)

			if runErr == nil {
				return nil
			}

			// SilenceErrors is set on root and main.go prints only on the
			// normal return path, which os.Exit bypasses — without this the
			// user gets a bare exit code and no message.
			if code := updateExitCode(runErr); code != 1 {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), runErr)

				os.Exit(code)
			}

			return runErr // cobra exits with 1 (printed by main.go)
		},
	}

	updateCmd.Flags().BoolVar(&checkOnly, "check", false, "report the available version without installing anything")
	updateCmd.Flags().BoolVar(&force, "force", false, "replace the binary even when another tool manages it, or reinstall the current version")
	updateCmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")

	return updateCmd
}

// runUpdate resolves a plan and either reports it (--check) or applies it.
func runUpdate(cmd *cobra.Command, updater *selfupdate.Updater, requested string, checkOnly bool, asJSON bool) error {
	out := cmd.OutOrStdout()

	plan, planErr := updater.Plan(cmd.Context(), requested)

	if planErr != nil {
		return planErr
	}

	if checkOnly {
		return reportPlan(out, plan, asJSON)
	}

	result, applyErr := updater.Apply(cmd.Context(), plan)

	if applyErr != nil {
		return applyErr
	}

	return reportResult(out, plan, result, asJSON)
}

// reportPlan renders a --check result. It always succeeds: whether an update
// is available is carried in the output, not the exit code, so a check that
// worked never reads as a failure to a script.
func reportPlan(out io.Writer, plan selfupdate.Plan, asJSON bool) error {
	if asJSON {
		return writeJSON(out, plan)
	}

	if !plan.UpdateNeeded {
		// "(latest)" is only true when latest is what was asked for; a pinned
		// version that happens to match says so instead.
		qualifier := "latest"

		if plan.RequestedVersion != selfupdate.LatestVersion {
			qualifier = "requested " + plan.RequestedVersion
		}

		_, _ = fmt.Fprintf(out, "update: already on %s (%s)\n", plan.CurrentVersion, qualifier)

		return nil
	}

	verb := "update available"

	if plan.Direction == "older" {
		verb = "downgrade requested"
	}

	_, _ = fmt.Fprintf(out, "update: %s — %s → %s\n", verb, plan.CurrentVersion, plan.TargetVersion)
	_, _ = fmt.Fprintf(out, "  binary:  %s (%s)\n", plan.BinaryPath, plan.InstallMethod)
	_, _ = fmt.Fprintf(out, "  archive: %s\n", plan.ArchiveName)

	// Predict the refusal rather than reporting an update that a real run is
	// guaranteed to reject.
	if plan.Refused {
		_, _ = fmt.Fprintf(out, "\nthis update would be refused:\n%s\n", plan.RefusalReason)
	}

	return nil
}

// reportResult renders the outcome of an applied update.
func reportResult(out io.Writer, plan selfupdate.Plan, result selfupdate.Result, asJSON bool) error {
	if asJSON {
		return writeJSON(out, result)
	}

	// Keyed off what Apply actually did, not off the plan: --force reinstalls
	// a matching version, and reporting "nothing to do" for a run that
	// rewrote the binary would be a lie.
	if !result.Installed {
		_, _ = fmt.Fprintf(out, "update: already on %s — nothing to do\n", result.NewVersion)

		return nil
	}

	if plan.Direction == "same" {
		_, _ = fmt.Fprintf(out, "update: reinstalling %s\n", result.NewVersion)
	}

	if plan.Direction == "older" {
		_, _ = fmt.Fprintf(out, "update: downgrading %s → %s\n", result.PreviousVersion, result.NewVersion)
	}

	_, _ = fmt.Fprintf(out, "update: installed %s to %s\n", result.NewVersion, result.BinaryPath)

	if result.ManPagesDir != "" {
		_, _ = fmt.Fprintf(out, "update: man pages installed to %s\n", filepath.Join(result.ManPagesDir, "man1"))
	}

	if result.ManPagesNote != "" {
		_, _ = fmt.Fprintf(out, "update: note: man pages skipped (%s)\n", result.ManPagesNote)
	}

	return nil
}

// updateExitCode maps a failure to a distinct process exit code for
// scripting. Anything unrecognized is 1, which cobra reports itself.
func updateExitCode(runErr error) int {
	switch {
	case errors.Is(runErr, selfupdate.ErrNetwork), errors.Is(runErr, selfupdate.ErrNoAsset),
		errors.Is(runErr, selfupdate.ErrInvalidVersion):
		return 2
	case errors.Is(runErr, selfupdate.ErrChecksum):
		return 3
	case errors.Is(runErr, selfupdate.ErrInstallMethod):
		return 4
	case errors.Is(runErr, selfupdate.ErrPermission):
		return 5
	}

	return 1
}
