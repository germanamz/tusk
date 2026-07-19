package selfupdate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectMethodSourceBuild(test *testing.T) {
	// Point the go-install probes somewhere unrelated so a local build in a
	// scratch directory cannot be mistaken for one.
	test.Setenv("GOBIN", test.TempDir())
	test.Setenv("GOPATH", test.TempDir())

	got := DetectMethod("/opt/tools/tusk", "v1.0.0-dev")

	if got != MethodSource {
		test.Errorf("DetectMethod with dev version = %v, want MethodSource", got)
	}
}

// TestDetectMethodHomebrewBeatsDevVersion asserts a Cellar path is classified
// as Homebrew even when the version is the dev fallback. A formula that builds
// from source produces exactly that combination, and Homebrew still owns the
// file — telling such a user to "git pull && make build" would be wrong.
func TestDetectMethodHomebrewBeatsDevVersion(test *testing.T) {
	got := DetectMethod("/opt/homebrew/Cellar/tusk/1.2.0/bin/tusk", "v1.0.0-dev")

	if got != MethodHomebrew {
		test.Errorf("DetectMethod(Cellar path, dev version) = %v, want MethodHomebrew", got)
	}
}

func TestDetectMethodHomebrew(test *testing.T) {
	cases := []string{
		"/opt/homebrew/Cellar/tusk/1.2.0/bin/tusk",
		"/usr/local/Cellar/tusk/1.2.0/bin/tusk",
		"/home/linuxbrew/.linuxbrew/bin/tusk",
	}

	for _, path := range cases {
		if got := DetectMethod(path, "v1.2.0"); got != MethodHomebrew {
			test.Errorf("DetectMethod(%q) = %v, want MethodHomebrew", path, got)
		}
	}
}

// TestDetectMethodGoInstall uses the dev version deliberately: "go install"
// applies no ldflags, so such a binary reports the internal/version fallback,
// never a real release version. A test asserting on "v1.2.0" here would be
// asserting on a state that cannot occur.
func TestDetectMethodGoInstall(test *testing.T) {
	gobin := test.TempDir()

	test.Setenv("GOBIN", gobin)

	path := filepath.Join(gobin, "tusk")

	if writeErr := os.WriteFile(path, []byte("binary"), 0o755); writeErr != nil {
		test.Fatalf("writing stand-in binary: %v", writeErr)
	}

	if got := DetectMethod(path, "v1.0.0-dev"); got != MethodGoInstall {
		test.Errorf("DetectMethod(%q) with GOBIN=%q = %v, want MethodGoInstall", path, gobin, got)
	}
}

// TestDetectMethodReleaseVersionWins asserts that an ldflag-stamped version is
// conclusive: only release builds carry one, so no path heuristic should
// override it.
func TestDetectMethodReleaseVersionWins(test *testing.T) {
	gobin := test.TempDir()

	test.Setenv("GOBIN", gobin)

	path := filepath.Join(gobin, "tusk")

	if writeErr := os.WriteFile(path, []byte("binary"), 0o755); writeErr != nil {
		test.Fatalf("writing stand-in binary: %v", writeErr)
	}

	if got := DetectMethod(path, "v1.2.0"); got != MethodRelease {
		test.Errorf("DetectMethod(%q) with a release version = %v, want MethodRelease", path, got)
	}
}

func TestDetectMethodRelease(test *testing.T) {
	// Point GOBIN somewhere unrelated so the default ~/go/bin probe cannot
	// accidentally match the temp dir.
	test.Setenv("GOBIN", test.TempDir())
	test.Setenv("GOPATH", test.TempDir())

	dir := test.TempDir()
	path := filepath.Join(dir, "tusk")

	if writeErr := os.WriteFile(path, []byte("binary"), 0o755); writeErr != nil {
		test.Fatalf("writing stand-in binary: %v", writeErr)
	}

	if got := DetectMethod(path, "v1.2.0"); got != MethodRelease {
		test.Errorf("DetectMethod(%q) = %v, want MethodRelease", path, got)
	}
}

// TestContainsPathSegmentIsExact guards against a substring match treating
// an unrelated directory as a Homebrew prefix.
func TestContainsPathSegmentIsExact(test *testing.T) {
	if containsPathSegment("/home/me/Cellarium/bin/tusk", "Cellar") {
		test.Error("Cellarium matched the Cellar segment, want an exact component match")
	}

	if !containsPathSegment("/opt/homebrew/Cellar/tusk/bin/tusk", "Cellar") {
		test.Error("a real Cellar path did not match")
	}
}

func TestMethodUpgradeCommand(test *testing.T) {
	cases := map[Method]string{
		MethodHomebrew:  "brew upgrade tusk",
		MethodGoInstall: "go install github.com/germanamz/tusk/cmd/tusk@latest",
		MethodSource:    "git pull && make build",
		MethodRelease:   "",
	}

	for method, want := range cases {
		if got := method.UpgradeCommand(); got != want {
			test.Errorf("%v.UpgradeCommand() = %q, want %q", method, got, want)
		}
	}
}

// TestMethodRefusalNamesTheCommand asserts the refusal tells the user how to
// actually upgrade, which is the entire point of refusing.
func TestMethodRefusalNamesTheCommand(test *testing.T) {
	refusal := MethodRefusal(MethodHomebrew, "/opt/homebrew/bin/tusk")

	if !errors.Is(refusal, ErrInstallMethod) {
		test.Errorf("refusal does not wrap ErrInstallMethod: %v", refusal)
	}

	for _, want := range []string{"brew upgrade tusk", "/opt/homebrew/bin/tusk", "--force"} {
		if !strings.Contains(refusal.Error(), want) {
			test.Errorf("refusal message missing %q:\n%s", want, refusal)
		}
	}
}

func TestManDirForBinPrefix(test *testing.T) {
	test.Setenv("MAN_DIR", "")

	got := ManDirFor("/usr/local/bin/tusk")
	want := filepath.Join("/usr/local", "share", "man")

	if got != want {
		test.Errorf("ManDirFor(/usr/local/bin/tusk) = %q, want %q", got, want)
	}
}

func TestManDirForRespectsOverride(test *testing.T) {
	test.Setenv("MAN_DIR", "/custom/man")

	if got := ManDirFor("/usr/local/bin/tusk"); got != "/custom/man" {
		test.Errorf("ManDirFor with MAN_DIR set = %q, want /custom/man", got)
	}
}

func TestManDirForNonBinFallsBackHome(test *testing.T) {
	test.Setenv("MAN_DIR", "")

	got := ManDirFor("/opt/tools/tusk")

	home, homeErr := os.UserHomeDir()

	if homeErr != nil {
		test.Skip("no home directory available")
	}

	want := filepath.Join(home, ".local", "share", "man")

	if got != want {
		test.Errorf("ManDirFor(/opt/tools/tusk) = %q, want %q", got, want)
	}
}
