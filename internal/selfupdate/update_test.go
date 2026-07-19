package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeRelease is a synthetic GitHub release served over httptest: a tar.gz
// holding a stand-in binary plus a man page, and a checksums.txt covering it.
type fakeRelease struct {
	version     string
	archiveName string
	archive     []byte
	checksums   string
	// corruptChecksum records a digest that does not match the archive, to
	// exercise the verification failure path.
	corruptChecksum bool
}

// newFakeRelease builds a release archive whose binary prints wantOutput, so
// tests can assert the swapped-in file is the one that was downloaded.
func newFakeRelease(harness testing.TB, version string, payload string) *fakeRelease {
	harness.Helper()

	archiveName := ArchiveName(version, runtime.GOOS, runtime.GOARCH)
	archive := buildTarGz(harness, map[string]string{
		"tusk":         payload,
		"man/tusk.1":   ".TH TUSK 1\n",
		"LICENSE":      "license text\n",
		"README.md":    "readme\n",
		"man/tusk-x.1": ".TH TUSK-X 1\n",
	})

	digest := sha256.Sum256(archive)

	return &fakeRelease{
		version:     version,
		archiveName: archiveName,
		archive:     archive,
		checksums:   fmt.Sprintf("%s  %s\n", hex.EncodeToString(digest[:]), archiveName),
	}
}

// buildTarGz assembles an in-memory tar.gz from a name-to-content map.
func buildTarGz(harness testing.TB, entries map[string]string) []byte {
	harness.Helper()

	ordered := make([][2]string, 0, len(entries))

	for name, content := range entries {
		ordered = append(ordered, [2]string{name, content})
	}

	return buildTarGzEntries(harness, ordered)
}

// buildTarGzEntries is the ordered form, used where entry order matters or
// where duplicate entry names are the point of the test.
func buildTarGzEntries(harness testing.TB, entries [][2]string) []byte {
	harness.Helper()

	var buffer bytes.Buffer

	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)

	for _, entry := range entries {
		name, content := entry[0], entry[1]
		header := &tar.Header{
			Name:     name,
			Mode:     0o755,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}

		if writeErr := tarWriter.WriteHeader(header); writeErr != nil {
			harness.Fatalf("writing tar header for %s: %v", name, writeErr)
		}

		if _, writeErr := tarWriter.Write([]byte(content)); writeErr != nil {
			harness.Fatalf("writing tar body for %s: %v", name, writeErr)
		}
	}

	if closeErr := tarWriter.Close(); closeErr != nil {
		harness.Fatalf("closing tar writer: %v", closeErr)
	}

	if closeErr := gzipWriter.Close(); closeErr != nil {
		harness.Fatalf("closing gzip writer: %v", closeErr)
	}

	return buffer.Bytes()
}

// startReleaseServer serves the GitHub endpoints an update touches. The
// returned base URL is what Updater.APIBase is pointed at.
func startReleaseServer(harness testing.TB, releases ...*fakeRelease) string {
	harness.Helper()

	byVersion := make(map[string]*fakeRelease, len(releases))

	for _, release := range releases {
		byVersion[release.version] = release
	}

	// The last release listed is treated as "latest".
	latest := releases[len(releases)-1]

	var server *httptest.Server

	mux := http.NewServeMux()

	writeRelease := func(writer http.ResponseWriter, release *fakeRelease) {
		payload := map[string]any{
			"tag_name": release.version,
			"assets": []map[string]string{
				{
					"name":                 release.archiveName,
					"browser_download_url": server.URL + "/download/" + release.version + "/" + release.archiveName,
				},
				{
					"name":                 ChecksumsAsset,
					"browser_download_url": server.URL + "/download/" + release.version + "/" + ChecksumsAsset,
				},
			},
		}

		writer.Header().Set("Content-Type", "application/json")

		if encodeErr := json.NewEncoder(writer).Encode(payload); encodeErr != nil {
			harness.Errorf("encoding release payload: %v", encodeErr)
		}
	}

	mux.HandleFunc("/releases/latest", func(writer http.ResponseWriter, _ *http.Request) {
		writeRelease(writer, latest)
	})

	mux.HandleFunc("/releases/tags/", func(writer http.ResponseWriter, request *http.Request) {
		tag := filepath.Base(request.URL.Path)

		release, known := byVersion[tag]

		if !known {
			writer.WriteHeader(http.StatusNotFound)

			return
		}

		writeRelease(writer, release)
	})

	mux.HandleFunc("/download/", func(writer http.ResponseWriter, request *http.Request) {
		name := filepath.Base(request.URL.Path)
		version := filepath.Base(filepath.Dir(request.URL.Path))

		release, known := byVersion[version]

		if !known {
			writer.WriteHeader(http.StatusNotFound)

			return
		}

		if name == ChecksumsAsset {
			body := release.checksums

			if release.corruptChecksum {
				body = fmt.Sprintf("%064d  %s\n", 0, release.archiveName)
			}

			_, _ = writer.Write([]byte(body))

			return
		}

		_, _ = writer.Write(release.archive)
	})

	server = httptest.NewServer(mux)

	harness.Cleanup(server.Close)

	return server.URL
}

