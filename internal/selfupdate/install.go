package selfupdate

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
)

// Method is how the running binary got onto this machine. Only MethodRelease
// is safe to replace in place; the others are owned by a tool that would be
// left inconsistent by a silent overwrite.
type Method int

const (
	// MethodRelease is a binary from a GitHub release archive — installed
	// by install.sh or unpacked by hand. Safe to swap.
	MethodRelease Method = iota
	// MethodHomebrew is a Homebrew-managed binary under a Cellar prefix.
	MethodHomebrew
	// MethodGoInstall is a binary produced by "go install" into GOBIN.
	MethodGoInstall
	// MethodSource is a local build: the version string is still the
	// internal/version dev fallback.
	MethodSource
)

// String names the method for messages.
func (method Method) String() string {
	switch method {
	case MethodRelease:
		return "release"
	case MethodHomebrew:
		return "homebrew"
	case MethodGoInstall:
		return "go install"
	case MethodSource:
		return "source build"
	}

	return "unknown"
}

// UpgradeCommand is the command that actually upgrades a binary installed
// this way. Empty for MethodRelease, which "tusk update" handles itself.
func (method Method) UpgradeCommand() string {
	switch method {
	case MethodHomebrew:
		return "brew upgrade tusk"
	case MethodGoInstall:
		return "go install github.com/germanamz/tusk/cmd/tusk@latest"
	case MethodSource:
		return "git pull && make build"
	}

	return ""
}

// DetectMethod classifies how the binary at execPath was installed.
// currentVersion is the running build's version string.
//
// Only release builds carry an ldflag-injected version, so both `go install`
// and a local `make build` report the dev fallback. Treating a dev version as
// proof of a source build would therefore misclassify every `go install`
// binary and send the user to a git checkout they may not have — so the
// module build info, which does distinguish the two, is consulted first.
func DetectMethod(execPath string, currentVersion string) Method {
	// Homebrew links <prefix>/bin/tusk at <prefix>/Cellar/tusk/<ver>/bin/tusk,
	// so the Cellar segment only appears once symlinks are resolved.
	resolved := execPath

	if link, linkErr := filepath.EvalSymlinks(execPath); linkErr == nil {
		resolved = link
	}

	if containsPathSegment(resolved, "Cellar") || containsPathSegment(resolved, "linuxbrew") {
		return MethodHomebrew
	}

	if !IsDevVersion(currentVersion) {
		return MethodRelease
	}

	// A dev version means this is not a release build. `go install` stamps a
	// real module version into the build info; a local build leaves the
	// "(devel)" placeholder.
	if installedByGoInstall() || inGoBin(resolved) || inGoBin(execPath) {
		return MethodGoInstall
	}

	return MethodSource
}

// installedByGoInstall reports whether the running binary was produced by
// "go install <module>@<version>" rather than built from a local checkout.
//
// The discriminator is the VCS stamp, not the module version. Building a
// tagged repository locally does not yield "(devel)" — Go derives a
// pseudo-version such as v1.18.1-0.20260718191123-fbe70f4e7989+dirty, which
// is indistinguishable from a real one by shape. But a local build is stamped
// with vcs.revision from the checkout it came from, while "go install" builds
// out of the module cache and carries no VCS settings at all.
func installedByGoInstall() bool {
	info, available := debug.ReadBuildInfo()

	if !available {
		return false
	}

	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return false
		}
	}

	return info.Main.Version != "" && info.Main.Version != "(devel)"
}

// containsPathSegment reports whether a path contains an exact directory
// component, so "/opt/homebrew/Cellar/tusk/..." matches but a file merely
// named "Cellarium" does not.
func containsPathSegment(path string, segment string) bool {
	for part := range strings.SplitSeq(filepath.ToSlash(path), "/") {
		if strings.EqualFold(part, segment) {
			return true
		}
	}

	return false
}

// inGoBin reports whether path sits in the directory "go install" writes to:
// GOBIN when set, else GOPATH/bin, else the ~/go/bin default.
func inGoBin(path string) bool {
	dir := filepath.Dir(path)

	candidates := []string{os.Getenv("GOBIN")}

	for _, gopath := range filepath.SplitList(os.Getenv("GOPATH")) {
		if gopath != "" {
			candidates = append(candidates, filepath.Join(gopath, "bin"))
		}
	}

	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		candidates = append(candidates, filepath.Join(home, "go", "bin"))
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}

		if sameDir(dir, candidate) {
			return true
		}
	}

	return false
}

// sameDir compares two directory paths, tolerating symlinks and (on
// case-insensitive filesystems) case differences.
func sameDir(left string, right string) bool {
	leftResolved, leftErr := filepath.EvalSymlinks(left)

	if leftErr != nil {
		leftResolved = left
	}

	rightResolved, rightErr := filepath.EvalSymlinks(right)

	if rightErr != nil {
		rightResolved = right
	}

	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(filepath.Clean(leftResolved), filepath.Clean(rightResolved))
	}

	return filepath.Clean(leftResolved) == filepath.Clean(rightResolved)
}

// MethodRefusal builds the error returned when the detected method is not
// one this command should replace in place.
func MethodRefusal(method Method, execPath string) error {
	return fmt.Errorf(
		"%w: %s looks like it was installed via %s, which owns that file.\n"+
			"  Upgrade it with:  %s\n"+
			"  Or pass --force to replace the binary in place anyway",
		ErrInstallMethod, execPath, method, method.UpgradeCommand())
}
