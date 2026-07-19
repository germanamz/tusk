package selfupdate

import "errors"

// Sentinel classes for update failures. Each maps to a distinct process exit
// code in cmd/tusk so scripts can branch on the failure mode; callers match
// with errors.Is rather than by inspecting message text.
var (
	// ErrNetwork covers release resolution and download failures: DNS,
	// connectivity, timeouts, and non-200 responses from GitHub.
	ErrNetwork = errors.New("network")

	// ErrChecksum means the downloaded archive did not match the SHA-256
	// recorded in the release checksums file. Nothing is extracted.
	ErrChecksum = errors.New("checksum verification")

	// ErrInstallMethod means the running binary was not installed from a
	// GitHub release, so replacing it in place would fight the tool that
	// owns it. Bypassed with --force.
	ErrInstallMethod = errors.New("install method")

	// ErrPermission means the binary's directory could not be written:
	// either the preflight probe or the swap itself was denied.
	ErrPermission = errors.New("permission")

	// ErrNoAsset means the release exists but ships no archive for this
	// OS/architecture pair.
	ErrNoAsset = errors.New("no release asset")

	// ErrInvalidVersion means a version string — supplied by the user or
	// reported by a release — is not a plain release tag. It is rejected
	// before reaching a URL or a filesystem path.
	ErrInvalidVersion = errors.New("invalid version")
)
