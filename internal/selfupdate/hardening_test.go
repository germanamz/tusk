package selfupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestValidateTagRejectsPathInjection covers the security boundary: a version
// is concatenated into an API URL and into a download filename, so anything
// carrying separators or dot segments must be rejected outright.
func TestValidateTagRejectsPathInjection(test *testing.T) {
	hostile := []string{
		"v/../../attacker/evil/releases/latest",
		"v1.0.0/../../../../etc/passwd",
		"v1.0.0/..",
		"v1.0.0%2F..",
		"v1.0.0?x=1",
		"v1.0.0#frag",
		"v1.0.0 v2.0.0",
		"../v1.0.0",
		"latest",
		"v1.0",
		"",
	}

	for _, tag := range hostile {
		if err := ValidateTag(tag); err == nil {
			test.Errorf("ValidateTag(%q) = nil, want rejection", tag)
		}
	}

	valid := []string{"v1.0.0", "v2.3.0", "v1.2.3-rc.1", "v1.2.3+build.5", "v10.20.30"}

	for _, tag := range valid {
		if err := ValidateTag(tag); err != nil {
			test.Errorf("ValidateTag(%q) = %v, want nil", tag, err)
		}
	}
}

// TestResolveRejectsHostileVersion asserts the rejection happens before any
// request is issued, so a crafted version can never reach the network layer.
func TestResolveRejectsHostileVersion(test *testing.T) {
	release := newFakeRelease(test, "v1.3.0", "binary")
	base := startReleaseServer(test, release)

	updater := &Updater{
		APIBase:        base,
		ExecPath:       installedBinary(test, "current"),
		CurrentVersion: "v1.2.0",
	}

	_, err := updater.Resolve(context.Background(), "v/../../attacker/evil/releases/latest")

	if !errors.Is(err, ErrInvalidVersion) {
		test.Fatalf("Resolve error = %v, want ErrInvalidVersion", err)
	}
}

// TestResolveRejectsHostileServerTag asserts a compromised or spoofed release
// response cannot steer the download path. The tag flows into the archive
// filename, which is a filesystem write.
func TestResolveRejectsHostileServerTag(test *testing.T) {
	release := newFakeRelease(test, "v1.3.0", "binary")
	release.version = "v1.0.0/../../../../../../tmp/pwned"

	base := startReleaseServer(test, release)

	updater := &Updater{
		APIBase:        base,
		ExecPath:       installedBinary(test, "current"),
		CurrentVersion: "v1.2.0",
	}

	_, err := updater.Resolve(context.Background(), LatestVersion)

	if !errors.Is(err, ErrNetwork) {
		test.Fatalf("Resolve error = %v, want ErrNetwork wrapping a tag rejection", err)
	}

	if !strings.Contains(err.Error(), "unusable tag") {
		test.Errorf("error = %v, want it to name the tag as the problem", err)
	}
}

// TestExtractRejectsNestedBinaryEntry asserts the binary must sit at the
// archive root. Matching on base name alone would let a nested entry named
// tusk overwrite the real binary, with archive ordering deciding the winner.
func TestExtractRejectsNestedBinaryEntry(test *testing.T) {
	dir := test.TempDir()
	archivePath := filepath.Join(dir, "release.tar.gz")

	archive := buildTarGz(test, map[string]string{
		"tusk":      "REAL BINARY",
		"docs/tusk": "IMPOSTOR",
	})

	if writeErr := os.WriteFile(archivePath, archive, 0o644); writeErr != nil {
		test.Fatalf("writing archive: %v", writeErr)
	}

	unpacked, extractErr := extractArchive(archivePath, test.TempDir(), "tusk")

	if extractErr != nil {
		test.Fatalf("extractArchive returned error: %v", extractErr)
	}

	content, readErr := os.ReadFile(unpacked.binaryPath)

	if readErr != nil {
		test.Fatalf("reading extracted binary: %v", readErr)
	}

	if string(content) != "REAL BINARY" {
		test.Errorf("extracted binary = %q, want the root entry to win", content)
	}
}

