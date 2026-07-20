package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// maxExtractSize bounds any single extracted file, so a malformed or hostile
// archive cannot exhaust the disk during decompression.
const maxExtractSize = 128 << 20 // 128 MiB

// extracted lists what an update needs out of a release archive: the binary
// and, when present, the man pages that ship beside it.
type extracted struct {
	binaryPath string
	manPages   []string
}

// extractArchive unpacks the entries we care about from archivePath into
// destDir. Everything else in the archive — LICENSE, README, CHANGELOG — is
// ignored; a self-update replaces the binary and its docs, not the tree.
func extractArchive(archivePath string, destDir string, binaryName string) (extracted, error) {
	if strings.HasSuffix(archivePath, ".zip") {
		return extractZip(archivePath, destDir, binaryName)
	}

	return extractTarGz(archivePath, destDir, binaryName)
}

// wanted classifies an archive entry. It returns the destination filename
// and whether the entry is the binary; entries we do not need return false.
//
// Both matches are anchored to the archive root rather than made on the base
// name alone. goreleaser puts the binary at the root and man pages under
// man/, so anchoring costs nothing — and it means a nested entry such as
// docs/tusk cannot masquerade as the binary and overwrite the real one.
func wanted(entryName string, binaryName string) (string, bool, bool) {
	clean := path.Clean(filepath.ToSlash(entryName))

	if clean == binaryName {
		return clean, true, true
	}

	if strings.HasPrefix(clean, "man/") && strings.HasSuffix(clean, ".1") {
		trimmed := strings.TrimPrefix(clean, "man/")

		// Only the man/ directory itself, not deeper nesting.
		if !strings.Contains(trimmed, "/") {
			return trimmed, false, true
		}
	}

	return "", false, false
}

// safeJoin resolves a destination path under destDir, rejecting any entry
// whose name would escape it. Only base names reach here, but the guard is
// kept so a future change to wanted cannot reintroduce traversal.
func safeJoin(destDir string, name string) (string, error) {
	target := filepath.Join(destDir, filepath.Base(name))

	if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry %q escapes the extraction directory", name)
	}

	return target, nil
}

// extractTarGz handles the tar.gz archives shipped for linux and darwin.
func extractTarGz(archivePath string, destDir string, binaryName string) (extracted, error) {
	file, openErr := os.Open(archivePath)

	if openErr != nil {
		return extracted{}, fmt.Errorf("opening %s: %w", archivePath, openErr)
	}

	defer file.Close()

	gzipReader, gzipErr := gzip.NewReader(file)

	if gzipErr != nil {
		return extracted{}, fmt.Errorf("reading %s as gzip: %w", archivePath, gzipErr)
	}

	defer gzipReader.Close()

	var result extracted

	reader := tar.NewReader(gzipReader)

	for {
		header, nextErr := reader.Next()

		if nextErr == io.EOF {
			break
		}

		if nextErr != nil {
			return extracted{}, fmt.Errorf("reading %s: %w", archivePath, nextErr)
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		name, isBinary, keep := wanted(header.Name, binaryName)

		if !keep {
			continue
		}

		target, joinErr := safeJoin(destDir, name)

		if joinErr != nil {
			return extracted{}, joinErr
		}

		if writeErr := writeEntry(target, reader, entryMode(isBinary)); writeErr != nil {
			return extracted{}, writeErr
		}

		if recordErr := result.record(target, isBinary); recordErr != nil {
			return extracted{}, recordErr
		}
	}

	return result.validate(archivePath, binaryName)
}

// extractZip handles the zip archives shipped for windows.
func extractZip(archivePath string, destDir string, binaryName string) (extracted, error) {
	reader, openErr := zip.OpenReader(archivePath)

	if openErr != nil {
		return extracted{}, fmt.Errorf("opening %s as zip: %w", archivePath, openErr)
	}

	defer reader.Close()

	var result extracted

	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}

		name, isBinary, keep := wanted(entry.Name, binaryName)

		if !keep {
			continue
		}

		target, joinErr := safeJoin(destDir, name)

		if joinErr != nil {
			return extracted{}, joinErr
		}

		source, entryErr := entry.Open()

		if entryErr != nil {
			return extracted{}, fmt.Errorf("reading %s from %s: %w", entry.Name, archivePath, entryErr)
		}

		writeErr := writeEntry(target, source, entryMode(isBinary))

		source.Close()

		if writeErr != nil {
			return extracted{}, writeErr
		}

		if recordErr := result.record(target, isBinary); recordErr != nil {
			return extracted{}, recordErr
		}
	}

	return result.validate(archivePath, binaryName)
}

// entryMode picks the permission bits for an extracted entry: the binary
// must be executable, docs need not be.
func entryMode(isBinary bool) os.FileMode {
	if isBinary {
		return 0o755
	}

	return 0o644
}

// writeEntry copies one archive entry to disk under a size cap.
func writeEntry(target string, source io.Reader, mode os.FileMode) error {
	file, createErr := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)

	if createErr != nil {
		return fmt.Errorf("creating %s: %w", target, createErr)
	}

	defer file.Close()

	written, copyErr := io.Copy(file, io.LimitReader(source, maxExtractSize+1))

	if copyErr != nil {
		return fmt.Errorf("writing %s: %w", target, copyErr)
	}

	if written > maxExtractSize {
		return fmt.Errorf("archive entry %s exceeds the %d byte cap", target, maxExtractSize)
	}

	// OpenFile honours mode only on creation; an existing file keeps its
	// old bits, so the executable bit is set explicitly.
	if chmodErr := os.Chmod(target, mode); chmodErr != nil {
		return fmt.Errorf("setting mode on %s: %w", target, chmodErr)
	}

	return nil
}

// record files an extracted entry into the result. A second binary entry is
// rejected rather than silently overwriting the first: an archive carrying
// two entries named tusk is malformed, and picking the last one would make
// which binary gets installed depend on archive ordering.
func (result *extracted) record(target string, isBinary bool) error {
	if !isBinary {
		result.manPages = append(result.manPages, target)

		return nil
	}

	if result.binaryPath != "" {
		return fmt.Errorf("archive contains more than one %s entry", filepath.Base(target))
	}

	result.binaryPath = target

	return nil
}

// validate rejects an archive that did not yield a binary — a release whose
// layout changed should fail loudly rather than swap in nothing.
func (result extracted) validate(archivePath string, binaryName string) (extracted, error) {
	if result.binaryPath == "" {
		return extracted{}, fmt.Errorf("%s contains no %s binary", archivePath, binaryName)
	}

	return result, nil
}
