package main

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/selfupdate"
)

// TestUpdateRegistered asserts the verb is wired into the root command and
// discoverable from help.
func TestUpdateRegistered(test *testing.T) {
	rootCmd := newRootCmd()

	var found bool

	for _, sub := range rootCmd.Commands() {
		if sub.Name() == "update" {
			found = true
		}
	}

	if !found {
		test.Fatal("update command is not registered on root")
	}
}

// TestUpdateRejectsExtraArgs asserts the command takes at most one version.
func TestUpdateRejectsExtraArgs(test *testing.T) {
	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"update", "v1.0.0", "v2.0.0"})

	var stdout, stderr bytes.Buffer

	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)

	if execErr := rootCmd.Execute(); execErr == nil {
		test.Fatal("update accepted two version arguments, want an error")
	}
}

func TestUpdateExitCode(test *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"network", fmt.Errorf("wrapped: %w", selfupdate.ErrNetwork), 2},
		{"no asset", fmt.Errorf("wrapped: %w", selfupdate.ErrNoAsset), 2},
		{"checksum", fmt.Errorf("wrapped: %w", selfupdate.ErrChecksum), 3},
		{"install method", fmt.Errorf("wrapped: %w", selfupdate.ErrInstallMethod), 4},
		{"permission", fmt.Errorf("wrapped: %w", selfupdate.ErrPermission), 5},
		{"unclassified", errors.New("something else"), 1},
	}

	for _, testCase := range cases {
		if got := updateExitCode(testCase.err); got != testCase.want {
			test.Errorf("updateExitCode(%s) = %d, want %d", testCase.name, got, testCase.want)
		}
	}
}

func TestReportPlanUpToDate(test *testing.T) {
	var out bytes.Buffer

	plan := selfupdate.Plan{
		CurrentVersion: "v1.2.0",
		TargetVersion:  "v1.2.0",
		Direction:      "same",
		UpdateNeeded:   false,
	}

	if reportErr := reportPlan(&out, plan, false); reportErr != nil {
		test.Fatalf("reportPlan returned error: %v", reportErr)
	}

	if !strings.Contains(out.String(), "already on v1.2.0") {
		test.Errorf("output = %q, want an up-to-date message", out.String())
	}
}

func TestReportPlanUpdateAvailable(test *testing.T) {
	var out bytes.Buffer

	plan := selfupdate.Plan{
		CurrentVersion: "v1.2.0",
		TargetVersion:  "v1.3.0",
		Direction:      "newer",
		UpdateNeeded:   true,
		BinaryPath:     "/usr/local/bin/tusk",
		ArchiveName:    "tusk_1.3.0_darwin_arm64.tar.gz",
		InstallMethod:  "release",
	}

	if reportErr := reportPlan(&out, plan, false); reportErr != nil {
		test.Fatalf("reportPlan returned error: %v", reportErr)
	}

	body := out.String()

	for _, want := range []string{"update available", "v1.2.0", "v1.3.0", "/usr/local/bin/tusk", "tusk_1.3.0_darwin_arm64.tar.gz"} {
		if !strings.Contains(body, want) {
			test.Errorf("output missing %q:\n%s", want, body)
		}
	}
}

// TestReportPlanDowngradeIsLabelled asserts a pinned older version is not
// presented as an "update available".
func TestReportPlanDowngradeIsLabelled(test *testing.T) {
	var out bytes.Buffer

	plan := selfupdate.Plan{
		CurrentVersion: "v1.5.0",
		TargetVersion:  "v1.0.0",
		Direction:      "older",
		UpdateNeeded:   true,
		BinaryPath:     "/usr/local/bin/tusk",
		ArchiveName:    "tusk_1.0.0_darwin_arm64.tar.gz",
		InstallMethod:  "release",
	}

	if reportErr := reportPlan(&out, plan, false); reportErr != nil {
		test.Fatalf("reportPlan returned error: %v", reportErr)
	}

	if !strings.Contains(out.String(), "downgrade requested") {
		test.Errorf("output = %q, want it labelled as a downgrade", out.String())
	}
}

func TestReportPlanJSON(test *testing.T) {
	var out bytes.Buffer

	plan := selfupdate.Plan{
		CurrentVersion: "v1.2.0",
		TargetVersion:  "v1.3.0",
		Direction:      "newer",
		UpdateNeeded:   true,
	}

	if reportErr := reportPlan(&out, plan, true); reportErr != nil {
		test.Fatalf("reportPlan returned error: %v", reportErr)
	}

	body := out.String()

	for _, want := range []string{`"current_version": "v1.2.0"`, `"target_version": "v1.3.0"`, `"update_needed": true`} {
		if !strings.Contains(body, want) {
			test.Errorf("JSON output missing %s:\n%s", want, body)
		}
	}
}

func TestReportResultInstalled(test *testing.T) {
	var out bytes.Buffer

	plan := selfupdate.Plan{Direction: "newer", UpdateNeeded: true}
	result := selfupdate.Result{
		PreviousVersion: "v1.2.0",
		NewVersion:      "v1.3.0",
		BinaryPath:      "/usr/local/bin/tusk",
		ManPagesDir:     "/usr/local/share/man",
		Installed:       true,
	}

	if reportErr := reportResult(&out, plan, result, false); reportErr != nil {
		test.Fatalf("reportResult returned error: %v", reportErr)
	}

	body := out.String()

	for _, want := range []string{"installed v1.3.0", "/usr/local/bin/tusk", filepath.Join("/usr/local/share/man", "man1")} {
		if !strings.Contains(body, want) {
			test.Errorf("output missing %q:\n%s", want, body)
		}
	}
}