// TestExtractRejectsDuplicateBinary asserts two root-level binary entries are
// an error rather than a last-one-wins race.
func TestExtractRejectsDuplicateBinary(test *testing.T) {
	dir := test.TempDir()
	archivePath := filepath.Join(dir, "release.tar.gz")

	// buildTarGz takes a map, so duplicate keys are impossible; write the
	// archive with two identical entry names directly.
	archive := buildTarGzEntries(test, [][2]string{
		{"tusk", "FIRST"},
		{"tusk", "SECOND"},
	})

	if writeErr := os.WriteFile(archivePath, archive, 0o644); writeErr != nil {
		test.Fatalf("writing archive: %v", writeErr)
	}

	if _, extractErr := extractArchive(archivePath, test.TempDir(), "tusk"); extractErr == nil {
		test.Fatal("extractArchive accepted a duplicate binary entry, want an error")
	}
}

// TestSwapBackupIsPerRun asserts two updates cannot contend for one backup
// slot, which would destroy each other's rollback copy.
func TestSwapBackupIsPerRun(test *testing.T) {
	dir := test.TempDir()
	target := filepath.Join(dir, "tusk")

	if writeErr := os.WriteFile(target, []byte("OLD"), 0o755); writeErr != nil {
		test.Fatalf("writing target: %v", writeErr)
	}

	// A backup left by a different process must not block this update.
	foreign := target + backupSuffix + ".999999"

	if writeErr := os.WriteFile(foreign, []byte("FOREIGN"), 0o755); writeErr != nil {
		test.Fatalf("writing foreign backup: %v", writeErr)
	}

	source := filepath.Join(test.TempDir(), "tusk-new")

	if writeErr := os.WriteFile(source, []byte("NEW"), 0o755); writeErr != nil {
		test.Fatalf("writing replacement: %v", writeErr)
	}

	staged, stageErr := stageBinary(source, target)

	if stageErr != nil {
		test.Fatalf("stageBinary returned error: %v", stageErr)
	}

	if swapErr := swapBinary(staged, target); swapErr != nil {
		test.Fatalf("swapBinary returned error: %v", swapErr)
	}

	content, readErr := os.ReadFile(target)

	if readErr != nil {
		test.Fatalf("reading target: %v", readErr)
	}

	if string(content) != "NEW" {
		test.Errorf("target = %q, want NEW", content)
	}
}

// TestSweepLeftoversSparesFreshStaging asserts a concurrent update's in-flight
// staging file is not ripped out from under it.
func TestSweepLeftoversSparesFreshStaging(test *testing.T) {
	dir := test.TempDir()
	target := filepath.Join(dir, "tusk")

	if writeErr := os.WriteFile(target, []byte("OLD"), 0o755); writeErr != nil {
		test.Fatalf("writing target: %v", writeErr)
	}

	fresh := filepath.Join(dir, stagePrefix+"inflight")

	if writeErr := os.WriteFile(fresh, []byte("IN FLIGHT"), 0o755); writeErr != nil {
		test.Fatalf("writing fresh staging file: %v", writeErr)
	}

	stale := filepath.Join(dir, stagePrefix+"abandoned")

	if writeErr := os.WriteFile(stale, []byte("ABANDONED"), 0o755); writeErr != nil {
		test.Fatalf("writing stale staging file: %v", writeErr)
	}

	old := time.Now().Add(-2 * stagingGrace)

	if chtimesErr := os.Chtimes(stale, old, old); chtimesErr != nil {
		test.Fatalf("ageing stale file: %v", chtimesErr)
	}

	sweepLeftovers(target)

	if _, statErr := os.Stat(fresh); statErr != nil {
		test.Errorf("fresh staging file was swept: %v", statErr)
	}

	if _, statErr := os.Stat(stale); !errors.Is(statErr, os.ErrNotExist) {
		test.Error("stale staging file survived the sweep")
	}
}

