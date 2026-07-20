// Package selfupdate replaces the running tusk binary with a published
// release build. It resolves a version from GitHub Releases, verifies the
// downloaded archive against the release checksums, and swaps the binary in
// place with rollback on failure.
//
// All logic here is cobra-free so it can be exercised directly in tests;
// cmd/tusk/cmd_update.go is a thin adapter over Plan and Apply.
package selfupdate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Direction reports how a target version relates to the running one.
type Direction int

const (
	// DirectionNewer means the target version is an upgrade.
	DirectionNewer Direction = iota
	// DirectionSame means the target version is already installed.
	DirectionSame
	// DirectionOlder means the target version is a downgrade.
	DirectionOlder
)

// String renders the direction for log and error messages.
func (direction Direction) String() string {
	switch direction {
	case DirectionNewer:
		return "newer"
	case DirectionSame:
		return "same"
	case DirectionOlder:
		return "older"
	}

	return "unknown"
}

// semver is a parsed semantic version. Tusk releases are cut by
// release-please, so they are always MAJOR.MINOR.PATCH with an optional
// prerelease suffix; build metadata is accepted and ignored.
type semver struct {
	major      int
	minor      int
	patch      int
	prerelease string
}

// tagPattern is the only shape a version may take once normalized. Both
// user input and the tag a release reports are checked against it before
// either reaches a URL or a filesystem path: an unvalidated version is a
// path-traversal and URL-injection primitive, since it is concatenated into
// the API endpoint and into the downloaded archive's filename.
var tagPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

// NormalizeVersion canonicalizes a user-supplied version string to the tag
// form used by GitHub releases. It accepts "2.3.0" and "v2.3.0" alike and
// leaves the literal "latest" untouched. The result is not guaranteed
// well-formed — call ValidateTag before using it in a URL or a path.
func NormalizeVersion(raw string) string {
	trimmed := strings.TrimSpace(raw)

	if trimmed == "" || strings.EqualFold(trimmed, LatestVersion) {
		return LatestVersion
	}

	if !strings.HasPrefix(trimmed, "v") {
		return "v" + trimmed
	}

	return trimmed
}

// ValidateTag rejects any version string that is not a plain release tag.
// This is a security boundary, not a convenience check: the value flows into
// the GitHub API path and into the archive filename, so a value containing
// path separators could repoint resolution at another repository or write the
// downloaded body outside the working directory.
func ValidateTag(tag string) error {
	if tagPattern.MatchString(tag) {
		return nil
	}

	return fmt.Errorf("%w: %q is not a release version (want vMAJOR.MINOR.PATCH, or \"latest\")", ErrInvalidVersion, tag)
}

// IsDevVersion reports whether a version string denotes a locally built
// binary rather than a published release. Dev builds keep the
// internal/version fallback, which carries a -dev prerelease.
func IsDevVersion(raw string) bool {
	return strings.Contains(raw, "-dev")
}

// parseSemver reads a version string into its numeric components. Build
// metadata after "+" is discarded: it never participates in precedence.
func parseSemver(raw string) (semver, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(raw), "v")

	if plus := strings.IndexByte(trimmed, '+'); plus >= 0 {
		trimmed = trimmed[:plus]
	}

	var prerelease string

	if dash := strings.IndexByte(trimmed, '-'); dash >= 0 {
		prerelease = trimmed[dash+1:]
		trimmed = trimmed[:dash]
	}

	fields := strings.Split(trimmed, ".")

	if len(fields) != 3 {
		return semver{}, fmt.Errorf("malformed version %q: want MAJOR.MINOR.PATCH", raw)
	}

	numbers := make([]int, 3)

	for index, field := range fields {
		value, convErr := strconv.Atoi(field)

		if convErr != nil || value < 0 {
			return semver{}, fmt.Errorf("malformed version %q: %q is not a number", raw, field)
		}

		numbers[index] = value
	}

	return semver{
		major:      numbers[0],
		minor:      numbers[1],
		patch:      numbers[2],
		prerelease: prerelease,
	}, nil
}

// CompareVersions reports how target relates to current. A version that
// cannot be parsed — a hand-built binary with an odd version string, say —
// yields an error so callers can decide whether to proceed.
func CompareVersions(current string, target string) (Direction, error) {
	currentVer, currentErr := parseSemver(current)

	if currentErr != nil {
		return DirectionNewer, fmt.Errorf("current version: %w", currentErr)
	}

	targetVer, targetErr := parseSemver(target)

	if targetErr != nil {
		return DirectionNewer, fmt.Errorf("target version: %w", targetErr)
	}

	return compareSemver(currentVer, targetVer), nil
}

// compareSemver applies semver precedence: numeric fields first, then
// prerelease, where a version without a prerelease outranks one with it.
func compareSemver(current semver, target semver) Direction {
	for _, pair := range [][2]int{
		{target.major, current.major},
		{target.minor, current.minor},
		{target.patch, current.patch},
	} {
		if pair[0] > pair[1] {
			return DirectionNewer
		}

		if pair[0] < pair[1] {
			return DirectionOlder
		}
	}

	return comparePrerelease(current.prerelease, target.prerelease)
}

// comparePrerelease resolves precedence once the numeric fields are equal.
// Per semver §11.3–11.4, a version without a prerelease outranks one with it,
// and two prereleases compare identifier by identifier rather than as whole
// strings — so rc.9 correctly precedes rc.10, which a byte-wise compare would
// get backwards.
func comparePrerelease(current string, target string) Direction {
	if current == target {
		return DirectionSame
	}

	if current == "" {
		return DirectionOlder
	}

	if target == "" {
		return DirectionNewer
	}

	currentParts := strings.Split(current, ".")
	targetParts := strings.Split(target, ".")

	for index := 0; index < len(currentParts) && index < len(targetParts); index++ {
		if direction := compareIdentifier(currentParts[index], targetParts[index]); direction != DirectionSame {
			return direction
		}
	}

	// Every shared identifier matched, so the longer prerelease wins.
	if len(targetParts) > len(currentParts) {
		return DirectionNewer
	}

	if len(targetParts) < len(currentParts) {
		return DirectionOlder
	}

	return DirectionSame
}

// compareIdentifier applies semver's per-identifier rule: two numeric
// identifiers compare numerically, and a numeric identifier always ranks
// below an alphanumeric one.
func compareIdentifier(current string, target string) Direction {
	currentNum, currentIsNum := strconv.Atoi(current)
	targetNum, targetIsNum := strconv.Atoi(target)

	switch {
	case currentIsNum == nil && targetIsNum == nil:
		if targetNum > currentNum {
			return DirectionNewer
		}

		if targetNum < currentNum {
			return DirectionOlder
		}

		return DirectionSame

	case currentIsNum == nil:
		// Numeric current, alphanumeric target: target outranks it.
		return DirectionNewer

	case targetIsNum == nil:
		return DirectionOlder
	}

	if target > current {
		return DirectionNewer
	}

	if target < current {
		return DirectionOlder
	}

	return DirectionSame
}
