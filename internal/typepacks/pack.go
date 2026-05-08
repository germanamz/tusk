package typepacks

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/germanamz/tusk/internal/lock"
	"github.com/germanamz/tusk/internal/workspace"
)

// AddPack fetches pack content from source, validates it, detects
// collisions with the user's tusk.toml, and atomically writes the
// merged manifest. Returns the first error encountered without
// modifying tusk.toml.
func AddPack(ctx context.Context, source string, force bool, workspaceRoot string) error {
	rawURL, resolveErr := Resolve(source)

	if resolveErr != nil {
		return resolveErr
	}

	workspaceLock, lockErr := lock.NewWorkspaceLock(workspaceRoot)

	if lockErr != nil {
		return fmt.Errorf("pack add: workspace lock: %w", lockErr)
	}

	if acquireErr := workspaceLock.Acquire(ctx); acquireErr != nil {
		return fmt.Errorf("pack add: acquire lock: %w", acquireErr)
	}

	defer func() { _ = workspaceLock.Release() }()

	manifestPath := filepath.Join(workspaceRoot, workspace.ManifestFilename)

	userBody, readErr := os.ReadFile(manifestPath)

	if readErr != nil {
		return fmt.Errorf("pack add: read tusk.toml: %w", readErr)
	}

	packBody, fetchErr := Fetch(ctx, rawURL)

	if fetchErr != nil {
		return fetchErr
	}

	pack, validateErr := Validate(packBody)

	if validateErr != nil {
		return validateErr
	}

	collisions, collisionErr := FindCollisions(userBody, pack)

	if collisionErr != nil {
		return collisionErr
	}

	if len(collisions) > 0 && !force {
		return fmt.Errorf("pack add: cannot apply pack from %s: %d colliding sections in tusk.toml:%s\nre-run with --force to overwrite, or remove the colliding sections by hand", rawURL, len(collisions), formatCollisionList(collisions))
	}

	finalBody := userBody

	if len(collisions) > 0 && force {
		finalBody = StripSections(userBody, collisions)
	}

	composed := composeManifest(finalBody, packBody, source)

	if writeErr := atomicWrite(manifestPath, composed); writeErr != nil {
		return fmt.Errorf("pack add: write tusk.toml: %w", writeErr)
	}

	return nil
}

// composeManifest concatenates the user portion (possibly with sections
// stripped), a header comment naming the source and date, and the pack
// body. Pack body is appended verbatim — no TOML re-emit. Ensures
// exactly one blank line between the user content and the header comment.
func composeManifest(userBody, packBody []byte, source string) []byte {
	header := fmt.Sprintf("# Added by `tusk pack add %s` on %s\n", source, time.Now().Format("2006-01-02"))

	// Normalise user content: trim all trailing newlines, then add exactly one.
	trimmed := bytes.TrimRight(userBody, "\n")
	output := make([]byte, 0, len(trimmed)+2+len(header)+len(packBody))
	output = append(output, trimmed...)
	output = append(output, '\n', '\n') // one blank line before header
	output = append(output, header...)
	output = append(output, packBody...)

	return output
}

// atomicWrite writes content to path via a sibling temp file, fsync,
// and rename. Mirrors internal/node/service.go atomicWrite.
func atomicWrite(path string, content []byte) error {
	dir := filepath.Dir(path)
	tempFile, createErr := os.CreateTemp(dir, ".tusk-pack-*.tmp")

	if createErr != nil {
		return createErr
	}

	tempPath := tempFile.Name()

	if _, writeErr := tempFile.Write(content); writeErr != nil {
		tempFile.Close()
		_ = os.Remove(tempPath)

		return writeErr
	}

	if syncErr := tempFile.Sync(); syncErr != nil {
		tempFile.Close()
		_ = os.Remove(tempPath)

		return syncErr
	}

	if closeErr := tempFile.Close(); closeErr != nil {
		_ = os.Remove(tempPath)

		return closeErr
	}

	if renameErr := os.Rename(tempPath, path); renameErr != nil {
		_ = os.Remove(tempPath)

		return renameErr
	}

	return nil
}

func formatCollisionList(sections []string) string {
	var output string

	for _, section := range sections {
		output += "\n  - [" + section + "]"
	}

	return output
}