// installedBinary lays down a stand-in for the running tusk binary in a
// scratch directory and returns its path.
func installedBinary(harness testing.TB, content string) string {
	harness.Helper()

	dir := harness.TempDir()
	path := filepath.Join(dir, "bin", "tusk")

	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o755); mkdirErr != nil {
		harness.Fatalf("creating bin dir: %v", mkdirErr)
	}

	if writeErr := os.WriteFile(path, []byte(content), 0o755); writeErr != nil {
		harness.Fatalf("writing stand-in binary: %v", writeErr)
	}

	return path
}

func TestPlanResolvesLatest(test *testing.T) {
	release := newFakeRelease(test, "v1.3.0", "new binary")
	base := startReleaseServer(test, release)

	updater := &Updater{
		APIBase:        base,
		ExecPath:       installedBinary(test, "old binary"),
		CurrentVersion: "v1.2.0",
	}

	plan, err := updater.Plan(context.Background(), "latest")

	if err != nil {
		test.Fatalf("Plan returned error: %v", err)
	}

	if plan.TargetVersion != "v1.3.0" {
		test.Errorf("TargetVersion = %q, want v1.3.0", plan.TargetVersion)
	}

	if !plan.UpdateNeeded {
		test.Error("UpdateNeeded = false, want true")
	}

	if plan.Direction != "newer" {
		test.Errorf("Direction = %q, want newer", plan.Direction)
	}
}

func TestPlanResolvesPinnedTag(test *testing.T) {
	older := newFakeRelease(test, "v1.0.0", "old release")
	newer := newFakeRelease(test, "v1.3.0", "new release")
	base := startReleaseServer(test, older, newer)

	updater := &Updater{
		APIBase:        base,
		ExecPath:       installedBinary(test, "current"),
		CurrentVersion: "v1.2.0",
	}

	// A bare version must normalize to the v-prefixed tag.
	plan, err := updater.Plan(context.Background(), "1.0.0")

	if err != nil {
		test.Fatalf("Plan returned error: %v", err)
	}

	if plan.TargetVersion != "v1.0.0" {
		test.Errorf("TargetVersion = %q, want v1.0.0", plan.TargetVersion)
	}

	if plan.Direction != "older" {
		test.Errorf("Direction = %q, want older (this is a downgrade)", plan.Direction)
	}

	if !plan.UpdateNeeded {
		test.Error("UpdateNeeded = false, want true for a downgrade")
	}
}

func TestPlanUnknownTagIsNetworkError(test *testing.T) {
	release := newFakeRelease(test, "v1.3.0", "binary")
	base := startReleaseServer(test, release)

	updater := &Updater{
		APIBase:        base,
		ExecPath:       installedBinary(test, "current"),
		CurrentVersion: "v1.2.0",
	}

	_, err := updater.Plan(context.Background(), "v9.9.9")

	if !errors.Is(err, ErrNetwork) {
		test.Fatalf("Plan error = %v, want ErrNetwork", err)
	}
}

func TestPlanSameVersionNeedsNoUpdate(test *testing.T) {
	release := newFakeRelease(test, "v1.2.0", "binary")
	base := startReleaseServer(test, release)

	updater := &Updater{
		APIBase:        base,
		ExecPath:       installedBinary(test, "current"),
		CurrentVersion: "v1.2.0",
	}

	plan, err := updater.Plan(context.Background(), "latest")

	if err != nil {
		test.Fatalf("Plan returned error: %v", err)
	}

	if plan.UpdateNeeded {
		test.Error("UpdateNeeded = true, want false when already on the target version")
	}
}

func TestPlanMissingAssetForPlatform(test *testing.T) {
	release := newFakeRelease(test, "v1.3.0", "binary")
	// Rewrite the asset name so it no longer matches this host.
	release.archiveName = "tusk_1.3.0_plan9_sparc.tar.gz"

	base := startReleaseServer(test, release)

	updater := &Updater{
		APIBase:        base,
		ExecPath:       installedBinary(test, "current"),
		CurrentVersion: "v1.2.0",
	}

	_, err := updater.Plan(context.Background(), "latest")

	if !errors.Is(err, ErrNoAsset) {
		test.Fatalf("Plan error = %v, want ErrNoAsset", err)
	}
}

