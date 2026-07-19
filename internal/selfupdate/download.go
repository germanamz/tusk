package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	downloadTimeout = 10 * time.Minute
	// archiveSizeCap bounds a release archive. Tusk archives are a handful
	// of megabytes; the cap exists so a redirect to something enormous
	// cannot fill the disk.
	archiveSizeCap = 256 << 20 // 256 MiB
	checksumCap    = 1 << 20   // 1 MiB
)

// downloadArchive streams the release archive to a file inside destDir and
// returns its path. The body is size-capped and hashed as it is written, so
// the archive is never read from disk twice.
func (updater *Updater) downloadArchive(ctx context.Context, archiveURL string, destDir string, name string) (string, string, error) {
	response, requestErr := updater.do(ctx, archiveURL, downloadTimeout)

	if requestErr != nil {
		return "", "", requestErr
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("%w: HTTP %d downloading %s", ErrNetwork, response.StatusCode, archiveURL)
	}

	// filepath.Base is belt-and-braces: the name derives from a tag that
	// Resolve has already validated, but a download must never be able to
	// write outside the working directory even if that guard regresses.
	archivePath := filepath.Join(destDir, filepath.Base(name))

	file, createErr := os.Create(archivePath)

	if createErr != nil {
		return "", "", fmt.Errorf("creating %s: %w", archivePath, createErr)
	}

	defer file.Close()

	digest := sha256.New()
	limited := io.LimitReader(response.Body, archiveSizeCap+1)

	written, copyErr := io.Copy(io.MultiWriter(file, digest), limited)

	if copyErr != nil {
		return "", "", fmt.Errorf("%w: downloading %s: %w", ErrNetwork, archiveURL, copyErr)
	}

	if written > archiveSizeCap {
		return "", "", fmt.Errorf("%w: %s exceeds the %d byte size cap", ErrNetwork, archiveURL, archiveSizeCap)
	}

	if syncErr := file.Sync(); syncErr != nil {
		return "", "", fmt.Errorf("flushing %s: %w", archivePath, syncErr)
	}

	return archivePath, hex.EncodeToString(digest.Sum(nil)), nil
}

// fetchChecksums downloads the release checksums file and indexes it by
// asset name. goreleaser writes one "<sha256>  <filename>" line per artifact.
func (updater *Updater) fetchChecksums(ctx context.Context, checksumURL string) (map[string]string, error) {
	response, requestErr := updater.do(ctx, checksumURL, resolveTimeout)

	if requestErr != nil {
		return nil, requestErr
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: HTTP %d downloading %s", ErrNetwork, response.StatusCode, checksumURL)
	}

	body, readErr := io.ReadAll(io.LimitReader(response.Body, checksumCap))

	if readErr != nil {
		return nil, fmt.Errorf("%w: reading %s: %w", ErrNetwork, checksumURL, readErr)
	}

	return parseChecksums(string(body)), nil
}

// parseChecksums turns a checksums.txt body into a name-to-digest map.
// Malformed lines are skipped rather than failing the parse: a missing entry
// for the archive we care about is caught by the lookup, and an unrelated
// junk line should not block a legitimate update.
func parseChecksums(body string) map[string]string {
	digests := make(map[string]string)

	for line := range strings.SplitSeq(body, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))

		if len(fields) != 2 {
			continue
		}

		// The binary-mode marker "*" prefixes the name in some tools.
		digests[strings.TrimPrefix(fields[1], "*")] = strings.ToLower(fields[0])
	}

	return digests
}

// verifyChecksum matches a computed digest against the release's recorded
// one. A missing entry is as fatal as a mismatch: an unlisted archive is an
// unverified archive.
func verifyChecksum(digests map[string]string, name string, actual string) error {
	expected, listed := digests[name]

	if !listed {
		return fmt.Errorf("%w: %s is not listed in %s", ErrChecksum, name, ChecksumsAsset)
	}

	if !strings.EqualFold(expected, actual) {
		return fmt.Errorf("%w: %s digest mismatch\n  expected %s\n  actual   %s",
			ErrChecksum, name, expected, actual)
	}

	return nil
}
