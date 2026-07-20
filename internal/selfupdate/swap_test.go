package selfupdate

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSwapBinaryReplacesAndCleansUp(test *testing.T) {
	dir := test.TempDir()
	target := filepath.Join(dir, "tusk")

	if writeErr := os.WriteFile(target, []byte("OLD"), 0o755); writeErr != nil {
		test.Fatalf("writing target: %v", writeErr)
	}

	source := filepath.Join(test.TempDir(), "tusk-new")

	if writeErr := os.WriteFile(source, []byte("NEW"), 0o755); writeErr != nil {
		test.Fatalf("writing replacement: %v", writeErr)
	}

	staged, stageErr := stageBinary(source, target)

	if stageErr != nil {
		test.Fatalf("stageBinary returned error: %v", stageErr)
	}

	// Staging must land beside the target so the swap is a same-filesystem
	// rename; a staged file elsewhere would make the rename non-atomic.
	if filepath.Dir(staged) != filepath.Dir(target) {
		test.Errorf("staged at %q, want it beside the target in %q", staged, filepath.Dir(target))
	}

	if swapErr := swapBinary(staged, target); swapErr != nil {
		test.Fatalf("swapBinary returned error: %v", swapErr)
	}

	content, readErr := os.ReadFile(target)

	if readErr != nil {
		test.Fatalf("reading target: %v", readErr)
	}

	if string(content) != "NEW" {
		test.Errorf("target content = %q, want NEW", content)
	}

	if _, statErr := os.Stat(target + backupSuffix); !errors.Is(statErr, os.ErrNotExist) {
		test.Error("backup file survived a successful swap")
	}
}

func TestSwapBinaryRestoresOnFailure(test *testing.T) {
	if runtime.GOOS == "windows" {
		test.Skip("directory permission bits do not block renames on windows")
	}

	dir := test.TempDir()
	target := filepath.Join(dir, "tusk")

	if writeErr := os.WriteFile(target, []byte("OLD"), 0o755); writeErr != nil {
		test.Fatalf("writing target: %v", writeErr)
	}

	// Point at a staged file that does not exist: the move-aside succeeds
	// and the install rename then fails, which is exactly the window the
	// rollback exists to cover.
	staged := filepath.Join(dir, stagePrefix+"missing")

	swapErr := swapBinary(staged, target)

	if swapErr == nil {
		test.Fatal("swapBinary succeeded with a missing staged file, want an error")
	}

	// The original binary must be back in place; leaving nothing at target
	// would uninstall tusk rather than fail to update it.
	restored, readErr := os.ReadFile(target)

	if readErr != nil {
		test.Fatalf("original binary was not restored: %v", readErr)
	}

	if string(restored) != "OLD" {
		test.Errorf("restored content = %q, want OLD", restored)
	}

	if _, statErr := os.Stat(target + backupSuffix); !errors.Is(statErr, os.ErrNotExist) {
		test.Error("backup file survived the rollback")
	}
}