func TestApplyReplacesBinary(test *testing.T) {
	release := newFakeRelease(test, "v1.3.0", "NEW BINARY CONTENT")
	base := startReleaseServer(test, release)

	target := installedBinary(test, "OLD BINARY CONTENT")

	updater := &Updater{
		APIBase:        base,
		ExecPath:       target,
		CurrentVersion: "v1.2.0",
		SkipManPages:   true,
	}

	plan, planErr := updater.Plan(context.Background(), "latest")

	if planErr != nil {
		test.Fatalf("Plan returned error: %v", planErr)
	}

	result, applyErr := updater.Apply(context.Background(), plan)

	if applyErr != nil {
		test.Fatalf("Apply returned error: %v", applyErr)
	}

	if result.NewVersion != "v1.3.0" {
		test.Errorf("NewVersion = %q, want v1.3.0", result.NewVersion)
	}

	swapped, readErr := os.ReadFile(target)

	if readErr != nil {
		test.Fatalf("reading swapped binary: %v", readErr)
	}

	if string(swapped) != "NEW BINARY CONTENT" {
		test.Errorf("binary content = %q, want the downloaded release payload", swapped)
	}

	// The executable bit must survive the swap, or the update bricks tusk.
	info, statErr := os.Stat(target)

	if statErr != nil {
		test.Fatalf("stat swapped binary: %v", statErr)
	}

	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		test.Errorf("swapped binary mode = %v, want the executable bit set", info.Mode().Perm())
	}

	// No staging or backup debris may survive a clean update.
	assertNoDebris(test, filepath.Dir(target))
}

func TestApplyInstallsManPages(test *testing.T) {
	release := newFakeRelease(test, "v1.3.0", "new binary")
	base := startReleaseServer(test, release)

	target := installedBinary(test, "old binary")

	updater := &Updater{
		APIBase:        base,
		ExecPath:       target,
		CurrentVersion: "v1.2.0",
	}

	plan, planErr := updater.Plan(context.Background(), "latest")

	if planErr != nil {
		test.Fatalf("Plan returned error: %v", planErr)
	}

	result, applyErr := updater.Apply(context.Background(), plan)

	if applyErr != nil {
		test.Fatalf("Apply returned error: %v", applyErr)
	}

	// The stand-in binary lives in <root>/bin, so man pages belong in
	// <root>/share/man — the same layout install.sh uses. The expectation
	// is derived from the resolved binary path because resolveTarget
	// follows symlinks (on macOS /var is a link to /private/var).
	wantDir := filepath.Join(filepath.Dir(filepath.Dir(plan.BinaryPath)), "share", "man")

	if result.ManPagesDir != wantDir {
		test.Errorf("ManPagesDir = %q, want %q", result.ManPagesDir, wantDir)
	}

	for _, page := range []string{"tusk.1", "tusk-x.1"} {
		if _, statErr := os.Stat(filepath.Join(wantDir, "man1", page)); statErr != nil {
			test.Errorf("man page %s not installed: %v", page, statErr)
		}
	}
}

func TestApplyRejectsChecksumMismatch(test *testing.T) {
	release := newFakeRelease(test, "v1.3.0", "new binary")
	release.corruptChecksum = true

	base := startReleaseServer(test, release)

	target := installedBinary(test, "OLD BINARY CONTENT")

	updater := &Updater{
		APIBase:        base,
		ExecPath:       target,
		CurrentVersion: "v1.2.0",
		SkipManPages:   true,
	}

	plan, planErr := updater.Plan(context.Background(), "latest")

	if planErr != nil {
		test.Fatalf("Plan returned error: %v", planErr)
	}

	_, applyErr := updater.Apply(context.Background(), plan)

	if !errors.Is(applyErr, ErrChecksum) {
		test.Fatalf("Apply error = %v, want ErrChecksum", applyErr)
	}

	// A failed verification must leave the running binary untouched.
	preserved, readErr := os.ReadFile(target)

	if readErr != nil {
		test.Fatalf("reading binary after failed update: %v", readErr)
	}

	if string(preserved) != "OLD BINARY CONTENT" {
		test.Errorf("binary content = %q, want the original to be preserved", preserved)
	}

	assertNoDebris(test, filepath.Dir(target))
}

