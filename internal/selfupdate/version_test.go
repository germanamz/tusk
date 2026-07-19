package selfupdate

import "testing"

func TestNormalizeVersion(test *testing.T) {
	cases := map[string]string{
		"v2.3.0":  "v2.3.0",
		"2.3.0":   "v2.3.0",
		" 2.3.0 ": "v2.3.0",
		"latest":  "latest",
		"LATEST":  "latest",
		"":        "latest",
	}

	for input, want := range cases {
		if got := NormalizeVersion(input); got != want {
			test.Errorf("NormalizeVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIsDevVersion(test *testing.T) {
	cases := map[string]bool{
		"v1.0.0-dev": true,
		"v1.0.0":     false,
		"v1.2.3-rc1": false,
	}

	for input, want := range cases {
		if got := IsDevVersion(input); got != want {
			test.Errorf("IsDevVersion(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestCompareVersions(test *testing.T) {
	cases := []struct {
		current string
		target  string
		want    Direction
	}{
		{"v1.2.0", "v1.3.0", DirectionNewer},
		{"v1.2.0", "v2.0.0", DirectionNewer},
		{"v1.2.0", "v1.2.1", DirectionNewer},
		{"v1.2.0", "v1.2.0", DirectionSame},
		{"1.2.0", "v1.2.0", DirectionSame},
		{"v1.5.0", "v1.0.0", DirectionOlder},
		{"v2.0.0", "v1.9.9", DirectionOlder},
		// A release outranks its own prereleases.
		{"v1.2.0-rc.1", "v1.2.0", DirectionNewer},
		{"v1.2.0", "v1.2.0-rc.1", DirectionOlder},
		{"v1.2.0-rc.1", "v1.2.0-rc.2", DirectionNewer},
		// Build metadata never affects precedence.
		{"v1.2.0+abc", "v1.2.0+def", DirectionSame},
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

func TestCompareVersionsRejectsMalformed(test *testing.T) {
	cases := []struct {
		current string
		target  string
	}{
		{"not-a-version", "v1.0.0"},
		{"v1.0.0", "banana"},
		{"v1.0", "v1.0.0"},
		{"v1.0.x", "v1.0.0"},
	}

	for _, testCase := range cases {
		if _, err := CompareVersions(testCase.current, testCase.target); err == nil {
			test.Errorf("CompareVersions(%q, %q) = nil error, want a parse failure",
				testCase.current, testCase.target)
		}
	}
}

func TestArchiveName(test *testing.T) {
	cases := []struct {
		version string
		goos    string
		goarch  string
		want    string
	}{
		{"v1.2.0", "darwin", "arm64", "tusk_1.2.0_darwin_arm64.tar.gz"},
		{"1.2.0", "linux", "amd64", "tusk_1.2.0_linux_amd64.tar.gz"},
		{"v1.2.0", "windows", "amd64", "tusk_1.2.0_windows_amd64.zip"},
	}

	for _, testCase := range cases {
		got := ArchiveName(testCase.version, testCase.goos, testCase.goarch)

		if got != testCase.want {
			test.Errorf("ArchiveName(%q, %q, %q) = %q, want %q",
				testCase.version, testCase.goos, testCase.goarch, got, testCase.want)
		}
	}
}

func TestParseChecksums(test *testing.T) {
	body := "" +
		"abc123  tusk_1.2.0_darwin_arm64.tar.gz\n" +
		"def456 *tusk_1.2.0_linux_amd64.tar.gz\n" +
		"garbage-line-with-no-second-field\n" +
		"\n"

	digests := parseChecksums(body)

	if got := digests["tusk_1.2.0_darwin_arm64.tar.gz"]; got != "abc123" {
		test.Errorf("darwin digest = %q, want abc123", got)
	}

	// The binary-mode "*" marker is not part of the filename.
	if got := digests["tusk_1.2.0_linux_amd64.tar.gz"]; got != "def456" {
		test.Errorf("linux digest = %q, want def456", got)
	}

	if len(digests) != 2 {
		test.Errorf("parsed %d entries, want 2 (malformed lines are skipped)", len(digests))
	}
}

func TestVerifyChecksum(test *testing.T) {
	digests := map[string]string{"archive.tar.gz": "ABCDEF"}

	if err := verifyChecksum(digests, "archive.tar.gz", "abcdef"); err != nil {
		test.Errorf("case-insensitive match rejected: %v", err)
	}

	if err := verifyChecksum(digests, "archive.tar.gz", "999999"); err == nil {
		test.Error("digest mismatch accepted, want an error")
	}

	// An archive absent from checksums.txt is unverified, so it must fail
	// exactly as loudly as a mismatch.
	if err := verifyChecksum(digests, "other.tar.gz", "abcdef"); err == nil {
		test.Error("unlisted archive accepted, want an error")
	}
}