// TestReportResultManPageNote asserts a best-effort man-page failure is
// surfaced as a note rather than presented as a failed update.
func TestReportResultManPageNote(test *testing.T) {
	var out bytes.Buffer

	plan := selfupdate.Plan{Direction: "newer", UpdateNeeded: true}
	result := selfupdate.Result{
		PreviousVersion: "v1.2.0",
		NewVersion:      "v1.3.0",
		BinaryPath:      "/usr/local/bin/tusk",
		ManPagesNote:    "creating /usr/local/share/man/man1: permission denied",
		Installed:       true,
	}

	if reportErr := reportResult(&out, plan, result, false); reportErr != nil {
		test.Fatalf("reportResult returned error: %v", reportErr)
	}

	body := out.String()

	if !strings.Contains(body, "installed v1.3.0") {
		test.Errorf("output does not report the successful install:\n%s", body)
	}

	if !strings.Contains(body, "man pages skipped") {
		test.Errorf("output does not note the man-page failure:\n%s", body)
	}
}

// TestReportResultDowngradeAnnounced asserts a downgrade says so explicitly,
// so a user who pinned an old tag by mistake sees it.
func TestReportResultDowngradeAnnounced(test *testing.T) {
	var out bytes.Buffer

	plan := selfupdate.Plan{Direction: "older", UpdateNeeded: true}
	result := selfupdate.Result{
		PreviousVersion: "v1.5.0",
		NewVersion:      "v1.0.0",
		BinaryPath:      "/usr/local/bin/tusk",
		Installed:       true,
	}

	if reportErr := reportResult(&out, plan, result, false); reportErr != nil {
		test.Fatalf("reportResult returned error: %v", reportErr)
	}

	if !strings.Contains(out.String(), "downgrading v1.5.0 → v1.0.0") {
		test.Errorf("output = %q, want an explicit downgrade line", out.String())
	}
}

// TestReportResultForcedReinstall asserts a --force reinstall is reported as
// work done, not as "nothing to do" — the binary really was rewritten.
func TestReportResultForcedReinstall(test *testing.T) {
	var out bytes.Buffer

	plan := selfupdate.Plan{Direction: "same", UpdateNeeded: false}
	result := selfupdate.Result{
		PreviousVersion: "v1.2.0",
		NewVersion:      "v1.2.0",
		BinaryPath:      "/usr/local/bin/tusk",
		Installed:       true,
	}

	if reportErr := reportResult(&out, plan, result, false); reportErr != nil {
		test.Fatalf("reportResult returned error: %v", reportErr)
	}

	body := out.String()

	if strings.Contains(body, "nothing to do") {
		test.Errorf("a forced reinstall was reported as a no-op:\n%s", body)
	}

	if !strings.Contains(body, "reinstalling v1.2.0") {
		test.Errorf("output does not report the reinstall:\n%s", body)
	}
}

// TestReportPlanPinnedUpToDate asserts a pinned version that matches is not
// mislabelled "(latest)" — the pin may well be older than the newest release.
func TestReportPlanPinnedUpToDate(test *testing.T) {
	var out bytes.Buffer

	plan := selfupdate.Plan{
		CurrentVersion:   "v1.2.0",
		TargetVersion:    "v1.2.0",
		RequestedVersion: "v1.2.0",
		Direction:        "same",
		UpdateNeeded:     false,
	}

	if reportErr := reportPlan(&out, plan, false); reportErr != nil {
		test.Fatalf("reportPlan returned error: %v", reportErr)
	}

	body := out.String()

	if strings.Contains(body, "latest") {
		test.Errorf("a pinned version was labelled latest:\n%s", body)
	}

	if !strings.Contains(body, "requested v1.2.0") {
		test.Errorf("output does not name the pin:\n%s", body)
	}
}

// TestReportPlanSurfacesRefusal asserts --check warns about an update that
// would be refused, instead of reporting a success the real run cannot deliver.
func TestReportPlanSurfacesRefusal(test *testing.T) {
	var out bytes.Buffer

	plan := selfupdate.Plan{
		CurrentVersion: "v1.0.0-dev",
		TargetVersion:  "v1.3.0",
		Direction:      "newer",
		UpdateNeeded:   true,
		BinaryPath:     "/opt/homebrew/bin/tusk",
		ArchiveName:    "tusk_1.3.0_darwin_arm64.tar.gz",
		InstallMethod:  "homebrew",
		Refused:        true,
		RefusalReason:  "install method: managed by homebrew\n  Upgrade it with:  brew upgrade tusk",
	}

	if reportErr := reportPlan(&out, plan, false); reportErr != nil {
		test.Fatalf("reportPlan returned error: %v", reportErr)
	}

	body := out.String()

	if !strings.Contains(body, "would be refused") {
		test.Errorf("output does not predict the refusal:\n%s", body)
	}

	if !strings.Contains(body, "brew upgrade tusk") {
		test.Errorf("output does not carry the remedy:\n%s", body)
	}
}
