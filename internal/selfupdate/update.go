package selfupdate

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/germanamz/tusk/internal/version"
)

// Updater performs self-updates. The zero value is usable and targets the
// real GitHub API, the running executable, and this build's version; tests
// override the fields to point at an httptest server and a scratch binary.
type Updater struct {
	// APIBase is the GitHub API root. Empty means DefaultAPIBase.
	APIBase string
	// ExecPath is the binary to replace. Empty means os.Executable.
	ExecPath string
	// CurrentVersion is the running build's version. Empty means the
	// compiled-in version.Current.
	CurrentVersion string
	// GOOS and GOARCH select the release archive. Empty means this build's.
	GOOS   string
	GOARCH string
	// Force bypasses the install-method refusal.
	Force bool
	// SkipManPages suppresses man-page installation.
	SkipManPages bool
}

// Plan is a resolved, not-yet-applied update. It is what --check reports and
// what Apply consumes.
type Plan struct {
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
	// RequestedVersion is what the user asked for, either a tag or the
	// "latest" sentinel. It lets output distinguish "already on the newest
	// release" from "already on the version you pinned".
	RequestedVersion string `json:"requested_version"`
	Direction        string `json:"direction"`
	UpdateNeeded     bool   `json:"update_needed"`
	BinaryPath       string `json:"binary_path"`
	ArchiveName      string `json:"archive_name"`
	InstallMethod    string `json:"install_method"`
	// Refused reports that Apply will reject this plan because another tool
	// manages the binary. Surfaced here so --check predicts the refusal
	// rather than reporting a success the real run cannot deliver.
	Refused bool `json:"refused"`
	// RefusalReason is the message Apply would fail with, empty unless
	// Refused is set.
	RefusalReason string `json:"refusal_reason,omitempty"`
	archiveURL    string
	checksumURL   string
	direction     Direction
	method        Method
}

// Result reports what Apply did.
type Result struct {
	PreviousVersion string `json:"previous_version"`
	NewVersion      string `json:"new_version"`
	BinaryPath      string `json:"binary_path"`
	// Installed distinguishes a real swap from a no-op. It is false only
	// when the target version was already installed and no reinstall was
	// forced.
	Installed   bool   `json:"installed"`
	ManPagesDir string `json:"man_pages_dir,omitempty"`
	// ManPagesNote carries a non-fatal man-page installation failure.
	ManPagesNote string `json:"man_pages_note,omitempty"`
}

func (updater *Updater) apiBase() string {
	if updater.APIBase != "" {
		return updater.APIBase
	}

	return DefaultAPIBase
}

// currentVersion is the running build's version, defaulting to the
// ldflag-injected compile-time value.
func (updater *Updater) currentVersion() string {
	if updater.CurrentVersion != "" {
		return updater.CurrentVersion
	}

	return version.Current
}

func (updater *Updater) goos() string {
	if updater.GOOS != "" {
		return updater.GOOS
	}

	return runtime.GOOS
}

func (updater *Updater) goarch() string {
	if updater.GOARCH != "" {
		return updater.GOARCH
	}

	return runtime.GOARCH
}

// binaryName is the executable's name inside the release archive.
func (updater *Updater) binaryName() string {
	if updater.goos() == "windows" {
		return "tusk.exe"
	}

	return "tusk"
}

// execPath resolves the binary this update would replace.
func (updater *Updater) execPath() (string, error) {
	if updater.ExecPath != "" {
		return resolveTarget(updater.ExecPath)
	}

	own, execErr := os.Executable()

	if execErr != nil {
		return "", fmt.Errorf("locating the running binary: %w", execErr)
	}

	return resolveTarget(own)
}