// TestSwapBinarySweepsStaleBackup covers the Windows path, where the previous
// update could not delete its backup because the image was still running.
func TestSwapBinarySweepsStaleBackup(test *testing.T) {
	dir := test.TempDir()
	target := filepath.Join(dir, "tusk")

	if writeErr := os.WriteFile(target, []byte("OLD"), 0o755); writeErr != nil {
		test.Fatalf("writing target: %v", writeErr)
	}

	if writeErr := os.WriteFile(target+backupSuffix, []byte("STALE"), 0o755); writeErr != nil {
		test.Fatalf("writing stale backup: %v", writeErr)
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

	if _, statErr := os.Stat(target + backupSuffix); !errors.Is(statErr, os.ErrNotExist) {
		test.Error("stale backup was not swept")
	}
}

func TestStageBinaryIsExecutable(test *testing.T) {
	if runtime.GOOS == "windows" {
		test.Skip("unix permission bits are not meaningful on windows")
	}

	dir := test.TempDir()
	target := filepath.Join(dir, "tusk")

	if writeErr := os.WriteFile(target, []byte("OLD"), 0o755); writeErr != nil {
		test.Fatalf("writing target: %v", writeErr)
	}

	source := filepath.Join(test.TempDir(), "tusk-new")

	// Deliberately non-executable: CreateTemp makes 0600 files, so the
	// staging step has to set the bit itself.
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

	if info.Mode().Perm()&0o111 == 0 {
		test.Errorf("staged mode = %v, want the executable bit set", info.Mode().Perm())
	}
}

func TestPreflightWritableRejectsReadOnlyDir(test *testing.T) {
	if runtime.GOOS == "windows" {
		test.Skip("directory permission bits do not block writes on windows")
	}

	if os.Geteuid() == 0 {
		test.Skip("root bypasses directory permission bits")
	}

	dir := test.TempDir()

	if chmodErr := os.Chmod(dir, 0o555); chmodErr != nil {
		test.Fatalf("making dir read-only: %v", chmodErr)
	}

	test.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	probeErr := preflightWritable(filepath.Join(dir, "tusk"))

	if !errors.Is(probeErr, ErrPermission) {
		test.Fatalf("preflightWritable error = %v, want ErrPermission", probeErr)
	}
}

func TestPreflightWritableLeavesNoProbeFile(test *testing.T) {
	dir := test.TempDir()

	if probeErr := preflightWritable(filepath.Join(dir, "tusk")); probeErr != nil {
		test.Fatalf("preflightWritable returned error: %v", probeErr)
	}

	entries, readErr := os.ReadDir(dir)

	if readErr != nil {
		test.Fatalf("reading dir: %v", readErr)
	}

	if len(entries) != 0 {
		test.Errorf("preflight left %d file(s) behind, want none", len(entries))
	}
}

// TestExtractIgnoresUnwantedEntries asserts the archive's docs are left in
// place: an update replaces the binary and its man pages, not the tree.
func TestExtractIgnoresUnwantedEntries(test *testing.T) {
	dir := test.TempDir()
	archivePath := filepath.Join(dir, "release.tar.gz")

	archive := buildTarGz(test, map[string]string{
		"tusk":       "binary",
		"man/tusk.1": "man page",
		"LICENSE":    "license",
		"README.md":  "readme",
	})

	if writeErr := os.WriteFile(archivePath, archive, 0o644); writeErr != nil {
		test.Fatalf("writing archive: %v", writeErr)
	}

	dest := test.TempDir()

	unpacked, extractErr := extractArchive(archivePath, dest, "tusk")

	if extractErr != nil {
		test.Fatalf("extractArchive returned error: %v", extractErr)
	}

	if unpacked.binaryPath == "" {
		test.Fatal("no binary extracted")
	}

	if len(unpacked.manPages) != 1 {
		test.Errorf("extracted %d man pages, want 1", len(unpacked.manPages))
	}

	for _, unwanted := range []string{"LICENSE", "README.md"} {
		if _, statErr := os.Stat(filepath.Join(dest, unwanted)); !errors.Is(statErr, os.ErrNotExist) {
			test.Errorf("%s was extracted, want it ignored", unwanted)
		}
	}
}

// TestExtractRejectsArchiveWithoutBinary guards the case where a release's
// layout changes: failing loudly beats swapping in nothing.
func TestExtractRejectsArchiveWithoutBinary(test *testing.T) {
	dir := test.TempDir()
	archivePath := filepath.Join(dir, "release.tar.gz")

	archive := buildTarGz(test, map[string]string{"README.md": "readme"})

	if writeErr := os.WriteFile(archivePath, archive, 0o644); writeErr != nil {
		test.Fatalf("writing archive: %v", writeErr)
	}

	if _, extractErr := extractArchive(archivePath, test.TempDir(), "tusk"); extractErr == nil {
		test.Fatal("extractArchive accepted an archive with no binary, want an error")
	}
}

// TestExtractRejectsPathTraversal asserts a hostile archive cannot write
// outside the extraction directory.
func TestExtractRejectsPathTraversal(test *testing.T) {
	dir := test.TempDir()
	archivePath := filepath.Join(dir, "release.tar.gz")

	archive := buildTarGz(test, map[string]string{
		"tusk":              "binary",
		"man/../../evil.1":  "escape attempt",
		"../../../etc/tusk": "escape attempt",
	})

	if writeErr := os.WriteFile(archivePath, archive, 0o644); writeErr != nil {
		test.Fatalf("writing archive: %v", writeErr)
	}

	dest := test.TempDir()

	unpacked, extractErr := extractArchive(archivePath, dest, "tusk")

	if extractErr != nil {
		test.Fatalf("extractArchive returned error: %v", extractErr)
	}

	// Nothing may land outside dest.
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(dest), "evil.1")); !errors.Is(statErr, os.ErrNotExist) {
		test.Error("a traversal entry escaped the extraction directory")
	}

	for _, page := range unpacked.manPages {
		if filepath.Dir(page) != filepath.Clean(dest) {
			test.Errorf("man page landed at %q, outside %q", page, dest)
		}
	}
}