func TestApplyRefusesNonReleaseInstall(test *testing.T) {
	release := newFakeRelease(test, "v1.3.0", "new binary")
	base := startReleaseServer(test, release)

	target := installedBinary(test, "OLD BINARY CONTENT")

	updater := &Updater{
		APIBase:  base,
		ExecPath: target,
		// A dev version marks a local build, which tusk update must not
		// silently replace.
		CurrentVersion: "v1.0.0-dev",
		SkipManPages:   true,
	}

	plan, planErr := updater.Plan(context.Background(), "latest")

	if planErr != nil {
		test.Fatalf("Plan returned error: %v", planErr)
	}

	if plan.InstallMethod != "source build" {
		test.Errorf("InstallMethod = %q, want source build", plan.InstallMethod)
	}

	_, applyErr := updater.Apply(context.Background(), plan)

	if !errors.Is(applyErr, ErrInstallMethod) {
		test.Fatalf("Apply error = %v, want ErrInstallMethod", applyErr)
	}

	preserved, readErr := os.ReadFile(target)

	if readErr != nil {
		test.Fatalf("reading binary after refusal: %v", readErr)
	}

	if string(preserved) != "OLD BINARY CONTENT" {
		test.Error("a refused update modified the binary")
	}
}

func TestApplyForceOverridesRefusal(test *testing.T) {
	release := newFakeRelease(test, "v1.3.0", "NEW BINARY CONTENT")
	base := startReleaseServer(test, release)

	target := installedBinary(test, "OLD BINARY CONTENT")

	updater := &Updater{
		APIBase:        base,
		ExecPath:       target,
		CurrentVersion: "v1.0.0-dev",
		Force:          true,
		SkipManPages:   true,
	}

	plan, planErr := updater.Plan(context.Background(), "latest")

	if planErr != nil {
		test.Fatalf("Plan returned error: %v", planErr)
	}

	if _, applyErr := updater.Apply(context.Background(), plan); applyErr != nil {
		test.Fatalf("Apply with Force returned error: %v", applyErr)
	}

	swapped, readErr := os.ReadFile(target)

	if readErr != nil {
		test.Fatalf("reading swapped binary: %v", readErr)
	}

	if string(swapped) != "NEW BINARY CONTENT" {
		test.Errorf("binary content = %q, want the update to have been applied", swapped)
	}
}

func TestApplySameVersionIsNoOp(test *testing.T) {
	release := newFakeRelease(test, "v1.2.0", "RELEASE CONTENT")
	base := startReleaseServer(test, release)

	target := installedBinary(test, "UNTOUCHED")

	updater := &Updater{
		APIBase:        base,
		ExecPath:       target,
		CurrentVersion: "v1.2.0",
		SkipManPages:   true,
	}

	plan, planErr := updater.Plan(context.Background(), "latest")

	if planErr != nil {
		test.Fatalf("Plan returned error: %v", planErr)
	}

	if _, applyErr := updater.Apply(context.Background(), plan); applyErr != nil {
		test.Fatalf("Apply returned error: %v", applyErr)
	}

	preserved, readErr := os.ReadFile(target)

	if readErr != nil {
		test.Fatalf("reading binary: %v", readErr)
	}

	if string(preserved) != "UNTOUCHED" {
		test.Error("a no-op update rewrote the binary")
	}
}

func TestApplyDowngrade(test *testing.T) {
	older := newFakeRelease(test, "v1.0.0", "OLD RELEASE CONTENT")
	newer := newFakeRelease(test, "v1.3.0", "NEW RELEASE CONTENT")
	base := startReleaseServer(test, older, newer)

	target := installedBinary(test, "CURRENT CONTENT")

	updater := &Updater{
		APIBase:        base,
		ExecPath:       target,
		CurrentVersion: "v1.2.0",
		SkipManPages:   true,
	}

	plan, planErr := updater.Plan(context.Background(), "v1.0.0")

	if planErr != nil {
		test.Fatalf("Plan returned error: %v", planErr)
	}

	if _, applyErr := updater.Apply(context.Background(), plan); applyErr != nil {
		test.Fatalf("Apply returned error: %v", applyErr)
	}

	swapped, readErr := os.ReadFile(target)

	if readErr != nil {
		test.Fatalf("reading swapped binary: %v", readErr)
	}

	if string(swapped) != "OLD RELEASE CONTENT" {
		test.Errorf("binary content = %q, want the downgrade to have been applied", swapped)
	}
}

// assertNoDebris fails if staging or backup files survived in dir.
func assertNoDebris(harness testing.TB, dir string) {
	harness.Helper()

	entries, readErr := os.ReadDir(dir)

	if readErr != nil {
		harness.Fatalf("reading %s: %v", dir, readErr)
	}

	for _, entry := range entries {
		name := entry.Name()

		if name == "tusk" {
			continue
		}

		harness.Errorf("leftover file %q in %s after update", name, dir)
	}
}