// Plan resolves the requested version and works out what applying it would
// do, without downloading anything or touching the filesystem.
func (updater *Updater) Plan(ctx context.Context, requested string) (Plan, error) {
	targetPath, pathErr := updater.execPath()

	if pathErr != nil {
		return Plan{}, pathErr
	}

	release, resolveErr := updater.Resolve(ctx, requested)

	if resolveErr != nil {
		return Plan{}, resolveErr
	}

	archiveURL, checksumURL, assetErr := release.AssetURL(updater.goos(), updater.goarch())

	if assetErr != nil {
		return Plan{}, assetErr
	}

	current := updater.currentVersion()

	direction, compareErr := CompareVersions(current, release.Version)

	method := DetectMethod(targetPath, current)

	if compareErr != nil {
		// The running binary reports a version this code cannot parse, so
		// there is no way to know whether the target is an upgrade. Treat it
		// as one, but withhold the automatic go-ahead: an unrecognizable
		// version is itself evidence the binary did not come from a release,
		// so require --force rather than silently overwriting it.
		direction = DirectionNewer

		if method == MethodRelease {
			method = MethodSource
		}
	}

	plan := Plan{
		CurrentVersion:   current,
		TargetVersion:    release.Version,
		RequestedVersion: NormalizeVersion(requested),
		Direction:        direction.String(),
		UpdateNeeded:     direction != DirectionSame,
		BinaryPath:       targetPath,
		ArchiveName:      ArchiveName(release.Version, updater.goos(), updater.goarch()),
		InstallMethod:    method.String(),
		archiveURL:       archiveURL,
		checksumURL:      checksumURL,
		direction:        direction,
		method:           method,
	}

	if method != MethodRelease && !updater.Force {
		plan.Refused = true
		plan.RefusalReason = MethodRefusal(method, targetPath).Error()
	}

	return plan, nil
}

// Apply downloads, verifies, and installs the release described by plan. It
// is a no-op when the target version is already installed.
//
// Ordering matters: the install-method refusal and the writability probe both
// run before any network transfer, so a refusal costs nothing. The checksum
// is verified before extraction, and extraction completes before the running
// binary is touched at all.
func (updater *Updater) Apply(ctx context.Context, plan Plan) (Result, error) {
	// --force doubles as "reinstall": without it, re-running on the current
	// version is a no-op, but a user repairing a corrupted binary needs a way
	// to force the download through.
	if plan.direction == DirectionSame && !updater.Force {
		return Result{
			PreviousVersion: plan.CurrentVersion,
			NewVersion:      plan.TargetVersion,
			BinaryPath:      plan.BinaryPath,
		}, nil
	}

	if plan.Refused {
		return Result{}, MethodRefusal(plan.method, plan.BinaryPath)
	}

	if probeErr := preflightWritable(plan.BinaryPath); probeErr != nil {
		return Result{}, probeErr
	}

	workDir, tempErr := os.MkdirTemp("", "tusk-update-*")

	if tempErr != nil {
		return Result{}, fmt.Errorf("creating a working directory: %w", tempErr)
	}

	defer func() { _ = os.RemoveAll(workDir) }()

	archivePath, digest, downloadErr := updater.downloadArchive(ctx, plan.archiveURL, workDir, plan.ArchiveName)

	if downloadErr != nil {
		return Result{}, downloadErr
	}

	digests, checksumErr := updater.fetchChecksums(ctx, plan.checksumURL)

	if checksumErr != nil {
		return Result{}, checksumErr
	}

	if verifyErr := verifyChecksum(digests, plan.ArchiveName, digest); verifyErr != nil {
		return Result{}, verifyErr
	}

	unpacked, extractErr := extractArchive(archivePath, workDir, updater.binaryName())

	if extractErr != nil {
		return Result{}, extractErr
	}

	stagedPath, stageErr := stageBinary(unpacked.binaryPath, plan.BinaryPath)

	if stageErr != nil {
		return Result{}, stageErr
	}

	if swapErr := swapBinary(stagedPath, plan.BinaryPath); swapErr != nil {
		return Result{}, swapErr
	}

	result := Result{
		PreviousVersion: plan.CurrentVersion,
		NewVersion:      plan.TargetVersion,
		BinaryPath:      plan.BinaryPath,
		Installed:       true,
	}

	if updater.SkipManPages {
		return result, nil
	}

	manDir := ManDirFor(plan.BinaryPath)

	if manErr := installManPages(unpacked.manPages, manDir); manErr != nil {
		result.ManPagesNote = manErr.Error()

		return result, nil
	}

	if len(unpacked.manPages) > 0 {
		result.ManPagesDir = manDir
	}

	return result, nil
}