// TestStageBinaryPreservesRestrictiveMode asserts a deliberately restricted
// install is not silently widened to 0755 by updating it.
func TestStageBinaryPreservesRestrictiveMode(test *testing.T) {
	if os.Geteuid() == 0 {
		test.Skip("root ignores permission bits")
	}

	dir := test.TempDir()
	target := filepath.Join(dir, "tusk")

	if writeErr := os.WriteFile(target, []byte("OLD"), 0o750); writeErr != nil {
		test.Fatalf("writing target: %v", writeErr)
	}

	source := filepath.Join(test.TempDir(), "tusk-new")

	if writeErr := os.WriteFile(source, []byte("NEW"), 0o644); writeErr != nil {
		test.Fatalf("writing replacement: %v", writeErr)
	}

	staged, stageErr := stageBinary(source, target)

	if stageErr != nil {
		test.Fatalf("stageBinary returned error: %v", stageErr)
	}

	info, statErr := os.Stat(staged)

	if statErr != nil {
		test.Fatalf("stat staged file: %v", statErr)
	}

	if info.Mode().Perm() != 0o750 {
		test.Errorf("staged mode = %v, want 0750 carried over from the target", info.Mode().Perm())
	}
}

// TestPlanSurfacesRefusal asserts --check predicts the refusal instead of
// reporting an update that the real run is guaranteed to reject.
func TestPlanSurfacesRefusal(test *testing.T) {
	release := newFakeRelease(test, "v1.3.0", "binary")
	base := startReleaseServer(test, release)

	updater := &Updater{
		APIBase:        base,
		ExecPath:       installedBinary(test, "current"),
		CurrentVersion: "v1.0.0-dev",
	}

	plan, planErr := updater.Plan(context.Background(), "latest")

	if planErr != nil {
		test.Fatalf("Plan returned error: %v", planErr)
	}

	if !plan.Refused {
		test.Fatal("Plan.Refused = false, want true for a non-release install")
	}

	if !strings.Contains(plan.RefusalReason, "--force") {
		test.Errorf("RefusalReason = %q, want it to mention --force", plan.RefusalReason)
	}
}

// TestPlanForceClearsRefusal asserts --force is reflected at plan time, so
// --check --force does not warn about a refusal that will not happen.
func TestPlanForceClearsRefusal(test *testing.T) {
	release := newFakeRelease(test, "v1.3.0", "binary")
	base := startReleaseServer(test, release)

	updater := &Updater{
		APIBase:        base,
		ExecPath:       installedBinary(test, "current"),
		CurrentVersion: "v1.0.0-dev",
		Force:          true,
	}

	plan, planErr := updater.Plan(context.Background(), "latest")

	if planErr != nil {
		test.Fatalf("Plan returned error: %v", planErr)
	}

	if plan.Refused {
		test.Error("Plan.Refused = true with Force set, want false")
	}
}

// TestApplyForceReinstallsSameVersion asserts --force doubles as a repair
// path: re-running on the current version must actually reinstall.
func TestApplyForceReinstallsSameVersion(test *testing.T) {
	release := newFakeRelease(test, "v1.2.0", "PRISTINE BINARY")
	base := startReleaseServer(test, release)

	target := installedBinary(test, "CORRUPTED")

	updater := &Updater{
		APIBase:        base,
		ExecPath:       target,
		CurrentVersion: "v1.2.0",
		Force:          true,
		SkipManPages:   true,
	}

	plan, planErr := updater.Plan(context.Background(), "latest")

	if planErr != nil {
		test.Fatalf("Plan returned error: %v", planErr)
	}

	if _, applyErr := updater.Apply(context.Background(), plan); applyErr != nil {
		test.Fatalf("Apply returned error: %v", applyErr)
	}

	content, readErr := os.ReadFile(target)

	if readErr != nil {
		test.Fatalf("reading binary: %v", readErr)
	}

	if string(content) != "PRISTINE BINARY" {
		test.Errorf("binary = %q, want --force to have reinstalled the release", content)
	}
}

// TestPlanRecordsRequestedVersion asserts output can tell "already on the
// newest release" apart from "already on the version you pinned".
func TestPlanRecordsRequestedVersion(test *testing.T) {
	release := newFakeRelease(test, "v1.2.0", "binary")
	base := startReleaseServer(test, release)

	updater := &Updater{
		APIBase:        base,
		ExecPath:       installedBinary(test, "current"),
		CurrentVersion: "v1.2.0",
	}

	pinned, pinnedErr := updater.Plan(context.Background(), "1.2.0")

	if pinnedErr != nil {
		test.Fatalf("Plan returned error: %v", pinnedErr)
	}

	if pinned.RequestedVersion != "v1.2.0" {
		test.Errorf("RequestedVersion = %q, want the normalized pin v1.2.0", pinned.RequestedVersion)
	}

	latest, latestErr := updater.Plan(context.Background(), "latest")

	if latestErr != nil {
		test.Fatalf("Plan returned error: %v", latestErr)
	}

	if latest.RequestedVersion != LatestVersion {
		test.Errorf("RequestedVersion = %q, want %q", latest.RequestedVersion, LatestVersion)
	}
}

// TestPrereleaseIdentifierPrecedence covers semver's per-identifier rule,
// which a byte-wise string compare gets wrong for rc.9 vs rc.10.
func TestPrereleaseIdentifierPrecedence(test *testing.T) {
	cases := []struct {
		current string
		target  string
		want    Direction
	}{
		{"v1.0.0-rc.9", "v1.0.0-rc.10", DirectionNewer},
		{"v1.0.0-rc.10", "v1.0.0-rc.9", DirectionOlder},
		{"v1.0.0-rc.2", "v1.0.0-rc.10", DirectionNewer},
		{"v1.0.0-alpha", "v1.0.0-alpha.1", DirectionNewer},
		{"v1.0.0-alpha.1", "v1.0.0-beta", DirectionNewer},
		{"v1.0.0-beta", "v1.0.0-alpha.1", DirectionOlder},
		// A numeric identifier ranks below an alphanumeric one.
		{"v1.0.0-1", "v1.0.0-alpha", DirectionNewer},
		{"v1.0.0-alpha", "v1.0.0-1", DirectionOlder},
	}

	for _, testCase := range cases {
		got, err := CompareVersions(testCase.current, testCase.target)

		if err != nil {
			test.Errorf("CompareVersions(%q, %q) returned error: %v", testCase.current, testCase.target, err)

			continue
		}

		if got != testCase.want {
			test.Errorf("CompareVersions(%q, %q) = %v, want %v",
				testCase.current, testCase.target, got, testCase.want)
		}
	}
}

// TestApplyReportsInstalledFlag asserts a no-op and a forced reinstall are
// distinguishable, so output cannot claim "nothing to do" for a run that
// rewrote the binary.
func TestApplyReportsInstalledFlag(test *testing.T) {
	release := newFakeRelease(test, "v1.2.0", "RELEASE")
	base := startReleaseServer(test, release)

	noop := &Updater{
		APIBase:        base,
		ExecPath:       installedBinary(test, "current"),
		CurrentVersion: "v1.2.0",
		SkipManPages:   true,
	}

	noopPlan, noopPlanErr := noop.Plan(context.Background(), "latest")

	if noopPlanErr != nil {
		test.Fatalf("Plan returned error: %v", noopPlanErr)
	}

	noopResult, noopErr := noop.Apply(context.Background(), noopPlan)

	if noopErr != nil {
		test.Fatalf("Apply returned error: %v", noopErr)
	}

	if noopResult.Installed {
		test.Error("Installed = true for a no-op run, want false")
	}

	forced := &Updater{
		APIBase:        base,
		ExecPath:       installedBinary(test, "current"),
		CurrentVersion: "v1.2.0",
		Force:          true,
		SkipManPages:   true,
	}

	forcedPlan, forcedPlanErr := forced.Plan(context.Background(), "latest")

	if forcedPlanErr != nil {
		test.Fatalf("Plan returned error: %v", forcedPlanErr)
	}

	forcedResult, forcedErr := forced.Apply(context.Background(), forcedPlan)

	if forcedErr != nil {
		test.Fatalf("Apply returned error: %v", forcedErr)
	}

	if !forcedResult.Installed {
		test.Error("Installed = false for a forced reinstall, want true")
	}
}

// TestInstalledByGoInstallRejectsLocalBuild guards the discriminator that
// matters: this test binary is built from a checkout, so it carries a
// vcs.revision stamp and must never be classified as a go install — even
// though Go gives it a real-looking pseudo-version rather than "(devel)".
func TestInstalledByGoInstallRejectsLocalBuild(test *testing.T) {
	if installedByGoInstall() {
		test.Error("installedByGoInstall() = true for a VCS-stamped local build, want false")
	}
}
